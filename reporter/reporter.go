// Package reporter tells the platform what the runner sees.
//
// The platform used to ask. The Agents Orchestrator dialed the runner on its
// reconcile interval and called InspectWorkload, so a workload was ready and
// serving for up to a full interval before anything recorded it -- and the
// sandbox behind it stayed "starting" for exactly that long. The runner is the
// only component that can see its workloads, so it is the only one that can
// say when.
//
// What a backend observes and how is its own business (a Pod informer on the
// k8s-runner); this package is the part that is the same for every runner:
// the last status sent per workload, so nothing is resent, and the best-effort
// delivery to the Gateway.
package reporter

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/agynio/runner-core/backend"
	gatewayv1 "github.com/agynio/runner-core/gen/agynio/api/gateway/v1"
	runnersv1 "github.com/agynio/runner-core/gen/agynio/api/runners/v1"
)

const (
	reportTimeout = 10 * time.Second
	retryBackoff  = time.Second
	retryAttempts = 3
)

// Reporter sends observed workload state to the platform. It implements
// backend.Observer, so a backend's state source hands what it sees straight
// to it.
type Reporter struct {
	gateway      gatewayv1.RunnersGatewayClient
	serviceToken string
	logger       *zap.Logger

	// reported is the last status sent per workload, so a resync or an
	// unrelated update does not resend what the platform already has.
	mu       sync.Mutex
	reported map[string]runnersv1.WorkloadStatus
}

var _ backend.Observer = (*Reporter)(nil)

func New(gateway gatewayv1.RunnersGatewayClient, serviceToken string, logger *zap.Logger) *Reporter {
	return &Reporter{
		gateway:      gateway,
		serviceToken: serviceToken,
		logger:       logger,
		reported:     map[string]runnersv1.WorkloadStatus{},
	}
}

// Observe reports a workload's runtime state when it is one the platform does
// not already have. The status goes out as observed; the reporter does not
// interpret it.
func (r *Reporter) Observe(ctx context.Context, observation backend.Observation) {
	if !r.changed(observation.WorkloadID, observation.Status) {
		return
	}
	r.report(ctx, observation.WorkloadID, observation.Status)
}

// Forget drops a workload's last reported status so one restarted under the
// same id reports again from scratch.
//
// Deliberately reports nothing itself. A workload that is gone is the
// platform's to settle -- the Orchestrator's reconciliation notices the
// absence and decides what it means.
func (r *Reporter) Forget(workloadID string) {
	r.mu.Lock()
	delete(r.reported, workloadID)
	r.mu.Unlock()
}

func (r *Reporter) changed(workloadID string, observed runnersv1.WorkloadStatus) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.reported[workloadID]; ok && last == observed {
		return false
	}
	r.reported[workloadID] = observed
	return true
}

// report is best-effort. A failure is retried a few times and then dropped: the
// Orchestrator's reconciliation is the backstop, so a runner that cannot report
// is behind on latency, not on correctness.
func (r *Reporter) report(ctx context.Context, workloadID string, observed runnersv1.WorkloadStatus) {
	observedAt := timestamppb.Now()
	for attempt := 0; attempt < retryAttempts; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, reportTimeout)
		_, err := r.gateway.ReportWorkloadState(callCtx, &runnersv1.ReportWorkloadStateRequest{
			ServiceToken: r.serviceToken,
			WorkloadId:   workloadID,
			Status:       observed,
			ObservedAt:   observedAt,
		})
		cancel()
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		// A platform older than this RPC answers Unimplemented. It reconciles on
		// its own interval exactly as it always did, so the runner keeps serving
		// and simply stops trying to help.
		if status.Code(err) == codes.Unimplemented {
			r.logger.Info("platform does not accept workload state reports; leaving it to reconciliation")
			return
		}
		if attempt == retryAttempts-1 {
			r.logger.Warn("report workload state",
				zap.String("workload_id", workloadID),
				zap.String("status", observed.String()),
				zap.Error(err))
			// Forgotten so the next observation retries rather than being
			// suppressed as already reported.
			r.mu.Lock()
			delete(r.reported, workloadID)
			r.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryBackoff << attempt):
		}
	}
}
