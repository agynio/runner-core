// Package backend is the seam between the runner front this module provides
// and whatever hosts the workloads: Pods on the k8s-runner, microVMs on the
// k8s-microvm-runner.
//
// The shared gRPC server calls a Backend for every RPC. What it adds is what
// every runner needs the same way: the exec and log stream plumbing, Ready,
// and Unimplemented for hibernation unless the backend says it can.
package backend

import (
	"context"
	"io"

	runnerv1 "github.com/agynio/runner-core/gen/agynio/api/runner/v1"
	runnersv1 "github.com/agynio/runner-core/gen/agynio/api/runners/v1"
)

// Backend hosts workloads. A runner implements it for its own substrate and
// hands it to Run; the front does the rest.
type Backend interface {
	Workloads
	Exec
	Logs
	Events
	StateSource
}

// Hibernator is what a Backend implements when it can pause a workload and
// persist its state, then resume it later. It is optional: a backend without
// it answers HibernateWorkload and WakeWorkload with Unimplemented and never
// advertises the hibernate capability, which is what the k8s-runner does.
type Hibernator interface {
	HibernateWorkload(ctx context.Context, req *runnerv1.HibernateWorkloadRequest) (*runnerv1.HibernateWorkloadResponse, error)
	WakeWorkload(ctx context.Context, req *runnerv1.WakeWorkloadRequest) (*runnerv1.WakeWorkloadResponse, error)
}

// Workloads is the unary surface of the Runner API. Requests arrive as the
// platform sent them and the backend answers in kind, including the gRPC
// status it wants the caller to see.
type Workloads interface {
	StartWorkload(ctx context.Context, req *runnerv1.StartWorkloadRequest) (*runnerv1.StartWorkloadResponse, error)
	StopWorkload(ctx context.Context, req *runnerv1.StopWorkloadRequest) (*runnerv1.StopWorkloadResponse, error)
	RemoveWorkload(ctx context.Context, req *runnerv1.RemoveWorkloadRequest) (*runnerv1.RemoveWorkloadResponse, error)
	InspectWorkload(ctx context.Context, req *runnerv1.InspectWorkloadRequest) (*runnerv1.InspectWorkloadResponse, error)
	TouchWorkload(ctx context.Context, req *runnerv1.TouchWorkloadRequest) (*runnerv1.TouchWorkloadResponse, error)
	ListWorkloads(ctx context.Context, req *runnerv1.ListWorkloadsRequest) (*runnerv1.ListWorkloadsResponse, error)
	GetWorkloadLabels(ctx context.Context, req *runnerv1.GetWorkloadLabelsRequest) (*runnerv1.GetWorkloadLabelsResponse, error)
	FindWorkloadsByLabels(ctx context.Context, req *runnerv1.FindWorkloadsByLabelsRequest) (*runnerv1.FindWorkloadsByLabelsResponse, error)
	ListWorkloadsByVolume(ctx context.Context, req *runnerv1.ListWorkloadsByVolumeRequest) (*runnerv1.ListWorkloadsByVolumeResponse, error)
	ListVolumes(ctx context.Context, req *runnerv1.ListVolumesRequest) (*runnerv1.ListVolumesResponse, error)
	RemoveVolume(ctx context.Context, req *runnerv1.RemoveVolumeRequest) (*runnerv1.RemoveVolumeResponse, error)
	PutArchive(ctx context.Context, req *runnerv1.PutArchiveRequest) (*runnerv1.PutArchiveResponse, error)
}

// Exec readies commands to run inside a workload. The server owns the session
// around it: the stream protocol, timeouts, cancellation and the exit event.
type Exec interface {
	// PrepareExec resolves targetID -- the main container id a start returned
	// -- and readies command to run in it with or without a TTY. An error
	// here reaches the caller as the reason the exec could not start, with
	// the error's text as the message.
	PrepareExec(ctx context.Context, targetID string, command []string, tty bool) (Execution, error)
}

// Execution is one prepared command.
type Execution interface {
	// Run starts the command wired to streams and returns when it exits. A
	// command that ran and exited non-zero is reported as an ExitError; any
	// other error is a failure of the runner itself, and a context error is
	// a cancellation.
	Run(ctx context.Context, streams ExecStreams) error
}

// ExitError is how an Execution reports a command that ran and exited
// non-zero. Any error that exposes ExitStatus qualifies, so a backend may
// return its substrate's own type.
type ExitError interface {
	error
	ExitStatus() int
}

// ExecStreams is what an Execution is wired to.
type ExecStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	// Stderr is nil in TTY mode, where the terminal merges it into stdout.
	Stderr io.Writer
	TTY    bool
	// Resize delivers terminal resizes in TTY mode. The server closes it
	// when the session ends.
	Resize <-chan TerminalSize
}

// TerminalSize is a terminal's dimensions in characters.
type TerminalSize struct {
	Width  uint16
	Height uint16
}

// Logs opens container log streams; the server reads them onto the wire.
type Logs interface {
	// OpenWorkloadLogs opens the stream the request asks for, with the
	// request's tail, since and follow options applied. An error is the gRPC
	// status the caller sees.
	OpenWorkloadLogs(ctx context.Context, req *runnerv1.StreamWorkloadLogsRequest) (io.ReadCloser, error)
	// WorkloadExists says whether the workload is still present. The server
	// asks when a log stream ends or fails, to tell a container that finished
	// from a workload that was deleted mid-stream. An error is the gRPC status
	// the caller sees.
	WorkloadExists(ctx context.Context, workloadID string) (bool, error)
}

// Events streams runtime events. What an event is and how it is filtered is
// the substrate's own vocabulary, so the backend owns the stream.
type Events interface {
	StreamEvents(req *runnerv1.StreamEventsRequest, stream runnerv1.RunnerService_StreamEventsServer) error
}

// StateSource watches the workloads a backend hosts and tells an Observer
// what it sees, as it sees it.
type StateSource interface {
	// WatchWorkloadState runs until ctx ends, handing every observed
	// transition to observer. It reports observed runtime state only, never
	// desired state: a deleted workload is forgotten, not reported stopped,
	// since ending a workload is the platform's decision.
	WatchWorkloadState(ctx context.Context, observer Observer) error
}

// Observer receives what a StateSource sees. The runner front's reporter
// implements it: it sends each change to the platform and suppresses repeats.
type Observer interface {
	Observe(ctx context.Context, observation Observation)
	// Forget drops what was last reported for a workload, so one restarted
	// under the same id reports again from scratch.
	Forget(workloadID string)
}

// Observation is one workload's observed runtime state. The status is passed
// to the platform unchanged, so a backend that hibernates reports the
// sleeping statuses through the same path.
type Observation struct {
	WorkloadID string
	Status     runnersv1.WorkloadStatus
	// SnapshotSizeBytes is the size of the persisted state a hibernated
	// workload occupies, which the platform meters. Set with the hibernated
	// status by a backend that hibernates; nil otherwise.
	SnapshotSizeBytes *int64
}
