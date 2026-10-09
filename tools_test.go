package agentcore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fakeTransport answers Gateway paths from a table. It records what was asked,
// so a test can assert that a refused call never reached the Gateway.
type fakeTransport struct {
	responses map[string]string
	status    map[string]int
	seen      []string
}

// traceFor is what the fake Gateway would have stamped on this call. A real
// Gateway issues one per request; the fake makes it derivable so a test can
// assert the record carries the SAME id the call was answered with.
func traceFor(key string) string {
	return "trace-" + strings.NewReplacer(" ", "-", "/", "_").Replace(key)
}

func (f *fakeTransport) Do(_ context.Context, method, path string, body []byte) (Answer, error) {
	key := method + " " + path
	f.seen = append(f.seen, key)
	answer := Answer{TraceID: traceFor(key)}
	if status, ok := f.status[key]; ok {
		answer.Status, answer.Body = status, []byte(f.responses[key])
		return answer, nil
	}
	response, ok := f.responses[key]
	if !ok {
		answer.Status, answer.Body = 404, []byte(`{"error":{"code":"OPERATION_NOT_FOUND","message":"no"}}`)
		return answer, nil
	}
	answer.Status, answer.Body = 200, []byte(response)
	return answer, nil
}

func (f *fakeTransport) called(prefix string) bool {
	for _, key := range f.seen {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

const dangerousOperation = `{
	"operationId": "terra.daemon.commands.execute.post",
	"permissions": ["process.execute"],
	"execution": {"risk": "dangerous", "idempotencyMode": "none", "retryMode": "never"},
	"output": {"mode": "accepted-job"}
}`

const readOperation = `{
	"operationId": "terra.daemon.status.get",
	"permissions": ["node.read"],
	"execution": {"risk": "read", "idempotencyMode": "safe", "retryMode": "safe-only"},
	"output": {"mode": "immediate"},
	"sideEffects": [{"action": "read"}]
}`

func newAgent(t *testing.T, autonomy Autonomy, transport *fakeTransport) (*Agent, *[]Record) {
	t.Helper()
	records := &[]Record{}
	agent := New(transport, Options{
		Autonomy: autonomy,
		Recorder: RecorderFunc(func(record Record) { *records = append(*records, record) }),
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	return agent, records
}

// 도구 수는 설치된 모듈 수와 무관하게 고정이다. 이것이 이 설계가 사는 성질이라
// 값 자체를 못으로 박는다.
func TestToolSurfaceIsFixed(t *testing.T) {
	tools := Tools()
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want 4 regardless of what is installed", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		if tool.Description == "" {
			t.Fatalf("%s has no description; a model cannot choose a tool it cannot read", tool.Name)
		}
		var schema struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("%s has an invalid input schema: %v", tool.Name, err)
		}
		if schema.Type != "object" {
			t.Fatalf("%s input schema type = %q, want object", tool.Name, schema.Type)
		}
		names[tool.Name] = true
	}
	for _, want := range []string{ToolSearch, ToolDescribe, ToolInvoke, ToolSession} {
		if !names[want] {
			t.Fatalf("missing tool %s", want)
		}
	}
}

// 승인이 필요한 호출은 Gateway에 도달하지 않는다. 거절이 아니라 "부르고 나서
// 후회하기"였다면 부작용이 이미 일어난 뒤다.
func TestApprovalRefusalNeverReachesTheGateway(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/terra.daemon.commands.execute.post": dangerousOperation,
	}}
	agent, records := newAgent(t, AutonomyAuto, transport)

	result := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"terra.daemon.commands.execute.post","input":{"command":"rm"}}`))
	if result.Err == nil || result.Err.Code != CodeApprovalRequired {
		t.Fatalf("result = %+v, want %s", result, CodeApprovalRequired)
	}
	if !strings.Contains(result.Err.Message, "dangerous") {
		t.Fatalf("the refusal does not say why: %q", result.Err.Message)
	}
	if transport.called("POST /api/v1/operations") {
		t.Fatal("a refused call still reached the Gateway")
	}
	if len(*records) != 1 || (*records)[0].Status != StatusRefused {
		t.Fatalf("records = %+v", *records)
	}
	if (*records)[0].Decision != DecisionConfirm {
		t.Fatalf("the record does not carry the decision: %+v", (*records)[0])
	}
}

// accepted-job은 "받아들였다"이지 "끝났다"가 아니다.
func TestAcceptedJobIsNotReportedAsFinished(t *testing.T) {
	// 되돌릴 수 있는 쓰기라 auto에서 통과한다 — 확인하려는 것은 승인이 아니라
	// accepted-job 응답의 포장이다.
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/io.example.job.post": `{
			"operationId": "io.example.job.post",
			"execution": {"risk": "write", "idempotencyMode": "key-supported", "retryMode": "safe-only"},
			"output": {"mode": "accepted-job"},
			"sideEffects": [{"action": "create"}]
		}`,
		"POST /api/v1/operations/io.example.job.post/invoke": `{"status":"accepted","data":{"task_id":"task_42"},"meta":{}}`,
	}}

	agent, _ := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"io.example.job.post","input":{}}`))
	if result.Err != nil {
		t.Fatalf("err = %+v", result.Err)
	}
	var payload struct {
		OutputMode string `json:"outputMode"`
		Accepted   bool   `json:"accepted"`
		TaskID     string `json:"taskId"`
		Hint       string `json:"hint"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OutputMode != "accepted-job" || !payload.Accepted {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.TaskID != "task_42" {
		t.Fatalf("taskId = %q, want the id from the provider's envelope", payload.TaskID)
	}
	if !strings.Contains(payload.Hint, ToolSearch) {
		t.Fatalf("the hint should say how to find the poll operation: %q", payload.Hint)
	}
}

// 스트림은 도구 결과로 돌려주지 않는다 — 노드가 출력하는 임의의 텍스트를
// 프롬프트에 붓는 자리다.
func TestStreamOperationIsRefusedRatherThanPoured(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog/operations/io.example.logs.stream": `{
			"operationId": "io.example.logs.stream",
			"execution": {"risk": "read", "idempotencyMode": "safe", "retryMode": "safe-only"},
			"output": {"mode": "stream"},
			"sideEffects": [{"action": "read"}]
		}`,
	}}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"io.example.logs.stream"}`))
	if result.Err == nil || result.Err.Code != CodeStreamUnsupported {
		t.Fatalf("result = %+v, want %s", result, CodeStreamUnsupported)
	}
	if transport.called("POST /api/v1/operations") {
		t.Fatal("a stream operation was invoked anyway")
	}
}

// Gateway의 오류는 코드 그대로 넘어간다. 문자열로 뭉개면 모델이 재시도해도
// 되는지 추측하게 된다.
func TestGatewayErrorsKeepTheirCode(t *testing.T) {
	transport := &fakeTransport{
		responses: map[string]string{
			"GET /api/v1/catalog/operations/terra.daemon.status.get": readOperation,
			"POST /api/v1/operations/terra.daemon.status.get/invoke": `{"error":{"code":"MODULE_PERMISSION_DENIED","message":"nope","traceId":"trace-1"}}`,
		},
		status: map[string]int{
			"POST /api/v1/operations/terra.daemon.status.get/invoke": 403,
		},
	}
	agent, records := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolInvoke,
		json.RawMessage(`{"operationId":"terra.daemon.status.get","reason":"checking health"}`))
	if result.Err == nil || result.Err.Code != "MODULE_PERMISSION_DENIED" {
		t.Fatalf("err = %+v", result.Err)
	}
	if result.Err.TraceID != "trace-1" {
		t.Fatalf("the trace id was dropped: %+v", result.Err)
	}
	if result.Err.Retryable {
		t.Fatal("a 403 was marked retryable; the model will repeat the same refusal")
	}
	if len(*records) != 1 || (*records)[0].Reason != "checking health" {
		t.Fatalf("the caller's stated reason was not recorded: %+v", *records)
	}
}

// 검색 결과는 각 operation을 부를 수 있는지까지 말한다. 부를 수 없는 것을
// 시도하게 두면 실패한 계획이 우회를 찾게 만든다.
func TestSearchCarriesTheApprovalDecision(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		"GET /api/v1/catalog": `{"generation":"gen","count":2,"operations":[` +
			readOperation + `,` + dangerousOperation + `]}`,
	}}
	agent, _ := newAgent(t, AutonomyAuto, transport)
	result := agent.Call(context.Background(), ToolSearch, json.RawMessage(`{}`))
	if result.Err != nil {
		t.Fatalf("err = %+v", result.Err)
	}
	var payload struct {
		Operations []operationSummary `json:"operations"`
	}
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Operations) != 2 {
		t.Fatalf("operations = %d", len(payload.Operations))
	}
	if payload.Operations[0].Approval != DecisionRun {
		t.Fatalf("a read operation should be runnable: %+v", payload.Operations[0])
	}
	if payload.Operations[1].Approval != DecisionConfirm {
		t.Fatalf("a dangerous operation should need confirming: %+v", payload.Operations[1])
	}
}

func TestUnknownToolIsNamed(t *testing.T) {
	agent, _ := newAgent(t, AutonomyAuto, &fakeTransport{responses: map[string]string{}})
	result := agent.Call(context.Background(), "terra_rm_rf", json.RawMessage(`{}`))
	if result.Err == nil || result.Err.Code != CodeUnknownTool {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Err.Message, "terra_rm_rf") {
		t.Fatalf("the error should name the tool: %q", result.Err.Message)
	}
}
