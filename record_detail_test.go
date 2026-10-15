package agentcore

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// M-5 — 호출 기록(Record)에 입력 요약·결과 요약·결과 방식(output.mode)을 싣는다.
// 호스트가 "에이전트가 실제로 무엇을 넘겼고 무엇을 받았는가"를 모델의 말이 아니라
// 기록에서 읽게 하는 칸이다. 입력은 비밀을 가린 뒤에만 기록에 닿는다(I1).

const (
	statusOp   = "terra.daemon.status.get"
	restartOp  = "terra.daemon.modules.by-module-id.restart.post"
	invokeStat = "POST /api/v1/operations/" + statusOp + "/invoke"
)

func describeKey(operationID string) string {
	return "GET /api/v1/catalog/operations/" + operationID
}

func invokeKeyOf(operationID string) string {
	return "POST /api/v1/operations/" + operationID + "/invoke"
}

func callWith(operationID, input string) json.RawMessage {
	return json.RawMessage(`{"operationId":"` + operationID + `","input":` + input + `,"reason":"test"}`)
}

// 입력은 키 이름이 비밀을 가리키면 값을 *** 로 바꾼 뒤 기록된다. 가림은 중첩과
// 배열 안에서도 하고, 비밀이 아닌 칸은 그대로 남아 사람이 무엇을 넘겼는지 읽을 수 있다.
func TestRecordInputSummaryMasksSecretsAndKeepsTheRest(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{
		describeKey(statusOp): readOperation,
		invokeStat:            `{"ok":true}`,
	}}
	agent, records := newAgent(t, AutonomyAuto, transport)

	input := `{"module_id":"io.terra.sample","api_key":"sk-live-1234567890","opts":{"Authorization":"Bearer abc.def","items":[{"password":"hunter2","name":"a"}]},"env":{"KEY":"v"}}`
	if result := agent.Call(context.Background(), ToolInvoke, callWith(statusOp, input)); result.Err != nil {
		t.Fatalf("invoke: %+v", result.Err)
	}
	summary := (*records)[0].InputSummary
	for _, leaked := range []string{"sk-live-1234567890", "Bearer abc.def", "hunter2", `"KEY"`} {
		if strings.Contains(summary, leaked) {
			t.Errorf("input summary leaks %q: %s", leaked, summary)
		}
	}
	for _, kept := range []string{"io.terra.sample", `"name":"a"`, `"***"`} {
		if !strings.Contains(summary, kept) {
			t.Errorf("input summary lost %q: %s", kept, summary)
		}
	}
	if !json.Valid([]byte(summary)) {
		t.Errorf("a short summary should still be JSON: %s", summary)
	}
}

// 입력을 JSON으로 읽을 수 없으면 날것을 옮기지 않는다 — 가릴 수 없는 것은 싣지 않는다.
func TestRecordInputSummaryIsEmptyWhenTheInputIsNotJSON(t *testing.T) {
	if got := summarizeInput(json.RawMessage(`token=abc`)); got != "" {
		t.Errorf("unparseable input was carried: %q", got)
	}
	if got := summarizeInput(nil); got != "" {
		t.Errorf("absent input gave %q", got)
	}
	if got := summarizeInput(json.RawMessage(`{}`)); got != "{}" {
		t.Errorf("empty object gave %q", got)
	}
}

// 긴 입력은 줄이고, 줄여도 글자(룬)를 자르지 않는다. 줄였다는 표시가 남는다.
func TestRecordSummaryIsBoundedOnARuneBoundary(t *testing.T) {
	long := `{"text":"` + strings.Repeat("가", 1000) + `"}`
	got := summarizeInput(json.RawMessage(long))
	if len(got) > summaryLimit+len("…") {
		t.Errorf("summary is %d bytes, limit %d", len(got), summaryLimit)
	}
	if !utf8.ValidString(got) {
		t.Errorf("summary cut a rune: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut summary must say it was cut: %q", got)
	}
}

// output.mode는 계약의 값을 그대로 싣는다. "접수"(accepted-job)는 "완료"가 아니다.
func TestRecordCarriesTheContractsOutputMode(t *testing.T) {
	for _, tc := range []struct{ name, operation, body, want string }{
		{"immediate", statusOp, readOperation, "immediate"},
		{"accepted-job", "terra.daemon.commands.execute.post", dangerousOperation, "accepted-job"},
		{"no output declared", statusOp, `{"operationId":"terra.daemon.status.get","permissions":["node.read"],"execution":{"risk":"read","idempotencyMode":"safe","retryMode":"safe-only"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{responses: map[string]string{
				describeKey(tc.operation):  tc.body,
				invokeKeyOf(tc.operation):  `{"task_id":"t-1"}`,
				"GET /api/v1/agent/whoami": localWhoami,
			}}
			agent, records := newAgent(t, AutonomyAsk, transport)
			agent.approver = ApproverFunc(func(context.Context, ApprovalRequest) (bool, error) { return true, nil })
			agent.Call(context.Background(), ToolInvoke, callWith(tc.operation, `{}`))
			if len(*records) != 1 || (*records)[0].OutputMode != tc.want {
				t.Fatalf("records = %+v, want OutputMode %q", *records, tc.want)
			}
		})
	}
}

// 결과 요약: 성공은 응답의 가려진 발췌, 오류는 코드와 메시지다. 응답의 비밀도 가린다.
func TestRecordResultSummary(t *testing.T) {
	t.Run("ok masks the response too", func(t *testing.T) {
		transport := &fakeTransport{responses: map[string]string{
			describeKey(statusOp): readOperation,
			invokeStat:            `{"state":"running","credential":"tsa_SECRETVALUE","modules":3}`,
		}}
		agent, records := newAgent(t, AutonomyAuto, transport)
		agent.Call(context.Background(), ToolInvoke, callWith(statusOp, `{}`))
		summary := (*records)[0].ResultSummary
		if strings.Contains(summary, "tsa_SECRETVALUE") || !strings.Contains(summary, `"state":"running"`) || !strings.Contains(summary, `"***"`) {
			t.Errorf("result summary = %q", summary)
		}
	})
	t.Run("error carries the code", func(t *testing.T) {
		transport := &fakeTransport{
			responses: map[string]string{
				describeKey(statusOp): readOperation,
				invokeStat:            `{"error":{"code":"MODULE_PERMISSION_DENIED","message":"no"}}`,
			},
			status: map[string]int{invokeStat: 403},
		}
		agent, records := newAgent(t, AutonomyAuto, transport)
		agent.Call(context.Background(), ToolInvoke, callWith(statusOp, `{}`))
		if got := (*records)[0].ResultSummary; !strings.Contains(got, "MODULE_PERMISSION_DENIED") {
			t.Errorf("result summary = %q", got)
		}
	})
	t.Run("a refused call has no result to summarize", func(t *testing.T) {
		const dangerous = "terra.daemon.commands.execute.post"
		transport := &fakeTransport{responses: map[string]string{describeKey(dangerous): dangerousOperation}}
		agent, records := newAgent(t, AutonomyAsk, transport)
		agent.Call(context.Background(), ToolInvoke, callWith(dangerous, `{"command":"ls"}`))
		record := (*records)[0]
		if record.Status != StatusRefused || record.ResultSummary != "" {
			t.Errorf("record = %+v", record)
		}
		// 입력과 결과 방식은 거절된 호출에도 남는다 — 무엇을 하려 했는가의 기록이다.
		if !strings.Contains(record.InputSummary, `"ls"`) || record.OutputMode != "accepted-job" {
			t.Errorf("refused record lost what was attempted: %+v", record)
		}
	})
}

// 계획만 한 호출(dry run)은 입력과 결과 방식을 남기고 결과는 없다.
func TestPlannedRecordCarriesInputAndModeButNoResult(t *testing.T) {
	transport := &fakeTransport{responses: map[string]string{describeKey(restartOp): reversibleWrite}}
	agent, records := newAgent(t, AutonomyAuto, transport)
	agent.dryRun = true
	agent.Call(context.Background(), ToolInvoke, callWith(restartOp, `{"module_id":"io.terra.sample"}`))
	record := (*records)[0]
	if record.Status != StatusPlanned || record.ResultSummary != "" {
		t.Fatalf("record = %+v", record)
	}
	if !strings.Contains(record.InputSummary, "io.terra.sample") || record.OutputMode != "immediate" {
		t.Errorf("planned record = %+v", record)
	}
}

// 외부 MCP 호출은 인자 요약만 남는다. 결과는 제3자의 글이라 요약하지 않고(크기만),
// output.mode는 Gateway 계약의 것이라 없다.
func TestExternalRecordCarriesMaskedArgumentsOnly(t *testing.T) {
	tools := []ExternalTool{{Server: "docs", Name: "lookup", ReadOnly: true}}
	caller := &fakeExternal{answer: ExternalResult{Content: "the page text"}}
	agent, records := externalAgent(t, AutonomyAuto, tools, caller)
	agent.Call(context.Background(), "ext__docs__lookup", json.RawMessage(`{"q":"terra","token":"abc123456"}`))
	record := (*records)[0]
	if strings.Contains(record.InputSummary, "abc123456") || !strings.Contains(record.InputSummary, `"q":"terra"`) {
		t.Errorf("external input summary = %q", record.InputSummary)
	}
	if record.ResultSummary != "" || record.OutputMode != "" {
		t.Errorf("external record carries what it cannot know: %+v", record)
	}
}

// 하위 호환: 새 칸이 비면 JSON에서 빠지고, 새 칸이 없는 옛 줄도 그대로 읽힌다.
func TestRecordDetailFieldsAreOptionalOnTheWire(t *testing.T) {
	encoded, err := json.Marshal(Record{Tool: ToolInvoke, Status: StatusOK})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"inputSummary", "resultSummary", "outputMode"} {
		if strings.Contains(string(encoded), key) {
			t.Errorf("empty %s must be absent: %s", key, encoded)
		}
	}
	var old Record
	if err := json.Unmarshal([]byte(`{"time":"1970-01-01T00:00:00Z","tool":"terra_invoke","status":"ok"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.InputSummary != "" || old.ResultSummary != "" || old.OutputMode != "" {
		t.Errorf("old record = %+v", old)
	}
}

// 디스크 왕복: 호스트는 Record를 jsonl 한 줄씩 쓰고 다시 읽는다. 새 칸이 채워진 줄은
// 값이 그대로 돌아오고, 새 칸이 없는 옛 줄은 같은 파일에 섞여 있어도 빈 값으로 읽힌다.
func TestRecordDetailRoundTripsThroughAJSONLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// 옛 줄(새 칸 없음)을 먼저 쓴다.
	if _, err := file.WriteString(`{"time":"1970-01-01T00:00:00Z","tool":"terra_invoke","operationId":"a.b","status":"ok"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	want := Record{
		Time: time.Unix(0, 0).UTC(), Tool: ToolInvoke, OperationID: "c.d", Status: StatusOK,
		InputSummary: `{"module_id":"m","token":"***"}`, ResultSummary: `{"ok":true}`, OutputMode: "accepted-job",
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(want); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var got []Record
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("line %q: %v", scanner.Text(), err)
		}
		got = append(got, record)
	}
	if len(got) != 2 {
		t.Fatalf("read %d records, want 2", len(got))
	}
	if got[0].OperationID != "a.b" || got[0].InputSummary != "" || got[0].ResultSummary != "" || got[0].OutputMode != "" {
		t.Errorf("old line = %+v", got[0])
	}
	if got[1] != want {
		t.Errorf("round trip = %+v, want %+v", got[1], want)
	}
}
