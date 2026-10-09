package agentcore

import "time"

// Record is one line of what an agent session did. It exists so "what did the
// agent actually do" has an answer that does not depend on the model's own
// account of it.
//
// Reason is the caller's stated intent and is the one field here that is NOT a
// fact: it is what the model said it was doing. It is recorded as a claim and
// never sent to the Gateway's audit trail, which only carries what the server
// itself established.
type Record struct {
	Time        time.Time `json:"time"`
	Tool        string    `json:"tool"`
	OperationID string    `json:"operationId,omitempty"`
	Decision    Decision  `json:"decision,omitempty"`
	Judgement   string    `json:"judgement,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	// TraceID is the Gateway's own id for the call. It is the join key: the
	// Gateway's audit line for the same call carries it too, so "what did the
	// agent do" and "what did the server allow" can be shown to be one event
	// rather than two accounts that happen to agree.
	TraceID   string `json:"traceId,omitempty"`
	Status    string `json:"status"`
	ErrorCode string `json:"errorCode,omitempty"`
	// External marks a call that left Terra through a registered MCP server
	// instead of through the Gateway. A host needs it to say the thing R8
	// (설계 §7.10) says must be said: there is no Gateway audit line for this
	// call, so this record is the only one there is.
	External bool `json:"external,omitempty"`
	// ResultBytes is how much an external server answered with, after the
	// host's cap. It is the size R8 asks the record to carry. A Gateway call
	// leaves it zero — there the trace id joins to a server-side line that
	// already knows.
	ResultBytes int `json:"resultBytes,omitempty"`
}

// Record statuses.
const (
	StatusOK      = "ok"
	StatusRefused = "refused"
	StatusError   = "error"
	// StatusPlanned is a call that was worked out and deliberately not made
	// (a dry run). It is the one status where "nothing happened" is the point.
	StatusPlanned = "planned"
)

// Recorder receives one Record per tool call. Implementations must be safe for
// concurrent use.
type Recorder interface {
	Record(record Record)
}

// RecorderFunc adapts a plain function to a Recorder.
type RecorderFunc func(Record)

// Record implements Recorder.
func (f RecorderFunc) Record(record Record) { f(record) }

// NopRecorder discards records. It is the default so that a host which has not
// decided where records live still gets a working agent.
type NopRecorder struct{}

// Record implements Recorder.
func (NopRecorder) Record(Record) {}
