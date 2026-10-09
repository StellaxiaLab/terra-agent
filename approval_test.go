package agentcore

import (
	"strings"
	"testing"
)

func operation(risk, confirmation, idempotency, retry string, actions ...string) CatalogOperation {
	entry := CatalogOperation{
		OperationID: "io.example.op",
		Execution: &CatalogExecution{
			Risk: risk, ConfirmationMode: confirmation,
			IdempotencyMode: idempotency, RetryMode: retry,
		},
	}
	for _, action := range actions {
		entry.SideEffects = append(entry.SideEffects, CatalogSideEffect{Action: action})
	}
	return entry
}

// 설계 §6.3 결정표를 그대로 옮긴다. 이 표가 정책의 정본이고, 코드는 그 함수다.
func TestApprovalTableMatchesTheDesign(t *testing.T) {
	for _, row := range []struct {
		name                        string
		operation                   CatalogOperation
		plan, ask, auto, unattended Decision
	}{
		{
			"read-only",
			operation("read", "none", "safe", "safe-only", "read"),
			DecisionRun, DecisionRun, DecisionRun, DecisionRun,
		},
		{
			"reversible write",
			operation("write", "none", "key-supported", "idempotency-key-only", "update"),
			DecisionPlan, DecisionConfirm, DecisionRun, DecisionRefuse,
		},
		{
			"write that cannot be repeated",
			operation("write", "none", "none", "never", "create"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"dangerous",
			operation("dangerous", "none", "safe", "safe-only", "update"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"privileged",
			operation("privileged", "none", "safe", "safe-only", "update"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"contract requires confirmation",
			operation("write", "required", "key-supported", "safe-only", "update"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"irreversible side effect",
			operation("write", "none", "key-supported", "safe-only", "delete"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"no execution policy declared",
			CatalogOperation{OperationID: "io.example.silent"},
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
		{
			"risk this build does not know",
			operation("catastrophic", "none", "safe", "safe-only"),
			DecisionPlan, DecisionConfirm, DecisionConfirm, DecisionRefuse,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, testCase := range []struct {
				autonomy Autonomy
				want     Decision
			}{
				{AutonomyPlan, row.plan},
				{AutonomyAsk, row.ask},
				{AutonomyAuto, row.auto},
				{AutonomyUnattended, row.unattended},
			} {
				got := Decide(row.operation, Policy{Autonomy: testCase.autonomy})
				if got.Decision != testCase.want {
					t.Errorf("%s: got %q (%s), want %q", testCase.autonomy, got.Decision, got.Reason, testCase.want)
				}
				if got.Decision != DecisionRun && got.Reason == "" {
					t.Errorf("%s: a refusal with no reason is one a caller will route around", testCase.autonomy)
				}
			}
		})
	}
}

// risk=read인데 부작용이 쓰기면 읽기 전용이 아니다. 두 선언이 어긋나면 넓은
// 쪽을 믿는다 — 계약이 스스로 모순될 때 좁은 쪽을 믿으면 그 모순이 통과한다.
func TestRiskReadWithAWriteSideEffectIsNotReadOnly(t *testing.T) {
	contradictory := operation("read", "none", "safe", "safe-only", "delete")
	if Decide(contradictory, Policy{Autonomy: AutonomyAuto}).Decision == DecisionRun {
		t.Fatal("an operation declaring risk=read and a delete side effect ran unattended")
	}
}

// 재시도는 승인과 다른 축이다. 무인으로 돌려도 되는 operation이 두 번 돌면 안 되는
// operation일 수 있다.
func TestRetryIsNotFoldedIntoApproval(t *testing.T) {
	safe := operation("write", "none", "key-supported", "idempotency-key-only", "update")
	if !AllowsRetry(safe) {
		t.Fatal("an idempotency-key operation with key support should be retryable")
	}
	if Decide(safe, Policy{Autonomy: AutonomyAuto}).Decision != DecisionRun {
		t.Fatal("a reversible write should run in auto")
	}

	never := operation("write", "none", "key-supported", "never", "update")
	if AllowsRetry(never) {
		t.Fatal("retry.mode=never was retried")
	}
	if Decide(never, Policy{Autonomy: AutonomyAuto}).Decision != DecisionRun {
		t.Fatal("retry.mode=never should not by itself demand approval")
	}

	// 키를 지원하지 않는데 idempotency-key-only면 재시도할 방법이 없다.
	unkeyed := operation("write", "none", "none", "idempotency-key-only", "update")
	if AllowsRetry(unkeyed) {
		t.Fatal("idempotency-key-only without key support was retried")
	}
	if AllowsRetry(CatalogOperation{}) {
		t.Fatal("an operation with no execution policy must not be retried")
	}
}

// A7 — unattended: what a person said yes to in advance is the only thing that
// runs when there is nobody to ask.
func TestUnattendedRunsOnlyWhatWasPreApproved(t *testing.T) {
	reversible := operation("write", "none", "idempotent", "safe-only", "update")
	reversible.OperationID = "terra.daemon.modules.by-module-id.restart.post"
	dangerous := operation("dangerous", "none", "none", "never", "delete")
	dangerous.OperationID = "terra.daemon.commands.execute.post"
	read := operation("read", "none", "safe", "safe-only", "read")
	read.OperationID = "terra.daemon.status.get"

	unattended := Policy{Autonomy: AutonomyUnattended}
	// Before anything is pre-approved the mode can only read.
	if got := Decide(read, unattended); got.Decision != DecisionRun {
		t.Fatalf("read = %+v", got)
	}
	if got := Decide(reversible, unattended); got.Decision != DecisionRefuse {
		t.Fatalf("an unlisted write = %+v, want refuse", got)
	}
	if got := Decide(reversible, unattended); !strings.Contains(got.Reason, "pre-approved") {
		t.Fatalf("the refusal does not say what would have let it run: %q", got.Reason)
	}

	// Named in advance, it runs — and only it.
	listed := Policy{Autonomy: AutonomyUnattended, PreApproved: []string{reversible.OperationID}}
	if got := Decide(reversible, listed); got.Decision != DecisionRun {
		t.Fatalf("a pre-approved write = %+v, want run", got)
	}
	if got := Decide(dangerous, listed); got.Decision != DecisionRefuse {
		t.Fatalf("an unlisted dangerous operation = %+v, want refuse", got)
	}
}

// Pre-approval is consent, so it answers the question the contract asks. A
// person who named a dangerous operation in advance IS the person it wanted.
func TestPreApprovalAnswersTheContractsQuestion(t *testing.T) {
	dangerous := operation("dangerous", "required", "none", "never", "delete")
	dangerous.OperationID = "terra.daemon.commands.execute.post"

	listed := Policy{Autonomy: AutonomyUnattended, PreApproved: []string{dangerous.OperationID}}
	got := Decide(dangerous, listed)
	if got.Decision != DecisionRun {
		t.Fatalf("decision = %+v", got)
	}
	// And the record still says WHY a person was wanted, so the line reads as
	// "this needed asking and was answered", not "this needed nothing".
	if !strings.Contains(got.Reason, "dangerous") {
		t.Fatalf("the reason loses the contract's own fact: %q", got.Reason)
	}
}

// A list belongs to a mode. With a person present the answer is asked for, so
// pre-approval must not quietly widen ask or auto.
func TestPreApprovalDoesNotLeakIntoTheAttendedModes(t *testing.T) {
	dangerous := operation("dangerous", "required", "none", "never", "delete")
	dangerous.OperationID = "terra.daemon.commands.execute.post"
	for _, autonomy := range []Autonomy{AutonomyPlan, AutonomyAsk, AutonomyAuto} {
		policy := Policy{Autonomy: autonomy, PreApproved: []string{dangerous.OperationID}}
		if got := Decide(dangerous, policy); got.Decision == DecisionRun {
			t.Fatalf("%s ran a dangerous operation because a list existed: %+v", autonomy, got)
		}
	}
}
