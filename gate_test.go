package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The three seams mode A needs from the shared gate, each pinned against the
// transport: a person who can be asked, a run that plans and never calls, and
// a repeat that the contract forbids.

// reversibleWrite runs without asking in auto mode (a repeatable write with an
// undoable side effect) and yet must never be retried by a machine — the two
// properties the approval table keeps apart (TestRetryIsNotFoldedIntoApproval).
const reversibleWrite = `{
	"operationId": "terra.daemon.modules.by-module-id.restart.post",
	"permissions": ["node.control"],
	"execution": {"risk": "write", "idempotencyMode": "idempotent", "retryMode": "never"},
	"output": {"mode": "immediate"},
	"sideEffects": [{"action": "update"}]
}`

func invokeCall(operationID, input string) json.RawMessage {
	return json.RawMessage(`{"operationId":"` + operationID + `","input":` + input + `,"reason":"test"}`)
}

// 승인 판정이 confirm이면 Approver에게 묻고, 그 답이 호출을 정한다. Approver가
// 없으면 종전대로 거절한다 — MCP 호스트에는 물을 사람이 없다(§7.5).
func TestApproverDecidesAConfirmCall(t *testing.T) {
	const dangerous = "terra.daemon.commands.execute.post"
	responses := map[string]string{
		"GET /api/v1/catalog/operations/" + dangerous:      dangerousOperation,
		"POST /api/v1/operations/" + dangerous + "/invoke": `{"task_id":"t-1"}`,
	}

	t.Run("approved", func(t *testing.T) {
		transport := &fakeTransport{responses: responses}
		var asked ApprovalRequest
		agent := New(transport, Options{Autonomy: AutonomyAsk, Approver: ApproverFunc(
			func(_ context.Context, request ApprovalRequest) (bool, error) { asked = request; return true, nil },
		)})
		result := agent.Call(context.Background(), ToolInvoke, invokeCall(dangerous, `{"command":"ls"}`))
		if result.Err != nil {
			t.Fatalf("approved call failed: %v", result.Err)
		}
		if !transport.called("POST /api/v1/operations/" + dangerous) {
			t.Fatal("an approved call never reached the Gateway")
		}
		if asked.OperationID != dangerous || asked.Reason != "test" || asked.Judgement.Decision != DecisionConfirm {
			t.Fatalf("the person was asked the wrong question: %+v", asked)
		}
		if !strings.Contains(string(asked.Input), `"ls"`) {
			t.Fatalf("the person did not see the input: %s", asked.Input)
		}
	})

	t.Run("declined", func(t *testing.T) {
		transport := &fakeTransport{responses: responses}
		agent, records := newAgent(t, AutonomyAsk, transport)
		agent.approver = ApproverFunc(func(context.Context, ApprovalRequest) (bool, error) { return false, nil })
		result := agent.Call(context.Background(), ToolInvoke, invokeCall(dangerous, `{"command":"ls"}`))
		if result.Err == nil || result.Err.Code != CodeApprovalDenied {
			t.Fatalf("result = %+v, want %s", result, CodeApprovalDenied)
		}
		if transport.called("POST /api/v1/operations") {
			t.Fatal("a declined call reached the Gateway")
		}
		if (*records)[0].Status != StatusRefused || (*records)[0].Decision != DecisionConfirm {
			t.Fatalf("record = %+v", (*records)[0])
		}
	})

	t.Run("unanswered", func(t *testing.T) {
		transport := &fakeTransport{responses: responses}
		agent, _ := newAgent(t, AutonomyAsk, transport)
		agent.approver = ApproverFunc(func(context.Context, ApprovalRequest) (bool, error) {
			return true, errors.New("session cancelled while waiting")
		})
		// A yes that comes with an error is not a yes.
		result := agent.Call(context.Background(), ToolInvoke, invokeCall(dangerous, `{}`))
		if result.Err == nil || result.Err.Code != CodeApprovalRequired {
			t.Fatalf("result = %+v, want %s", result, CodeApprovalRequired)
		}
		if transport.called("POST /api/v1/operations") {
			t.Fatal("an unanswered call reached the Gateway")
		}
	})

	t.Run("nobody to ask", func(t *testing.T) {
		transport := &fakeTransport{responses: responses}
		agent, _ := newAgent(t, AutonomyAsk, transport)
		result := agent.Call(context.Background(), ToolInvoke, invokeCall(dangerous, `{}`))
		if result.Err == nil || result.Err.Code != CodeApprovalRequired {
			t.Fatalf("result = %+v, want %s", result, CodeApprovalRequired)
		}
	})
}

// 계획 모드는 Approver가 있어도 묻지 않는다 — 제안만 한다. 읽기는 묻지 않고 부른다.
func TestApproverIsNotAskedWhenTheTableDoesNotSayConfirm(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/terra.daemon.commands.execute.post": dangerousOperation,
		"GET /api/v1/catalog/operations/terra.daemon.status.get":            readOperation,
		"POST /api/v1/operations/terra.daemon.status.get/invoke":            `{"status":"ok"}`,
	}}
	asked := 0
	agent := New(transport, Options{Autonomy: AutonomyPlan, Approver: ApproverFunc(
		func(context.Context, ApprovalRequest) (bool, error) { asked++; return true, nil },
	)})
	if result := agent.Call(context.Background(), ToolInvoke,
		invokeCall("terra.daemon.commands.execute.post", `{}`)); result.Err == nil || result.Err.Code != CodeApprovalRequired {
		t.Fatalf("plan mode called something: %+v", result)
	}
	if result := agent.Call(context.Background(), ToolInvoke,
		invokeCall("terra.daemon.status.get", `{}`)); result.Err != nil {
		t.Fatalf("a read was refused in plan mode: %v", result.Err)
	}
	if asked != 0 {
		t.Fatalf("the approver was asked %d time(s) in plan mode", asked)
	}
}

// dry run: 카탈로그는 읽되 invoke는 하나도 Gateway에 닿지 않는다. 판정은 그대로
// 계산되어 계획에 실린다.
func TestDryRunNeverReachesTheGateway(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/terra.daemon.commands.execute.post":             dangerousOperation,
		"GET /api/v1/catalog/operations/terra.daemon.status.get":                        readOperation,
		"GET /api/v1/catalog/operations/terra.daemon.modules.by-module-id.restart.post": reversibleWrite,
		"POST /api/v1/operations/terra.daemon.status.get/invoke":                        `{"status":"ok"}`,
		"POST /api/v1/operations/terra.daemon.modules.by-module-id.restart.post/invoke": `{"accepted":true}`,
		"POST /api/v1/operations/terra.daemon.commands.execute.post/invoke":             `{"task_id":"t"}`,
		"GET /api/v1/catalog": `{"operations":[]}`,
	}}
	records := &[]Record{}
	asked := 0
	agent := New(transport, Options{
		Autonomy: AutonomyAuto, DryRun: true,
		Recorder: RecorderFunc(func(record Record) { *records = append(*records, record) }),
		Approver: ApproverFunc(func(context.Context, ApprovalRequest) (bool, error) { asked++; return true, nil }),
	})
	if !agent.DryRun() {
		t.Fatal("DryRun() is false")
	}

	expected := map[string]Decision{
		"terra.daemon.status.get":                        DecisionRun,
		"terra.daemon.modules.by-module-id.restart.post": DecisionRun,
		"terra.daemon.commands.execute.post":             DecisionConfirm,
	}
	for operationID, decision := range expected {
		result := agent.Call(context.Background(), ToolInvoke, invokeCall(operationID, `{"a":1}`))
		if result.Err != nil {
			t.Fatalf("%s: dry run answered an error: %v", operationID, result.Err)
		}
		var plan struct {
			DryRun      bool            `json:"dryRun"`
			OperationID string          `json:"operationId"`
			Input       json.RawMessage `json:"input"`
			Approval    Decision        `json:"approval"`
			WouldRun    bool            `json:"wouldRun"`
		}
		if err := json.Unmarshal(result.Payload, &plan); err != nil {
			t.Fatal(err)
		}
		if !plan.DryRun || plan.OperationID != operationID || plan.Approval != decision || plan.WouldRun != (decision == DecisionRun) {
			t.Fatalf("%s: plan = %+v, want decision %s", operationID, plan, decision)
		}
		if string(plan.Input) != `{"a":1}` {
			t.Fatalf("%s: the plan lost the input: %s", operationID, plan.Input)
		}
	}
	// The catalog was read (a plan needs the contract); the search too.
	if result := agent.Call(context.Background(), ToolSearch, json.RawMessage(`{"limit":5}`)); result.Err != nil {
		t.Fatalf("search failed in dry run: %v", result.Err)
	}
	if !transport.called("GET /api/v1/catalog") {
		t.Fatal("dry run did not read the catalog")
	}
	if transport.called("POST /api/v1/operations") {
		t.Fatalf("dry run reached an invoke: %v", transport.seen)
	}
	if asked != 0 {
		t.Fatal("dry run asked a person")
	}
	planned := 0
	for _, record := range *records {
		if record.Status == StatusPlanned {
			planned++
		}
	}
	if planned != len(expected) {
		t.Fatalf("planned records = %d, want %d: %+v", planned, len(expected), *records)
	}
}

// retry.mode=never인 operation은 되풀이하지 않는다 — 앞선 시도가 Gateway에 닿은
// 뒤 실패했다면. 닿기 전에 거절된(4xx) 것은 실행되지 않았으니 되풀이해도 된다.
func TestARepeatOfANeverRetryCallIsRefusedAfterAnAmbiguousFailure(t *testing.T) {
	const restart = "terra.daemon.modules.by-module-id.restart.post"
	invokePath := "POST /api/v1/operations/" + restart + "/invoke"
	transport := &fakeTransport{
		responses: map[string]string{
			"GET /api/v1/catalog/operations/" + restart: reversibleWrite,
			invokePath: `{"error":{"code":"UPSTREAM_TIMEOUT","message":"no answer"}}`,
		},
		status: map[string]int{invokePath: 504},
	}
	agent, records := newAgent(t, AutonomyAuto, transport)
	input := `{"module_id":"io.terra.sample","b":2}`

	first := agent.Call(context.Background(), ToolInvoke, invokeCall(restart, input))
	if first.Err == nil || first.Err.Code != "UPSTREAM_TIMEOUT" {
		t.Fatalf("first = %+v", first)
	}
	// Same call, keys in another order: still the same call.
	second := agent.Call(context.Background(), ToolInvoke, invokeCall(restart, `{"b":2,"module_id":"io.terra.sample"}`))
	if second.Err == nil || second.Err.Code != CodeRetryRefused {
		t.Fatalf("second = %+v, want %s", second, CodeRetryRefused)
	}
	if !strings.Contains(second.Err.Message, "UPSTREAM_TIMEOUT") {
		t.Fatalf("the refusal does not name the earlier failure: %s", second.Err.Message)
	}
	invokes := 0
	for _, key := range transport.seen {
		if key == invokePath {
			invokes++
		}
	}
	if invokes != 1 {
		t.Fatalf("the Gateway saw %d invokes, want 1", invokes)
	}
	if last := (*records)[len(*records)-1]; last.Status != StatusRefused || last.ErrorCode != CodeRetryRefused {
		t.Fatalf("record = %+v", last)
	}

	// A different input is a different call.
	if third := agent.Call(context.Background(), ToolInvoke, invokeCall(restart, `{"module_id":"io.terra.other"}`)); third.Err == nil || third.Err.Code != "UPSTREAM_TIMEOUT" {
		t.Fatalf("third = %+v", third)
	}

	// A refusal before dispatch (403) did not run anything: repeating it is allowed.
	transport.status[invokePath] = 403
	transport.responses[invokePath] = `{"error":{"code":"MODULE_PERMISSION_DENIED","message":"no"}}`
	transport.seen = nil
	agent2, _ := newAgent(t, AutonomyAuto, transport)
	for attempt := 0; attempt < 2; attempt++ {
		if result := agent2.Call(context.Background(), ToolInvoke, invokeCall(restart, input)); result.Err == nil || result.Err.Code != "MODULE_PERMISSION_DENIED" {
			t.Fatalf("attempt %d = %+v", attempt, result)
		}
	}
	if len(transport.seen) < 4 {
		t.Fatalf("a refused-before-dispatch call was not allowed to repeat: %v", transport.seen)
	}
}

// 계약이 되풀이를 허락하면(retry.mode=safe-only 읽기) 막지 않는다.
func TestARetryableCallMayRepeatAfterAFailure(t *testing.T) {
	const status = "terra.daemon.status.get"
	invokePath := "POST /api/v1/operations/" + status + "/invoke"
	transport := &fakeTransport{
		responses: map[string]string{
			"GET /api/v1/catalog/operations/" + status: readOperation,
			invokePath: `{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"down"}}`,
		},
		status: map[string]int{invokePath: 503},
	}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	for attempt := 0; attempt < 2; attempt++ {
		if result := agent.Call(context.Background(), ToolInvoke, invokeCall(status, `{}`)); result.Err == nil || result.Err.Code != "UPSTREAM_UNAVAILABLE" {
			t.Fatalf("attempt %d = %+v", attempt, result)
		}
	}
	// Then it comes back, and the success clears the memory for good measure.
	transport.status[invokePath] = 200
	transport.responses[invokePath] = `{"status":"ok"}`
	if result := agent.Call(context.Background(), ToolInvoke, invokeCall(status, `{}`)); result.Err != nil {
		t.Fatalf("recovered call failed: %v", result.Err)
	}
}

// The trace id is the join key: what the Gateway answered with has to be what
// the record carries, or the agent's account of itself cannot be checked
// against the server's (A5, 설계 §12).
func TestTheTraceIdReachesTheRecordAndTheModel(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/terra.daemon.status.get": readOperation,
		"POST /api/v1/operations/terra.daemon.status.get/invoke": `{"ok":true}`,
	}}
	agent, records := newAgent(t, AutonomyAuto, transport)

	result := agent.Call(context.Background(), ToolInvoke, json.RawMessage(`{"operationId":"terra.daemon.status.get"}`))
	if result.Err != nil {
		t.Fatalf("invoke: %+v", result.Err)
	}
	want := traceFor("POST /api/v1/operations/terra.daemon.status.get/invoke")
	if result.TraceID != want {
		t.Fatalf("result trace = %q, want %q", result.TraceID, want)
	}
	if len(*records) != 1 || (*records)[0].TraceID != want {
		t.Fatalf("record = %+v, want the gateway's id for the call", *records)
	}
	// The model sees it too, so an answer that reports a call can quote the id
	// an operator would look up.
	var payload map[string]any
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["traceId"] != want {
		t.Fatalf("payload = %v", payload)
	}
}

// A refusal is the case that most needs looking up. When the Gateway's error
// envelope omits the id, the one the transport saw stands in — a refusal
// nobody can find in the audit trail is a refusal nobody can act on.
func TestARefusalWithoutATraceIdInItsEnvelopeStillCarriesOne(t *testing.T) {
	const path = "POST /api/v1/operations/terra.daemon.status.get/invoke"
	transport := &fakeTransport{
		responses: map[string]string{
			"GET /api/v1/catalog/operations/terra.daemon.status.get": readOperation,
			path: `{"error":{"code":"MODULE_PERMISSION_DENIED","message":"no"}}`,
		},
		status: map[string]int{path: 403},
	}
	agent, records := newAgent(t, AutonomyAuto, transport)

	result := agent.Call(context.Background(), ToolInvoke, json.RawMessage(`{"operationId":"terra.daemon.status.get"}`))
	if result.Err == nil || result.Err.Code != "MODULE_PERMISSION_DENIED" {
		t.Fatalf("result = %+v", result)
	}
	want := traceFor(path)
	if result.Err.TraceID != want {
		t.Fatalf("refusal trace = %q, want %q", result.Err.TraceID, want)
	}
	if len(*records) != 1 || (*records)[0].TraceID != want {
		t.Fatalf("record = %+v", *records)
	}
}

// An envelope that names its own id keeps it — the Gateway is the authority on
// what it called the call, and the header is only the fallback.
func TestAnEnvelopeTraceIdWins(t *testing.T) {
	const path = "POST /api/v1/operations/terra.daemon.status.get/invoke"
	transport := &fakeTransport{
		responses: map[string]string{
			"GET /api/v1/catalog/operations/terra.daemon.status.get": readOperation,
			path: `{"error":{"code":"MODULE_PERMISSION_DENIED","message":"no","traceId":"trace-from-envelope"}}`,
		},
		status: map[string]int{path: 403},
	}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolInvoke, json.RawMessage(`{"operationId":"terra.daemon.status.get"}`))
	if result.Err == nil || result.Err.TraceID != "trace-from-envelope" {
		t.Fatalf("result = %+v", result)
	}
}

// A6 — fleet. The fifth tool is offered only to a credential that can actually
// reach another node: a tool that always fails is worse than one not offered.

const localWhoami = `{"principal":"alice","permissions":["node.read"],"delegate":"agent:agn_1","reach":"local","reachLimited":true}`
const clusterWhoami = `{"principal":"alice","permissions":["node.read"],"delegate":"agent:agn_1","reach":"cluster","reachLimited":true}`
const personWhoami = `{"principal":"alice","permissions":["node.read"],"reachLimited":false}`

func toolNames(tools []Tool) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return strings.Join(names, ",")
}

func TestTheFleetToolIsOfferedOnlyWhenTheCredentialReaches(t *testing.T) {
	for _, row := range []struct {
		name    string
		whoami  string
		offered bool
	}{
		{"a local grant does not see the fleet", localWhoami, false},
		{"a cluster grant does", clusterWhoami, true},
		{"a person's own session is not reach-limited", personWhoami, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			transport := &fakeTransport{responses: map[string]string{"GET /api/v1/agent/whoami": row.whoami}}
			agent, _ := newAgent(t, AutonomyAuto, transport)
			tools := agent.ToolsFor(context.Background())
			if got := strings.Contains(toolNames(tools), ToolNodes); got != row.offered {
				t.Fatalf("tools = %s, terra_nodes offered = %v, want %v", toolNames(tools), got, row.offered)
			}
			// The four are always there, whatever the credential.
			if len(tools) != len(Tools())+map[bool]int{true: 1, false: 0}[row.offered] {
				t.Fatalf("tools = %s", toolNames(tools))
			}
		})
	}
}

// Reach is frozen when a grant is issued, so it is asked once.
func TestReachIsAskedOnce(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/agent/whoami": clusterWhoami,
		"GET /api/v1/agent/nodes":  `{"count":1,"nodes":[{"nodeId":"node-b","displayName":"B","status":"online"}]}`,
	}}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	agent.ToolsFor(context.Background())
	agent.ToolsFor(context.Background())
	agent.Call(context.Background(), ToolNodes, nil)

	asked := 0
	for _, seen := range transport.seen {
		if seen == "GET /api/v1/agent/whoami" {
			asked++
		}
	}
	if asked != 1 {
		t.Fatalf("whoami asked %d times, want once", asked)
	}
}

// Asking for the fleet without the reach for it is refused here, before the
// Gateway — the tool was never offered, so the call is the model reaching for
// something it was not given.
func TestTheFleetToolRefusesALocalCredential(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{"GET /api/v1/agent/whoami": localWhoami}}
	agent, records := newAgent(t, AutonomyAuto, transport)

	result := agent.Call(context.Background(), ToolNodes, nil)
	if result.Err == nil || result.Err.Code != CodeReachExceeded {
		t.Fatalf("result = %+v, want %s", result, CodeReachExceeded)
	}
	if transport.called("GET /api/v1/agent/nodes") {
		t.Fatal("a refused fleet call still reached the Gateway")
	}
	if len(*records) != 1 || (*records)[0].Tool != ToolNodes {
		t.Fatalf("records = %+v", *records)
	}
}

func TestTheFleetToolAnswersTheNodeVocabulary(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/agent/whoami": clusterWhoami,
		"GET /api/v1/agent/nodes": `{"count":2,"nodes":[
			{"nodeId":"node-a","displayName":"A","status":"online","roles":["leaf"]},
			{"nodeId":"node-b","displayName":"B","status":"offline"}]}`,
	}}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolNodes, nil)
	if result.Err != nil {
		t.Fatalf("result = %+v", result.Err)
	}
	var payload struct {
		Count int `json:"count"`
		Nodes []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 2 || payload.Nodes[0].NodeID != "node-a" || payload.Nodes[1].Status != "offline" {
		t.Fatalf("payload = %+v", payload)
	}
}

// The list a session runs under is the one the Gateway says was granted, not
// one the session chose for itself (A7).
const unattendedWhoami = `{"principal":"alice","permissions":["node.control"],"delegate":"agent:agn_1",
	"reach":"local","reachLimited":true,"unattended":true,
	"preApproved":["terra.daemon.modules.by-module-id.restart.post"]}`

func TestUnattendedRunsThePreApprovedCallAndNothingElse(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/agent/whoami": unattendedWhoami,
		"GET /api/v1/catalog/operations/terra.daemon.modules.by-module-id.restart.post": reversibleWrite,
		"POST /api/v1/operations/terra.daemon.modules.by-module-id.restart.post/invoke": `{"ok":true}`,
		"GET /api/v1/catalog/operations/terra.daemon.commands.execute.post":             dangerousOperation,
	}}
	agent, records := newAgent(t, AutonomyUnattended, transport)

	approved := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"terra.daemon.modules.by-module-id.restart.post","reason":"nightly"}`))
	if approved.Err != nil {
		t.Fatalf("a pre-approved call was refused: %+v", approved.Err)
	}
	if !transport.called("POST /api/v1/operations/terra.daemon.modules.by-module-id.restart.post/invoke") {
		t.Fatal("the pre-approved call never reached the Gateway")
	}

	// Anything not named stops, which is what "상한 초과 시 중단" rests on.
	refused := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"terra.daemon.commands.execute.post","reason":"cleanup"}`))
	if refused.Err == nil || refused.Err.Code != CodeApprovalRequired {
		t.Fatalf("an unlisted call = %+v", refused)
	}
	if transport.called("POST /api/v1/operations/terra.daemon.commands.execute.post/invoke") {
		t.Fatal("an unlisted call reached the Gateway")
	}
	if len(*records) != 2 || (*records)[0].Decision != DecisionRun || (*records)[1].Decision != DecisionRefuse {
		t.Fatalf("records = %+v", *records)
	}
}

// A credential the Gateway will not describe gets no pre-approval at all.
// Guessing would hand a session an authority nobody granted it.
func TestAnUnreadableCredentialPreApprovesNothing(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/terra.daemon.modules.by-module-id.restart.post": reversibleWrite,
	}}
	agent, _ := newAgent(t, AutonomyUnattended, transport)

	result := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"terra.daemon.modules.by-module-id.restart.post"}`))
	if result.Err == nil || result.Err.Code != CodeApprovalRequired {
		t.Fatalf("result = %+v", result)
	}
	if transport.called("POST /api/v1/operations") {
		t.Fatal("a session that could not read its own credential still called")
	}
}
