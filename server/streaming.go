package server

import (
	"errors"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	runnerv1 "github.com/agynio/runner-core/gen/agynio/api/runner/v1"
)

func (s *Server) StreamWorkloadLogs(req *runnerv1.StreamWorkloadLogsRequest, stream runnerv1.RunnerService_StreamWorkloadLogsServer) error {
	workloadID := strings.TrimSpace(req.GetWorkloadId())
	if workloadID == "" {
		return status.Error(codes.InvalidArgument, "workload_id_required")
	}
	if strings.TrimSpace(req.GetContainerName()) == "" {
		return status.Error(codes.InvalidArgument, "container_name_required")
	}

	ctx := stream.Context()

	logStream, err := s.backend.OpenWorkloadLogs(ctx, req)
	if err != nil {
		return err
	}
	defer logStream.Close()

	const logReadBufferSize = 4096
	buf := make([]byte, logReadBufferSize)
	for {
		n, readErr := logStream.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			resp := &runnerv1.StreamWorkloadLogsResponse{
				Event: &runnerv1.StreamWorkloadLogsResponse_Chunk{
					Chunk: &runnerv1.LogChunk{
						Data: chunk,
						Ts:   timestamppb.New(time.Now().UTC()),
					},
				},
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			workloadAlive, err := s.backend.WorkloadExists(ctx, workloadID)
			if err != nil {
				return err
			}
			return logReadError(readErr, workloadAlive)
		}
	}
}

// logReadError tells a container that finished from a workload that was
// deleted mid-stream: the first closes cleanly, the second is Unavailable.
func logReadError(readErr error, workloadAlive bool) error {
	if !workloadAlive {
		return status.Error(codes.Unavailable, "pod_deleted")
	}
	if errors.Is(readErr, io.EOF) {
		return nil
	}
	return status.Error(codes.Internal, "logs_stream_error")
}
