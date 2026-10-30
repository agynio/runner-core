// Package runnercore is the backend-agnostic front of an agyn runner: it
// serves the Runner API, enrols with the Gateway, reports the catalog and the
// workload state it is told about, and binds the runner's OpenZiti service.
//
// A runner implements backend.Backend for whatever hosts its workloads and
// calls Run. What differs between runners -- Pods or microVMs, how a flavor
// is realised, whether a workload can sleep -- stays behind that interface.
package runnercore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/openziti/sdk-golang/ziti"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/agynio/runner-core/backend"
	"github.com/agynio/runner-core/catalog"
	"github.com/agynio/runner-core/config"
	runnersgatewayv1 "github.com/agynio/runner-core/gen/agynio/api/gateway/v1"
	runnerv1 "github.com/agynio/runner-core/gen/agynio/api/runner/v1"
	runnersv1 "github.com/agynio/runner-core/gen/agynio/api/runners/v1"
	"github.com/agynio/runner-core/reporter"
	"github.com/agynio/runner-core/server"
)

const (
	retryInitialBackoff = 1 * time.Second
	retryMaxBackoff     = 15 * time.Second
)

const (
	zitiIdentityCheckInterval    = 30 * time.Second
	zitiIdentityFailureThreshold = 5
)

// Options is what a runner hands to Run.
type Options struct {
	Config  config.Config
	Logger  *zap.Logger
	Backend backend.Backend
	// Catalog is what this runner reports it offers.
	Catalog catalog.Catalog
	// Capabilities is what the backend implements beyond what the catalog
	// declares -- docker on a runner with an implementation configured. The
	// report is the union of the two. hibernate is never taken from here:
	// it is advertised exactly when Backend implements backend.Hibernator.
	Capabilities []string
}

// Run serves the runner until ctx ends or something it cannot serve without
// fails: the listener, enrollment, the OpenZiti identity.
func Run(ctx context.Context, opts Options) error {
	if opts.Backend == nil {
		return errors.New("runner backend is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	cfg := opts.Config

	runnerServer := server.New(opts.Backend)
	grpcServer := grpc.NewServer()
	runnerv1.RegisterRunnerServiceServer(grpcServer, runnerServer)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	startServe := func(listener net.Listener, label string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("gRPC server starting", zap.String("listener", label), zap.String("addr", listener.Addr().String()))
			err := grpcServer.Serve(listener)
			if errors.Is(err, grpc.ErrServerStopped) {
				err = nil
			}
			if err != nil {
				errCh <- err
			}
		}()
	}

	listener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.GRPCAddr, err)
	}
	defer listener.Close()
	startServe(listener, "tcp")

	if cfg.ZitiEnabled {
		gatewayConn, err := grpc.DialContext(ctx, cfg.GatewayAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("dial gateway: %w", err)
		}
		defer gatewayConn.Close()

		gatewayClient := runnersgatewayv1.NewRunnersGatewayClient(gatewayConn)

		enrollmentCtx, cancel := context.WithTimeout(ctx, cfg.ZitiEnrollmentTimeout)
		defer cancel()

		var enrollResponse *runnersv1.EnrollRunnerResponse
		if err := retryWithBackoff(enrollmentCtx, logger, "gateway enrollment", func(attemptCtx context.Context) error {
			var requestErr error
			enrollResponse, requestErr = gatewayClient.EnrollRunner(attemptCtx, &runnersv1.EnrollRunnerRequest{
				ServiceToken: cfg.ServiceToken,
			})
			return requestErr
		}); err != nil {
			return fmt.Errorf("enroll runner via gateway: %w", err)
		}

		// Reported before the service is bound, so the platform knows what this
		// runner offers by the time anything can be scheduled onto it.
		//
		// On its own deadline rather than the enrollment one: enrollment has
		// already consumed an unknown share of that budget by this point, so
		// sharing it made a slow enrollment leave no time to report and killed
		// the runner for a delay it had already survived.
		report := opts.Catalog.Report(cfg.ServiceToken, reportedCapabilities(opts, runnerServer.CanHibernate(), logger))
		catalogCtx, cancelCatalog := context.WithTimeout(ctx, cfg.ZitiEnrollmentTimeout)
		catalogErr := retryWithBackoff(catalogCtx, logger, "catalog report", func(attemptCtx context.Context) error {
			_, requestErr := gatewayClient.ReportRunnerCatalog(attemptCtx, report)
			return requestErr
		})
		cancelCatalog()
		if catalogErr != nil {
			// Not fatal. A platform whose Runners service predates
			// ReportRunnerCatalog answers Unimplemented, and refusing to start
			// would mean a runner cannot be upgraded before the platform is —
			// it would crash-loop rather than serve.
			//
			// Serving without a reported catalog is already a handled state: a
			// workload naming a flavor the platform cannot resolve fails to
			// schedule with the standard retry and unschedulable flagging, and
			// recovers as soon as a report lands.
			logger.Error("catalog report failed; serving without it",
				zap.Error(catalogErr))
		} else {
			logger.Info("catalog reported",
				zap.Int("flavors", len(opts.Catalog.Flavors)),
				zap.Int("storageClasses", len(opts.Catalog.StorageClasses)),
				zap.Int("capabilities", len(opts.Catalog.Capabilities)))
		}

		zitiConfig := &ziti.Config{}
		if err := json.Unmarshal([]byte(enrollResponse.IdentityJson), zitiConfig); err != nil {
			return fmt.Errorf("parse ziti identity: %w", err)
		}

		zitiContext, err := ziti.NewContext(zitiConfig)
		if err != nil {
			return fmt.Errorf("create ziti context: %w", err)
		}
		defer zitiContext.Close()

		zitiListener, err := zitiContext.ListenWithOptions(enrollResponse.ServiceName, ziti.DefaultListenOptions())
		if err != nil {
			return fmt.Errorf("listen on ziti service %s: %w", enrollResponse.ServiceName, err)
		}
		defer zitiListener.Close()
		startServe(zitiListener, "ziti")

		wg.Add(1)
		go func() {
			defer wg.Done()
			err := watchZitiIdentity(ctx, zitiContext.RefreshServices, zitiIdentityCheckInterval, zitiIdentityFailureThreshold, logger)
			if err != nil {
				errCh <- err
			}
		}()

		// The platform used to discover a workload was running by dialing this
		// runner on its reconcile interval and asking.
		wg.Add(1)
		go func() {
			defer wg.Done()
			workloadReporter := reporter.New(gatewayClient, cfg.ServiceToken, logger)
			if err := opts.Backend.WatchWorkloadState(ctx, workloadReporter); err != nil && ctx.Err() == nil {
				// Not fatal: reconciliation still converges without it, so a
				// runner that cannot report serves on and is late, not wrong.
				logger.Warn("workload state reporting stopped", zap.Error(err))
			}
		}()
	}

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		grpcServer.GracefulStop()
	}

	wg.Wait()
	return nil
}

// reportedCapabilities is what this runner tells the platform it can do: what
// the catalog declares merged with what the backend implements, and hibernate
// exactly when the backend can -- a runner that answers HibernateWorkload with
// Unimplemented advertises nothing, whatever its catalog says.
func reportedCapabilities(opts Options, canHibernate bool, logger *zap.Logger) []string {
	implemented := append([]string(nil), opts.Capabilities...)
	if canHibernate {
		implemented = append(implemented, catalog.CapabilityHibernate)
	}
	capabilities := catalog.Capabilities(opts.Catalog.Capabilities, implemented...)
	if canHibernate {
		return capabilities
	}
	kept := capabilities[:0]
	for _, capability := range capabilities {
		if capability == catalog.CapabilityHibernate {
			logger.Warn("catalog declares the hibernate capability but the backend cannot hibernate; not advertising it")
			continue
		}
		kept = append(kept, capability)
	}
	return kept
}

func retryWithBackoff(ctx context.Context, logger *zap.Logger, operationName string, fn func(context.Context) error) error {
	backoff := retryInitialBackoff
	attempt := 1
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !isRetryableGrpcError(err) {
			return err
		}

		delay := backoff
		if delay > retryMaxBackoff {
			delay = retryMaxBackoff
		}

		logger.Warn(
			"operation failed, retrying",
			zap.String("operation", operationName),
			zap.Int("attempt", attempt),
			zap.Duration("backoff", delay),
			zap.Error(err),
		)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		backoff *= 2
		if backoff > retryMaxBackoff {
			backoff = retryMaxBackoff
		}
		attempt++
	}
}

func isRetryableGrpcError(err error) bool {
	statusErr, ok := status.FromError(err)
	if !ok {
		return false
	}
	return statusErr.Code() == codes.Unavailable || statusErr.Code() == codes.Unknown
}

// The runner enrols once, at startup. If the controller stops accepting that
// identity -- it was deleted, or the controller lost it -- the SDK retries the
// bind forever and the runner stays Running with no terminator, so the platform
// keeps scheduling onto a runner nothing can reach. Exiting hands the problem to
// Kubernetes, which restarts the pod into a fresh enrolment.
func watchZitiIdentity(ctx context.Context, refresh func() error, interval time.Duration, threshold int, logger *zap.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if err := refresh(); err != nil {
			failures++
			logger.Warn("ziti identity check failed",
				zap.Int("consecutiveFailures", failures), zap.Error(err))
			if failures >= threshold {
				return fmt.Errorf("ziti identity is no longer usable after %d checks: %w", failures, err)
			}
			continue
		}
		if failures > 0 {
			logger.Info("ziti identity recovered", zap.Int("afterFailures", failures))
		}
		failures = 0
	}
}
