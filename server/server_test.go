package server

import (
	"context"
	"io"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/agynio/runner-core/backend"
	runnerv1 "github.com/agynio/runner-core/gen/agynio/api/runner/v1"
)

// fakeBackend answers every RPC with an empty response, so a test can see
// what the server adds around the backend rather than what the backend does.
type fakeBackend struct {
	inspected []string
}

func (f *fakeBackend) StartWorkload(context.Context, *runnerv1.StartWorkloadRequest) (*runnerv1.StartWorkloadResponse, error) {
	return &runnerv1.StartWorkloadResponse{}, nil
}
func (f *fakeBackend) StopWorkload(context.Context, *runnerv1.StopWorkloadRequest) (*runnerv1.StopWorkloadResponse, error) {
	return &runnerv1.StopWorkloadResponse{}, nil
}
func (f *fakeBackend) RemoveWorkload(context.Context, *runnerv1.RemoveWorkloadRequest) (*runnerv1.RemoveWorkloadResponse, error) {
	return &runnerv1.RemoveWorkloadResponse{}, nil
}
func (f *fakeBackend) InspectWorkload(_ context.Context, req *runnerv1.InspectWorkloadRequest) (*runnerv1.InspectWorkloadResponse, error) {
	f.inspected = append(f.inspected, req.GetWorkloadId())
	return &runnerv1.InspectWorkloadResponse{Id: req.GetWorkloadId()}, nil
}
func (f *fakeBackend) TouchWorkload(context.Context, *runnerv1.TouchWorkloadRequest) (*runnerv1.TouchWorkloadResponse, error) {
	return &runnerv1.TouchWorkloadResponse{}, nil
}
func (f *fakeBackend) ListWorkloads(context.Context, *runnerv1.ListWorkloadsRequest) (*runnerv1.ListWorkloadsResponse, error) {
	return &runnerv1.ListWorkloadsResponse{}, nil
}
func (f *fakeBackend) GetWorkloadLabels(context.Context, *runnerv1.GetWorkloadLabelsRequest) (*runnerv1.GetWorkloadLabelsResponse, error) {
	return &runnerv1.GetWorkloadLabelsResponse{}, nil
}
func (f *fakeBackend) FindWorkloadsByLabels(context.Context, *runnerv1.FindWorkloadsByLabelsRequest) (*runnerv1.FindWorkloadsByLabelsResponse, error) {
	return &runnerv1.FindWorkloadsByLabelsResponse{}, nil
}
func (f *fakeBackend) ListWorkloadsByVolume(context.Context, *runnerv1.ListWorkloadsByVolumeRequest) (*runnerv1.ListWorkloadsByVolumeResponse, error) {
	return &runnerv1.ListWorkloadsByVolumeResponse{}, nil
}
func (f *fakeBackend) ListVolumes(context.Context, *runnerv1.ListVolumesRequest) (*runnerv1.ListVolumesResponse, error) {
	return &runnerv1.ListVolumesResponse{}, nil
}
func (f *fakeBackend) RemoveVolume(context.Context, *runnerv1.RemoveVolumeRequest) (*runnerv1.RemoveVolumeResponse, error) {
	return &runnerv1.RemoveVolumeResponse{}, nil
}
func (f *fakeBackend) PutArchive(context.Context, *runnerv1.PutArchiveRequest) (*runnerv1.PutArchiveResponse, error) {
	return &runnerv1.PutArchiveResponse{}, nil
}
func (f *fakeBackend) PrepareExec(context.Context, string, []string, bool) (backend.Execution, error) {
	return nil, status.Error(codes.Unimplemented, "not in this test")
}
func (f *fakeBackend) OpenWorkloadLogs(context.Context, *runnerv1.StreamWorkloadLogsRequest) (io.ReadCloser, error) {
	return nil, status.Error(codes.Unimplemented, "not in this test")
}
func (f *fakeBackend) WorkloadExists(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fakeBackend) StreamEvents(*runnerv1.StreamEventsRequest, runnerv1.RunnerService_StreamEventsServer) error {
	return nil
}
func (f *fakeBackend) WatchWorkloadState(ctx context.Context, _ backend.Observer) error {
	<-ctx.Done()
	return ctx.Err()
}

var _ backend.Backend = (*fakeBackend)(nil)

// hibernatingBackend is a fakeBackend that can also sleep and wake.
type hibernatingBackend struct {
	fakeBackend
	hibernated []string
	woken      []string
}

func (h *hibernatingBackend) HibernateWorkload(_ context.Context, req *runnerv1.HibernateWorkloadRequest) (*runnerv1.HibernateWorkloadResponse, error) {
	h.hibernated = append(h.hibernated, req.GetWorkloadId())
	return &runnerv1.HibernateWorkloadResponse{}, nil
}

func (h *hibernatingBackend) WakeWorkload(_ context.Context, req *runnerv1.WakeWorkloadRequest) (*runnerv1.WakeWorkloadResponse, error) {
	h.woken = append(h.woken, req.GetWorkloadId())
	return &runnerv1.WakeWorkloadResponse{}, nil
}

// A backend that cannot hibernate answers exactly as a runner predating the
// RPCs does, so the Orchestrator treats both the same way.
func TestHibernationIsUnimplementedUnlessTheBackendCan(t *testing.T) {
	s := New(&fakeBackend{})
	if s.CanHibernate() {
		t.Fatal("expected a plain backend not to hibernate")
	}

	_, err := s.HibernateWorkload(context.Background(), &runnerv1.HibernateWorkloadRequest{WorkloadId: "w-1"})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("HibernateWorkload code = %v, want Unimplemented", status.Code(err))
	}
	if msg := status.Convert(err).Message(); msg != "method HibernateWorkload not implemented" {
		t.Fatalf("unexpected message %q", msg)
	}

	_, err = s.WakeWorkload(context.Background(), &runnerv1.WakeWorkloadRequest{WorkloadId: "w-1"})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("WakeWorkload code = %v, want Unimplemented", status.Code(err))
	}
	if msg := status.Convert(err).Message(); msg != "method WakeWorkload not implemented" {
		t.Fatalf("unexpected message %q", msg)
	}
}

func TestHibernationReachesABackendThatCan(t *testing.T) {
	b := &hibernatingBackend{}
	s := New(b)
	if !s.CanHibernate() {
		t.Fatal("expected a hibernating backend to be recognised")
	}

	if _, err := s.HibernateWorkload(context.Background(), &runnerv1.HibernateWorkloadRequest{WorkloadId: "w-1"}); err != nil {
		t.Fatalf("HibernateWorkload: %v", err)
	}
	if _, err := s.WakeWorkload(context.Background(), &runnerv1.WakeWorkloadRequest{WorkloadId: "w-1"}); err != nil {
		t.Fatalf("WakeWorkload: %v", err)
	}
	if len(b.hibernated) != 1 || b.hibernated[0] != "w-1" || len(b.woken) != 1 || b.woken[0] != "w-1" {
		t.Fatalf("expected the backend to see both calls, got hibernated=%v woken=%v", b.hibernated, b.woken)
	}
}

func TestUnaryCallsReachTheBackend(t *testing.T) {
	b := &fakeBackend{}
	s := New(b)

	resp, err := s.InspectWorkload(context.Background(), &runnerv1.InspectWorkloadRequest{WorkloadId: "w-2"})
	if err != nil {
		t.Fatalf("InspectWorkload: %v", err)
	}
	if resp.GetId() != "w-2" || len(b.inspected) != 1 {
		t.Fatalf("expected the backend's answer, got %+v (calls %v)", resp, b.inspected)
	}
}

func TestReadySaysOK(t *testing.T) {
	resp, err := New(&fakeBackend{}).Ready(context.Background(), &runnerv1.ReadyRequest{})
	if err != nil || resp.GetStatus() != "ok" {
		t.Fatalf("Ready = %+v, %v", resp, err)
	}
}

func TestCancelExecutionOfUnknownSessionIsNotCancelled(t *testing.T) {
	s := New(&fakeBackend{})

	resp, err := s.CancelExecution(context.Background(), &runnerv1.CancelExecutionRequest{ExecutionId: "nope"})
	if err != nil || resp.GetCancelled() {
		t.Fatalf("CancelExecution = %+v, %v", resp, err)
	}
	_, err = s.CancelExecution(context.Background(), &runnerv1.CancelExecutionRequest{ExecutionId: " "})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}
