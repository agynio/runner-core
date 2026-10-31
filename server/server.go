// Package server implements the RunnerService gRPC API on top of a backend.
//
// Every RPC reaches the backend; what the server adds is what every runner
// needs the same way: the exec session protocol, the log stream, Ready, and
// Unimplemented for hibernation unless the backend can.
package server

import (
	"context"
	"sync"

	"github.com/agynio/runner-core/backend"
	runnerv1 "github.com/agynio/runner-core/gen/agynio/api/runner/v1"
)

// Server implements the RunnerService gRPC API against a backend.
type Server struct {
	runnerv1.UnimplementedRunnerServiceServer
	backend backend.Backend
	// hibernator is the backend again when it can hibernate, nil otherwise.
	hibernator backend.Hibernator

	execSessions   map[string]*execSession
	execSessionsMu sync.Mutex
}

// New constructs a RunnerService server over b. HibernateWorkload and
// WakeWorkload are served only when b implements backend.Hibernator.
func New(b backend.Backend) *Server {
	s := &Server{
		backend:      b,
		execSessions: make(map[string]*execSession),
	}
	if hibernator, ok := b.(backend.Hibernator); ok {
		s.hibernator = hibernator
	}
	return s
}

// CanHibernate reports whether the backend implements hibernation, which is
// also whether the runner advertises the capability.
func (s *Server) CanHibernate() bool {
	return s.hibernator != nil
}

func (s *Server) Ready(_ context.Context, _ *runnerv1.ReadyRequest) (*runnerv1.ReadyResponse, error) {
	return &runnerv1.ReadyResponse{Status: "ok"}, nil
}

func (s *Server) StartWorkload(ctx context.Context, req *runnerv1.StartWorkloadRequest) (*runnerv1.StartWorkloadResponse, error) {
	return s.backend.StartWorkload(ctx, req)
}

func (s *Server) StopWorkload(ctx context.Context, req *runnerv1.StopWorkloadRequest) (*runnerv1.StopWorkloadResponse, error) {
	return s.backend.StopWorkload(ctx, req)
}

func (s *Server) RemoveWorkload(ctx context.Context, req *runnerv1.RemoveWorkloadRequest) (*runnerv1.RemoveWorkloadResponse, error) {
	return s.backend.RemoveWorkload(ctx, req)
}

func (s *Server) InspectWorkload(ctx context.Context, req *runnerv1.InspectWorkloadRequest) (*runnerv1.InspectWorkloadResponse, error) {
	return s.backend.InspectWorkload(ctx, req)
}

func (s *Server) TouchWorkload(ctx context.Context, req *runnerv1.TouchWorkloadRequest) (*runnerv1.TouchWorkloadResponse, error) {
	return s.backend.TouchWorkload(ctx, req)
}

// HibernateWorkload is answered by the backend when it can hibernate, and
// with Unimplemented otherwise, exactly as a runner predating the RPC would.
func (s *Server) HibernateWorkload(ctx context.Context, req *runnerv1.HibernateWorkloadRequest) (*runnerv1.HibernateWorkloadResponse, error) {
	if s.hibernator == nil {
		return s.UnimplementedRunnerServiceServer.HibernateWorkload(ctx, req)
	}
	return s.hibernator.HibernateWorkload(ctx, req)
}

// WakeWorkload is answered like HibernateWorkload.
func (s *Server) WakeWorkload(ctx context.Context, req *runnerv1.WakeWorkloadRequest) (*runnerv1.WakeWorkloadResponse, error) {
	if s.hibernator == nil {
		return s.UnimplementedRunnerServiceServer.WakeWorkload(ctx, req)
	}
	return s.hibernator.WakeWorkload(ctx, req)
}

func (s *Server) ListWorkloads(ctx context.Context, req *runnerv1.ListWorkloadsRequest) (*runnerv1.ListWorkloadsResponse, error) {
	return s.backend.ListWorkloads(ctx, req)
}

func (s *Server) GetWorkloadLabels(ctx context.Context, req *runnerv1.GetWorkloadLabelsRequest) (*runnerv1.GetWorkloadLabelsResponse, error) {
	return s.backend.GetWorkloadLabels(ctx, req)
}

func (s *Server) FindWorkloadsByLabels(ctx context.Context, req *runnerv1.FindWorkloadsByLabelsRequest) (*runnerv1.FindWorkloadsByLabelsResponse, error) {
	return s.backend.FindWorkloadsByLabels(ctx, req)
}

func (s *Server) ListWorkloadsByVolume(ctx context.Context, req *runnerv1.ListWorkloadsByVolumeRequest) (*runnerv1.ListWorkloadsByVolumeResponse, error) {
	return s.backend.ListWorkloadsByVolume(ctx, req)
}

func (s *Server) ListVolumes(ctx context.Context, req *runnerv1.ListVolumesRequest) (*runnerv1.ListVolumesResponse, error) {
	return s.backend.ListVolumes(ctx, req)
}

func (s *Server) RemoveVolume(ctx context.Context, req *runnerv1.RemoveVolumeRequest) (*runnerv1.RemoveVolumeResponse, error) {
	return s.backend.RemoveVolume(ctx, req)
}

func (s *Server) PutArchive(ctx context.Context, req *runnerv1.PutArchiveRequest) (*runnerv1.PutArchiveResponse, error) {
	return s.backend.PutArchive(ctx, req)
}

func (s *Server) StreamEvents(req *runnerv1.StreamEventsRequest, stream runnerv1.RunnerService_StreamEventsServer) error {
	return s.backend.StreamEvents(req, stream)
}
