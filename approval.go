// Package agentcore is the half of the agent that both execution shapes share:
// projecting the Gateway catalog into a tool surface, deciding what may run
// without asking a person, invoking, and recording what happened.
//
// It is deliberately transport-free and model-free. Mode B (an MCP server the
// CLI hosts) and mode A (a planning loop the module hosts) link the same
// package, which is how "the approval rules exist once" stops being a promise
// and becomes a fact about the build. A second copy would drift, and the drift
// would show up as a call the MCP surface allows and the CLI refuses.
package agentcore

import (
	"strings"
)

// Autonomy is how much a caller may do without asking a person.
type Autonomy string

const (
	// AutonomyPlan reads and proposes. Nothing that changes anything runs.
	AutonomyPlan Autonomy = "plan"
	// AutonomyAsk runs reads and asks about everything else.
	AutonomyAsk Autonomy = "ask"
	// AutonomyAuto also runs reversible writes.
	AutonomyAuto Autonomy = "auto"
	// AutonomyUnattended is the mode with nobody to ask. Until a pre-approval
	// list exists (A7) it runs reads only.
	AutonomyUnattended Autonomy = "unattended"
)

// KnownAutonomy reports whether name is one of the four modes.
func KnownAutonomy(name string) bool {
	switch Autonomy(name) {
	case AutonomyPlan, AutonomyAsk, AutonomyAuto, AutonomyUnattended:
		return true
	}
	return false
}

// Decision is what a caller should do with an operation.
type Decision string

const (
	// DecisionRun may proceed without asking.
	DecisionRun Decision = "run"
	// DecisionConfirm needs a person to say yes first.
	DecisionConfirm Decision = "confirm"
	// DecisionPlan means propose it and do not call.
	DecisionPlan Decision = "plan"
	// DecisionRefuse means not in this mode, and no amount of asking helps
	// because there is nobody to ask.
	DecisionRefuse Decision = "refuse"
)

// irreversibleActions are the declared side effects that cannot be walked back
// by calling something else. `create` and `update` are absent on purpose: they
// are what a reversible write looks like.
var irreversibleActions = map[string]bool{
	"delete":        true,
	"stop":          true,
	"publish":       true,
	"send":          true,
	"external-call": true,
}

// Policy is what a caller is allowed to decide for itself: how much runs
// without asking, and — for a session with nobody to ask — what a person
// already said yes to.
//
// The two travel together because in unattended mode neither answers alone.
// The mode says "there is no one here"; the list says "these were agreed in
// advance". Without the list the mode can only refuse, which is what it did
// before A7.
type Policy struct {
	Autonomy Autonomy
	// PreApproved are the operation ids a person named when the credential was
	// issued. The Gateway establishes it and reports it through whoami, so the
	// list a session runs under is the one that was actually granted rather
	// than one the session chose for itself.
	PreApproved []string
}

// preApproves reports whether this operation was named in advance.
func (p Policy) preApproves(operationID string) bool {
	for _, approved := range p.PreApproved {
		if approved == operationID {
			return true
		}
	}
	return false
}

// Judgement is a decision and the contract fact that produced it. The reason
// travels with the decision because a refusal a caller cannot explain is a
// refusal a caller will try to route around.
type Judgement struct {
	Decision Decision
	Reason   string
}

// Decide applies the approval table to one operation.
//
// Every input is a field the CONTRACT declares. There is no list of dangerous
// operation ids anywhere in this tree, and that is the point: a list goes stale
// the moment a module ships a new operation, and nothing makes that staleness
// visible. A missing declaration is read as dangerous — an operation that does
// not say what it does is not thereby safe.
func Decide(operation CatalogOperation, policy Policy) Judgement {
	reason, needsPerson := confirmationTrigger(operation)
	readOnly := isReadOnly(operation)

	switch policy.Autonomy {
	case AutonomyPlan:
		if readOnly {
			return Judgement{DecisionRun, "read-only"}
		}
		return Judgement{DecisionPlan, "plan mode proposes anything that changes something"}
	case AutonomyAsk:
		if readOnly {
			return Judgement{DecisionRun, "read-only"}
		}
		if needsPerson {
			return Judgement{DecisionConfirm, reason}
		}
		return Judgement{DecisionConfirm, "ask mode confirms every write"}
	case AutonomyAuto:
		if readOnly {
			return Judgement{DecisionRun, "read-only"}
		}
		if needsPerson {
			return Judgement{DecisionConfirm, reason}
		}
		return Judgement{DecisionRun, "reversible write"}
	case AutonomyUnattended:
		if readOnly {
			return Judgement{DecisionRun, "read-only"}
		}
		// 사전 승인은 사람이 발급 시점에 이 operation을 이름으로 지목한 것이다.
		// 계약이 "사람에게 물어야 한다"고 말한 것에도 적용된다 — 물어야 할
		// 사람이 이미 답했기 때문이고, 그 답은 자격에 박혀 서버가 안다.
		if policy.preApproves(operation.OperationID) {
			if needsPerson {
				return Judgement{DecisionRun, "pre-approved in advance (" + reason + ")"}
			}
			return Judgement{DecisionRun, "pre-approved in advance"}
		}
		if needsPerson {
			return Judgement{DecisionRefuse, reason}
		}
		// 목록 밖이면 중단한다(§13). 목록 없이 "되돌릴 수 있으니 괜찮다"로 열면,
		// 되돌릴 수 있는 쓰기가 사람 없이 반복되는 상태를 아무도 승인한 적이
		// 없게 된다.
		return Judgement{DecisionRefuse, "unattended mode runs only what was pre-approved; this operation was not"}
	}
	return Judgement{DecisionRefuse, "unknown autonomy mode"}
}

// isReadOnly reports whether an operation only observes. Both halves must say
// so: risk=read AND no side effect that is not itself a read.
func isReadOnly(operation CatalogOperation) bool {
	if operation.Execution == nil || operation.Execution.Risk != "read" {
		return false
	}
	for _, effect := range operation.SideEffects {
		if strings.TrimSpace(effect.Action) != "read" {
			return false
		}
	}
	return true
}

// confirmationTrigger returns why an operation needs a person, if it does.
func confirmationTrigger(operation CatalogOperation) (string, bool) {
	if operation.Execution == nil {
		return "the contract declares no execution policy", true
	}
	execution := operation.Execution
	switch execution.Risk {
	case "dangerous", "privileged":
		return "risk=" + execution.Risk, true
	case "read", "write":
	default:
		// 계약이 말하지 않은 위험도. 모르는 값은 위험한 쪽으로 읽는다.
		return "unknown risk " + quoted(execution.Risk), true
	}
	if execution.ConfirmationMode == "required" {
		return "the contract requires confirmation", true
	}
	for _, effect := range operation.SideEffects {
		if irreversibleActions[strings.TrimSpace(effect.Action)] {
			return "side effect " + quoted(effect.Action) + " cannot be undone by calling something else", true
		}
	}
	if execution.Risk == "write" && execution.IdempotencyMode == "none" {
		return "a write that cannot be safely repeated", true
	}
	return "", false
}

// AllowsRetry reports whether a failed call may be retried automatically.
//
// This is not part of the approval decision and must not be folded into it: an
// operation can be perfectly fine to run unattended and still be one that must
// never run twice. A model retrying a failed command because the failure "looked
// transient" is the most common way an agent does something twice that was only
// meant to happen once.
func AllowsRetry(operation CatalogOperation) bool {
	if operation.Execution == nil {
		return false
	}
	switch operation.Execution.RetryMode {
	case "never", "":
		return false
	case "idempotency-key-only":
		return operation.Execution.IdempotencyMode == "key-supported"
	}
	return true
}

func quoted(value string) string {
	if strings.TrimSpace(value) == "" {
		return `""`
	}
	return `"` + value + `"`
}
