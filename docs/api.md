---
title: "terra-agent 공개 API 목록"
doc_type: "reference"
scope: "project"
target: "terra-agent"
status: "active"
version: "v0.1"
last_updated: "2026-10-09"
related:
  - "docs/design/module-independent-repos.md"
---

# terra-agent 공개 API 목록

패키지 `agentcore` (`github.com/StellaxiaLab/terra-agent`)의 공개 심볼 전체와, 소비자 둘이 쓰는 심볼의 대응표.
작업 ID **A-4**. 원본은 Terra `products/common/packages/terra-agent-core`이고, 이동 과정에서 이름·시그니처를 바꾸지 않았다.
단 하나의 예외는 `CatalogOperation` 계열 타입의 정의 위치다. `terra-api-contract`에서 이 패키지로 옮겨졌다([§4](#4-catalogoperation-계열-p-1), 설계 문서 §7).

## 1. 소비자별 사용 심볼

**이 목록의 심볼은 하나도 사라지면 안 된다.** `api_test.go`가 컴파일 타임 참조로 이 보증을 지킨다. 심볼이 빠지거나 모양이 바뀌면 패키지가 빌드되지 않는다.

### 1.1 terra-cli

Terra `products/common/apps/terra-cli/internal/{mcp/server.go, app/mcp.go, client/client.go}`(+ `mcp/server_test.go`)를 `agentcore\.` 로 스캔한 실측이다(Terra `origin/main` b2a76a6).

| 심볼 | 종류 |
| --- | --- |
| `Agent` | 타입 |
| `New` | 함수 |
| `Options` | 타입 |
| `Recorder`, `RecorderFunc`, `NopRecorder` | 인터페이스, 함수 어댑터, 기본 구현 |
| `Record` | 타입 |
| `Autonomy`, `AutonomyPlan`, `AutonomyAsk`, `AutonomyAuto`, `KnownAutonomy` | 타입, 상수, 함수 |
| `Answer` | 타입 |
| `ToolSearch`, `ToolDescribe`, `ToolInvoke`, `ToolSession` | 상수 |
| `Tools` | 함수 |
| `Transport` | 인터페이스 |

terra-cli는 `CatalogOperation`을 직접 쓰지 않는다(위 세 파일에 `apicontract`·`CatalogOperation` 참조 없음). 그래서 이 타입이 `terra-api-contract`에서 `agentcore`로 옮겨져도 CLI 쪽에서 변환할 것이 없다.

### 1.2 modules — `io.terra.agent`

`StellaxiaLab/modules` `main` a78819d의 `common/io.terra.agent`를 `agentcore\.` 로 스캔한 실측이다. 서로 다른 심볼 **47개**로, 설계 문서 §3의 "27"보다 많다(그 수치는 당시 스캔 기준이며 이후 늘었거나 다르게 센 것으로 보인다).

| 그룹 | 심볼 |
| --- | --- |
| 에이전트 | `Agent`, `New`, `Options`, `Tools`, `Tool`, `ToolResult`, `NewClient` |
| 전송 | `Transport`, `Answer` |
| 승인 | `Approver`, `ApproverFunc`, `ApprovalRequest`, `Policy`, `Decision`, `DecisionRun`, `DecisionPlan`, `DecisionConfirm`, `DecisionRefuse`, `DecideExternal` |
| 자율도 | `Autonomy`, `AutonomyPlan`, `AutonomyAsk`, `AutonomyAuto`, `AutonomyUnattended`, `KnownAutonomy` |
| 기록 | `Record`, `Recorder`, `RecorderFunc`, `StatusOK`, `StatusRefused`, `StatusError`, `StatusPlanned` |
| 도구 이름 | `ToolSearch`, `ToolDescribe`, `ToolInvoke`, `ToolSession`, `ToolNodes` |
| 오류 | `Error`, `CodeUnknownTool`, `CodeApprovalRequired`, `CodeApprovalDenied`, `CodeRetryRefused` |
| 외부 도구 | `ExternalTool`, `ExternalResult`, `ExternalToolPrefix`, `ParseExternalToolName`, `ValidExternalName` |

modules는 `ApprovalRequest.Operation`의 하위 필드를 직접 읽지 않는다(`.Operation.` 참조 없음). 따라서 `CatalogOperation` 이동이 modules 코드에 미치는 영향은 타입 이름 참조가 없는 한 없다.

두 소비자가 겹치는 것: `Agent`, `New`, `Options`, `Answer`, `Transport`, `Record`, `Recorder`, `RecorderFunc`, `Autonomy*`, `ToolInvoke`.

## 2. 전체 공개 심볼

### 2.1 상수

| 그룹 | 심볼 = 값 |
| --- | --- |
| 기록 상태 | `StatusOK="ok"`, `StatusRefused="refused"`, `StatusError="error"`, `StatusPlanned="planned"` |
| 메타 도구 이름 | `ToolSearch="terra_search_operations"`, `ToolDescribe="terra_describe_operation"`, `ToolInvoke="terra_invoke"`, `ToolSession="terra_session"`, `ToolNodes="terra_nodes"` |
| 거절 코드 | `CodeUnknownTool`, `CodeInvalidArguments`, `CodeApprovalRequired`, `CodeApprovalDenied`, `CodeStreamUnsupported`, `CodeRetryRefused`, `CodeReachExceeded`, `CodeExternalFailed` (값은 `TERRA_` 접두 대문자 스네이크, 예: `TERRA_APPROVAL_REQUIRED`) |
| 도달 범위 | `ReachLocal="local"`, `ReachNode="node"`, `ReachCluster="cluster"`, `ReachGlobal="global"` |
| 외부 도구 | `ExternalToolPrefix="ext__"` |
| 자율도 | `AutonomyPlan="plan"`, `AutonomyAsk="ask"`, `AutonomyAuto="auto"`, `AutonomyUnattended="unattended"` |
| 판정 | `DecisionRun="run"`, `DecisionConfirm="confirm"`, `DecisionPlan="plan"`, `DecisionRefuse="refuse"` |

### 2.2 함수

```go
func New(transport Transport, options Options) *Agent
func NewClient(transport Transport) *Client
func Tools() []Tool
func ProjectExternalTools(tools []ExternalTool) []Tool
func Decide(operation CatalogOperation, policy Policy) Judgement
func DecideExternal(tool ExternalTool, policy Policy) Judgement
func AllowsRetry(operation CatalogOperation) bool
func KnownAutonomy(name string) bool
func FenceExternal(server, tool string, result ExternalResult) string
func ParseExternalToolName(name string) (server, tool string, ok bool)
func ValidExternalName(name string) bool
```

### 2.3 인터페이스

```go
type Transport interface {
    Do(ctx context.Context, method, path string, body []byte) (Answer, error)
}
type Approver interface {
    Approve(ctx context.Context, request ApprovalRequest) (approved bool, err error)
}
type Recorder interface { Record(record Record) }
type ExternalCaller interface {
    CallExternal(ctx context.Context, server, tool string, arguments json.RawMessage) (ExternalResult, error)
}
```

함수 어댑터: `type ApproverFunc func(ctx, ApprovalRequest) (bool, error)`, `type RecorderFunc func(Record)`. 기본 구현: `type NopRecorder struct{}`.

### 2.4 타입과 메서드

| 타입 | 공개 필드 / 메서드 |
| --- | --- |
| `Agent` | `Autonomy() Autonomy`, `Call(ctx, name string, arguments json.RawMessage) ToolResult`, `DryRun() bool`, `ToolsFor(ctx) []Tool` |
| `Options` | `Autonomy`, `Recorder`, `Approver`, `DryRun`, `External []ExternalTool`, `ExternalCaller`, `Now func() time.Time` |
| `Answer` | `Status int`, `Body []byte`, `TraceID string` |
| `Client` | `Describe`, `Example`, `Invoke`, `Nodes`, `Search`, `Whoami` |
| `Query` | `Text`, `Category`, `Tag`, `Permissions []string`, `Limit int` |
| `Session` | `Principal`, `Permissions`, `AllPermissions`, `Delegate`, `Reach`, `ReachLimited`, `AgentSessionID`, `Unattended`, `PreApproved`, `IssuedAt`, `ExpiresAt` |
| `Node`, `NodeListing` | `NodeID`, `DisplayName`, `Status`, `Roles`, `ClusterID` / `Count`, `Nodes` |
| `Policy` | `Autonomy`, `PreApproved []string` |
| `Judgement` | `Decision Decision`, `Reason string` |
| `ApprovalRequest` | `OperationID`, `Input json.RawMessage`, `Reason`, `Judgement`, `Operation CatalogOperation` |
| `Tool` | `Name`, `Title`, `Description`, `InputSchema json.RawMessage` |
| `ToolResult` | `Payload json.RawMessage`, `Err *Error`, `TraceID string` |
| `Error` | `Code`, `Message`, `TraceID`, `Status int`, `Retryable bool`, `Error() string` |
| `Record` | `Time`, `Tool`, `OperationID`, `Decision`, `Judgement`, `Reason`, `TraceID`, `Status`, `ErrorCode`, `External`, `ResultBytes` |
| `ExternalTool` | `Server`, `Name`, `Title`, `Description`, `InputSchema`, `ReadOnly`, `QualifiedName() string` |
| `ExternalResult` | `Content string`, `Truncated bool`, `IsError bool` |

JSON 태그가 있는 타입(`Session`, `Node`, `NodeListing`, `Error`, `Record`, `Tool`, `Catalog*`)의 태그는 원본과 같다. Gateway·MCP와 주고받는 JSON이므로 바꾸지 않는다.

## 3. 안정성 약속

- v0.x 동안에도 §1의 심볼은 제거하지 않는다. 시그니처를 바꿔야 하면 소비자(Terra·modules)와 먼저 조율한다.
- 이 패키지는 외부 의존 0이다. `imports_test.go`가 비표준 임포트를 막고, `net`·`net/http`·`os/exec` 같은 표준 패키지도 막는다.

## 4. CatalogOperation 계열 (P-1)

원본은 `terra-api-contract/catalog.go`에 있었고, 이 패키지는 그 중 `CatalogOperation`과 딸린 4개 타입을 `catalog.go`에 자체 정의한다. JSON 태그는 원본과 같다. 어떤 필드를 왜 가져왔는지는 설계 문서 §7의 P-1 결과를 본다.

공개 시그니처에서 이 타입이 나타나는 곳: `Decide`, `AllowsRetry`, `ApprovalRequest.Operation`, `Client.Search`, `Client.Describe`. 타입 이름은 같지만 패키지가 `apicontract`에서 `agentcore`로 바뀌었다.

## 관련 문서

- [모듈 독립 레포 전환 — 설계와 작업 분담](design/module-independent-repos.md) — D-15, §5.2 A-1~A-4, §7
- [README](../README.md)
