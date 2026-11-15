package agentcore

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCatalogExecutionApprovalRoundTrips(t *testing.T) {
	raw := `{"risk":"dangerous","approval":{"tier":"T4","impact":"trust","inputs":["a"],"switch":"remote-manage","secret_result":["code"]}}`
	var execution CatalogExecution
	if err := json.Unmarshal([]byte(raw), &execution); err != nil {
		t.Fatal(err)
	}
	want := &CatalogApproval{Tier: "T4", Impact: "trust", Inputs: []string{"a"}, Switch: "remote-manage", SecretResult: []string{"code"}}
	if !reflect.DeepEqual(execution.Approval, want) {
		t.Fatalf("approval = %#v", execution.Approval)
	}
	out, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	var again CatalogExecution
	if err := json.Unmarshal(out, &again); err != nil || !reflect.DeepEqual(again.Approval, want) {
		t.Fatalf("round trip lost approval: %s", out)
	}
}
