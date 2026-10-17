package agent

import "context"

// CallTrace identifies one logical node. Call IDs and execution IDs are assigned
// by the gateway, so resumes/transport failures cannot overwrite earlier costs.
type CallTrace struct {
	RootJobID       string `json:"root_job_id,omitempty"`
	ExecutionID     string `json:"execution_id,omitempty"`
	NodeID          string `json:"node_id,omitempty"`
	AttemptID       string `json:"attempt_id,omitempty"`
	SharedTaskID    string `json:"shared_task_id,omitempty"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
}

type callTraceKey struct{}

func WithCallTrace(ctx context.Context, trace CallTrace) context.Context {
	previous := CallTraceFromContext(ctx)
	if trace.RootJobID == "" {
		trace.RootJobID = previous.RootJobID
	}
	if trace.ExecutionID == "" {
		trace.ExecutionID = previous.ExecutionID
	}
	if trace.SharedTaskID == "" {
		trace.SharedTaskID = previous.SharedTaskID
	}
	return context.WithValue(ctx, callTraceKey{}, trace)
}

func CallTraceFromContext(ctx context.Context) CallTrace {
	trace, _ := ctx.Value(callTraceKey{}).(CallTrace)
	return trace
}
