package agentcore

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Gateway가 내려주는 JSON과 describe 도구가 모델에 돌려주는 JSON은 같은 모양이어야
// 한다. 원본(Terra의 terra-api-contract)의 JSON 태그와 하나라도 어긋나면 필드가
// 조용히 사라지므로, 모든 필드를 채운 JSON이 그대로 왕복하는지 본다.
func TestCatalogOperationJSONRoundTripsEveryField(t *testing.T) {
	const wire = `{
		"operationId": "io.example.do",
		"providerId": "io.example",
		"version": "1.2.0",
		"status": "stable",
		"title": "Do",
		"summary": "Does a thing",
		"category": "demo",
		"tags": ["a", "b"],
		"permissions": ["p.read"],
		"bindings": ["http"],
		"errors": ["E_X"],
		"availability": {
			"products": ["terra"],
			"hostRoles": ["master"],
			"scopes": ["node"],
			"requiresProviderState": "ready",
			"requiresCapabilities": ["cap.a"]
		},
		"execution": {
			"risk": "write",
			"confirmationMode": "required",
			"idempotencyMode": "key-supported",
			"retryMode": "idempotency-key-only",
			"cancellation": "supported"
		},
		"output": {"mode": "accepted-job"},
		"sideEffects": [{"resourceId": "r1", "action": "create"}],
		"input": {"type":"object"}
	}`
	var operation CatalogOperation
	if err := json.Unmarshal([]byte(wire), &operation); err != nil {
		t.Fatal(err)
	}
	if operation.Execution == nil || operation.Execution.RetryMode != "idempotency-key-only" ||
		operation.Output == nil || operation.Output.Mode != "accepted-job" ||
		len(operation.SideEffects) != 1 || operation.SideEffects[0].Action != "create" ||
		operation.Availability == nil || operation.Availability.RequiresProviderState != "ready" ||
		len(operation.Input) == 0 {
		t.Fatalf("a field did not decode: %+v", operation)
	}
	encoded, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal([]byte(wire), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round trip changed the JSON\nwant %v\n got %v", want, got)
	}
}
