package agentcore

import "encoding/json"

// 이 파일의 타입은 Terra의 terra-api-contract 패키지(catalog.go)에 있는
// CatalogOperation 계열을 이 패키지가 쓰는 만큼 옮겨 온 것이다. agentcore를
// 외부 의존 0으로 유지하기 위해 자체 정의한다(설계 문서 P-1).
//
// JSON 태그는 원본과 같아야 한다. Gateway가 내려주는 JSON을 그대로 읽고,
// describe 도구가 같은 모양으로 모델에 돌려주기 때문이다. 필드를 줄이면 그
// 필드는 describe 출력에서도 사라진다.

// CatalogOperation is a discoverable operation as the Gateway catalog serves it.
type CatalogOperation struct {
	OperationID  string               `json:"operationId"`
	ProviderID   string               `json:"providerId,omitempty"`
	Version      string               `json:"version,omitempty"`
	Status       string               `json:"status,omitempty"`
	Title        string               `json:"title,omitempty"`
	Summary      string               `json:"summary,omitempty"`
	Category     string               `json:"category,omitempty"`
	Tags         []string             `json:"tags,omitempty"`
	Permissions  []string             `json:"permissions,omitempty"`
	Bindings     []string             `json:"bindings,omitempty"`
	Errors       []string             `json:"errors,omitempty"`
	Availability *CatalogAvailability `json:"availability,omitempty"`
	// Execution, Output and SideEffects are what the contract says about
	// RUNNING the operation; the approval table reads them.
	Execution   *CatalogExecution   `json:"execution,omitempty"`
	Output      *CatalogOutput      `json:"output,omitempty"`
	SideEffects []CatalogSideEffect `json:"sideEffects,omitempty"`
	// Input is the operation's input JSON Schema, verbatim. The Gateway serves
	// it on describe only.
	Input json.RawMessage `json:"input,omitempty"`
}

// CatalogExecution is an operation's execution policy. The zero value means
// the contract declared none, which must be read as "unknown", not as "safe".
type CatalogExecution struct {
	Risk             string `json:"risk,omitempty"`
	ConfirmationMode string `json:"confirmationMode,omitempty"`
	IdempotencyMode  string `json:"idempotencyMode,omitempty"`
	RetryMode        string `json:"retryMode,omitempty"`
	Cancellation     string `json:"cancellation,omitempty"`
}

// CatalogOutput is how an operation answers: immediate, accepted-job, stream
// or empty.
type CatalogOutput struct {
	Mode string `json:"mode,omitempty"`
}

// CatalogSideEffect is one declared side effect: what resource changes and how.
type CatalogSideEffect struct {
	ResourceID string `json:"resourceId,omitempty"`
	Action     string `json:"action,omitempty"`
}

// CatalogAvailability is an operation's availability constraints.
type CatalogAvailability struct {
	Products              []string `json:"products,omitempty"`
	HostRoles             []string `json:"hostRoles,omitempty"`
	Scopes                []string `json:"scopes,omitempty"`
	RequiresProviderState string   `json:"requiresProviderState,omitempty"`
	RequiresCapabilities  []string `json:"requiresCapabilities,omitempty"`
}
