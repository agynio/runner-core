package reporter

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/agynio/runner-core/backend"
	gatewayv1 "github.com/agynio/runner-core/gen/agynio/api/gateway/v1"
	runnersv1 "github.com/agynio/runner-core/gen/agynio/api/runners/v1"
)

// Every update and every resync re-delivers the same observation. Without this
// the runner would resend a state the platform already has, on a timer.
func TestChangedSuppressesARepeatOfTheSameStatus(t *testing.T) {
	r := New(nil, "token", nil)

	if !r.changed("workload-1", runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING) {
		t.Fatal("expected the first observation to be reported")
	}
	if r.changed("workload-1", runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING) {
		t.Fatal("expected a repeat of the same status to be suppressed")
	}
	if !r.changed("workload-1", runnersv1.WorkloadStatus_WORKLOAD_STATUS_FAILED) {
		t.Fatal("expected a different status to be reported")
	}
}

// A workload restarted under the same id must report again from scratch.
func TestForgetClearsTheRememberedStatus(t *testing.T) {
	r := New(nil, "token", nil)
	r.changed("workload-1", runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING)

	r.Forget("workload-1")

	if !r.changed("workload-1", runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING) {
		t.Fatal("expected a forgotten workload to report again")
	}
}

type recordingGateway struct {
	gatewayv1.RunnersGatewayClient
	reports []*runnersv1.ReportWorkloadStateRequest
	err     error
}

func (g *recordingGateway) ReportWorkloadState(_ context.Context, req *runnersv1.ReportWorkloadStateRequest, _ ...grpc.CallOption) (*runnersv1.ReportWorkloadStateResponse, error) {
	g.reports = append(g.reports, req)
	return &runnersv1.ReportWorkloadStateResponse{}, g.err
}

// The status reaches the platform exactly as the backend observed it, so a
// backend that hibernates reports the sleeping statuses through the same path.
func TestObservePassesTheStatusThroughUnchanged(t *testing.T) {
	gateway := &recordingGateway{}
	r := New(gateway, "token", zap.NewNop())

	r.Observe(context.Background(), backend.Observation{WorkloadID: "workload-1", Status: runnersv1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED})
	r.Observe(context.Background(), backend.Observation{WorkloadID: "workload-1", Status: runnersv1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED})

	if len(gateway.reports) != 1 {
		t.Fatalf("expected one report, got %d", len(gateway.reports))
	}
	report := gateway.reports[0]
	if report.ServiceToken != "token" || report.WorkloadId != "workload-1" {
		t.Fatalf("unexpected report %+v", report)
	}
	if report.Status != runnersv1.WorkloadStatus_WORKLOAD_STATUS_HIBERNATED {
		t.Fatalf("expected the observed status, got %v", report.Status)
	}
	if report.ObservedAt == nil {
		t.Fatal("expected observed_at to be set")
	}
}

// A platform older than this RPC answers Unimplemented; the runner keeps
// serving and simply stops trying to help, without retrying.
func TestObserveGivesUpOnUnimplemented(t *testing.T) {
	gateway := &recordingGateway{err: status.Error(codes.Unimplemented, "nope")}
	r := New(gateway, "token", zap.NewNop())

	r.Observe(context.Background(), backend.Observation{WorkloadID: "workload-1", Status: runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING})

	if len(gateway.reports) != 1 {
		t.Fatalf("expected a single attempt, got %d", len(gateway.reports))
	}
}

// A cancelled context ends the attempt loop rather than sleeping through it.
func TestObserveStopsWhenTheContextEnds(t *testing.T) {
	gateway := &recordingGateway{err: errors.New("gateway down")}
	r := New(gateway, "token", zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.Observe(ctx, backend.Observation{WorkloadID: "workload-1", Status: runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING})

	if len(gateway.reports) != 1 {
		t.Fatalf("expected a single attempt, got %d", len(gateway.reports))
	}
}
