package agentcore

import (
	"context"
	"encoding/json"
)

// ApprovalRequest is what a person is asked before a call the approval table
// marked DecisionConfirm is made. Everything in it is either a contract fact
// (Operation, Judgement) or the model's own claim (Reason, Input) — and the
// asker should show which is which, because a person approving "restart the
// dead module" is approving the model's description of the call, not the call.
type ApprovalRequest struct {
	OperationID string
	Input       json.RawMessage
	// Reason is what the model said it was doing. A claim, not a fact.
	Reason    string
	Judgement Judgement
	Operation CatalogOperation
}

// Approver puts an ApprovalRequest in front of a person and reports the answer.
//
// It is the seam between the shared gate and the two execution shapes. Mode A
// (the module) implements it as a session that waits for `terra agent approve`
// or a typed "approve"; mode B (an MCP host) has no implementation at all,
// because the host's own permission prompt is not Terra's gate (§7.5) — an
// Approver that answered "yes" on the host's behalf would be exactly the second
// gate the design refuses to have.
//
// Approve blocks until the person answers or ctx ends. An error means the
// question could not be put or was not answered (a timeout, a cancelled
// session); it is reported to the model as a refusal, never as a yes.
type Approver interface {
	Approve(ctx context.Context, request ApprovalRequest) (approved bool, err error)
}

// ApproverFunc adapts a plain function to an Approver.
type ApproverFunc func(ctx context.Context, request ApprovalRequest) (bool, error)

// Approve implements Approver.
func (f ApproverFunc) Approve(ctx context.Context, request ApprovalRequest) (bool, error) {
	return f(ctx, request)
}
