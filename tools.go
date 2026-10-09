package agentcore

// 도구 표면 — 카탈로그를 모델이 쓸 수 있는 형태로 투영한다.
//
// operation 하나에 도구 하나를 만들지 않는다. 계약이 지금 선언하는 것만 데몬 54
// + Master 133이고 설치된 모듈이 거기 더해지므로, 1:1로 펴면 도구 정의만으로
// 컨텍스트가 차고 모듈을 하나 설치할 때마다 호스트 설정이 낡는다.
//
// 대신 고정된 메타툴 넷이다. 카탈로그가 이미 검색·필터를 지원하고 그 결과가
// 호출자 자격으로 좁혀져 나오므로, "무엇을 부를 수 있는가"는 도구 목록이 아니라
// 검색 응답이 답한다 — 그리고 모델은 자기가 못 부르는 것을 아예 보지 못한다.
//
// 도구 수가 설치 상태와 무관하게 고정된다는 것이 이 설계가 사는 성질이다.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// The meta-tool names. They are the whole tool surface, whatever is installed.
const (
	ToolSearch   = "terra_search_operations"
	ToolDescribe = "terra_describe_operation"
	ToolInvoke   = "terra_invoke"
	ToolSession  = "terra_session"
	ToolNodes    = "terra_nodes"
)

// Tool is one exposed tool: a name, a description and an input JSON Schema.
// The shape matches what an MCP host expects, but nothing here speaks MCP —
// the adapter that does lives in the host.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Tools returns the tool surface every credential has.
//
// terra_nodes is not here: it is offered only to a credential that can actually
// reach another node (ToolsFor), because a tool that always fails is worse than
// a tool that is not offered.
func Tools() []Tool {
	return []Tool{
		{
			Name:  ToolSearch,
			Title: "Search Terra operations",
			Description: "Find operations you may call on this node. Results are already narrowed to " +
				"your credential: an operation you cannot call does not appear. Each result carries " +
				"the contract's declared risk and whether calling it needs a person's approval.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Free text matched against operation id, title and summary."},
					"category": {"type": "string", "description": "Exact category, e.g. \"modules\"."},
					"tag": {"type": "string", "description": "Exact tag."},
					"permission": {"type": "array", "items": {"type": "string"}, "description": "Only operations requiring these permissions."},
					"limit": {"type": "integer", "minimum": 1, "description": "Maximum results."}
				},
				"additionalProperties": false
			}`),
		},
		{
			Name:  ToolDescribe,
			Title: "Describe a Terra operation",
			Description: "Return one operation's input JSON Schema, required permissions, declared risk, " +
				"side effects and output mode, plus whether this session may call it without approval.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"operationId": {"type": "string", "description": "The operation id, e.g. \"terra.daemon.status.get\"."},
					"includeExample": {"type": "boolean", "description": "Also return the contract's declared examples."}
				},
				"required": ["operationId"],
				"additionalProperties": false
			}`),
		},
		{
			Name:  ToolInvoke,
			Title: "Invoke a Terra operation",
			Description: "Call one operation through the Gateway. The call is refused when the contract says " +
				"it needs a person's approval and this session runs without one, and when your credential " +
				"does not permit it. Failures return the contract's own error code.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"operationId": {"type": "string", "description": "The operation id to call."},
					"input": {"type": "object", "description": "The operation's input, matching its declared schema."},
					"reason": {"type": "string", "description": "Why you are making this call. Recorded with it."}
				},
				"required": ["operationId"],
				"additionalProperties": false
			}`),
		},
		{
			Name:  ToolSession,
			Title: "Describe this session",
			Description: "Report what this credential can actually do: its effective permissions, how far it " +
				"reaches, when it expires, and how autonomously it is allowed to act. Read this before " +
				"planning anything that spans more than one node.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		},
	}
}

// nodesTool is the fleet half of the surface (A6). It exists because the §14.2
// remote namespace addresses nodes by id, and a credential that cannot learn a
// node id cannot use that namespace at all.
func nodesTool() Tool {
	return Tool{
		Name:  ToolNodes,
		Title: "List the nodes you can reach",
		Description: "Report the other nodes this session may address, with their id, name and status. " +
			"Use it before anything that spans more than one node: the id is how a node is named, " +
			"and a node that does not appear here is one this credential cannot reach.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
	}
}

// ToolsFor returns the tool surface for THIS credential: the fixed four, plus
// terra_nodes when the session can reach past its own node.
//
// The count varies by credential and not by what is installed, which is the
// property that matters — a host's tool list does not go stale when a module
// ships. Reach is asked once and remembered: it is a property of the credential,
// frozen when the grant was issued, so it cannot change under a running session.
//
// A session whose reach cannot be established (the Gateway is unreachable, the
// credential is not a delegate) gets the fixed four. Offering the fleet tool on
// a guess would be offering one that fails on first use.
func (a *Agent) ToolsFor(ctx context.Context) []Tool {
	tools := Tools()
	if a.fleetReach(ctx) {
		tools = append(tools, nodesTool())
	}
	// External tools come last and namespaced, so nothing a third party
	// declares can sit where a Terra tool sits (설계 §7.10 R3).
	return append(tools, ProjectExternalTools(a.external)...)
}

// policy is what this session may decide for itself: its mode, and what the
// credential was issued pre-approved for.
func (a *Agent) policy(ctx context.Context) Policy {
	a.credential(ctx)
	return Policy{Autonomy: a.autonomy, PreApproved: a.preApproved}
}

// fleetReach reports whether this credential reaches past its own node.
func (a *Agent) fleetReach(ctx context.Context) bool {
	a.credential(ctx)
	return a.reachesFleet
}

// credential reads the facts the Gateway established about this credential —
// how far it reaches, and what it may run unasked. Both are frozen when the
// grant is issued, so asking twice would be asking the same question twice.
//
// A session whose credential cannot be read gets neither: no fleet tool, and
// an empty pre-approval list. Guessing either way would hand a session an
// authority nobody granted it.
func (a *Agent) credential(ctx context.Context) {
	a.reachOnce.Do(func() {
		session, err := a.client.Whoami(ctx)
		if err != nil {
			return
		}
		a.preApproved = append([]string(nil), session.PreApproved...)
		// An unlimited credential (a person's own session) reaches everywhere;
		// a delegate reaches exactly as far as its grant said.
		a.reachesFleet = !session.ReachLimited ||
			session.Reach == ReachCluster || session.Reach == ReachGlobal
	})
}

// Agent dispatches tool calls against one Gateway credential.
type Agent struct {
	client   *Client
	autonomy Autonomy
	recorder Recorder
	approver Approver
	dryRun   bool
	now      func() time.Time
	calls    int
	// reachOnce/reachesFleet remember whether this credential reaches past its
	// own node. Reach is frozen when a grant is issued, so asking twice would
	// be asking the same question twice.
	reachOnce    sync.Once
	reachesFleet bool
	// external is the allowlisted third-party surface (A8). Both are nil for a
	// session that registered no server, which is every session by default —
	// an external server exists only because a person registered it (R1).
	external       []ExternalTool
	externalCaller ExternalCaller
	// preApproved is what a person said yes to when this credential was issued
	// (A7). It is read from the same whoami answer as reach, and for the same
	// reason: both are frozen at issue, so both are asked once.
	preApproved []string
	// dispatched remembers invokes that failed AFTER they may have reached the
	// provider, keyed by operation and input, so an identical repeat of a call
	// the contract says must never run twice can be refused (see invoke).
	dispatched map[string]*Error
}

// Options configures an Agent.
type Options struct {
	// Autonomy is how much may run without asking. Defaults to AutonomyAuto.
	Autonomy Autonomy
	Recorder Recorder
	// Approver is who gets asked when the approval table says DecisionConfirm.
	// nil means nobody is there to ask — which is the truth for an MCP host,
	// whose own approval UI is not Terra's gate (§7.5) — and such calls are
	// refused. Mode A supplies the person at the terminal.
	Approver Approver
	// DryRun keeps every invoke away from the Gateway. Reads of the catalog
	// still happen (a plan needs them); the operation itself is reported as
	// what WOULD have been called, with the approval decision it would have
	// met. It is the "아무것도 부르지 않고 계획만" of the design (§10.1).
	DryRun bool
	// External are the tools of the servers a person registered for this
	// session, and ExternalCaller runs them. Both empty means no external
	// surface at all, which is the default: nothing is discovered (설계 §7.10
	// R1).
	External       []ExternalTool
	ExternalCaller ExternalCaller
	Now            func() time.Time
}

// New returns an Agent over transport.
func New(transport Transport, options Options) *Agent {
	autonomy := options.Autonomy
	if !KnownAutonomy(string(autonomy)) {
		autonomy = AutonomyAuto
	}
	recorder := options.Recorder
	if recorder == nil {
		recorder = NopRecorder{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Agent{
		client: NewClient(transport), autonomy: autonomy, recorder: recorder,
		approver: options.Approver, dryRun: options.DryRun, now: now,
		external:       append([]ExternalTool(nil), options.External...),
		externalCaller: options.ExternalCaller,
		dispatched:     map[string]*Error{},
	}
}

// Autonomy reports the mode this agent runs in.
func (a *Agent) Autonomy() Autonomy { return a.autonomy }

// DryRun reports whether this agent only plans.
func (a *Agent) DryRun() bool { return a.dryRun }

// ToolResult is one tool call's answer. Exactly one of Payload and Err is set.
type ToolResult struct {
	Payload json.RawMessage
	Err     *Error
	// TraceID is the Gateway's id for the call this result came from, when one
	// was made. It travels on the result so the record of the call and the
	// Gateway's audit line for the same call can be shown to be one event.
	TraceID string
}

// Tool call refusal codes. They are agent-side, not Gateway codes, so a caller
// can tell "the Gateway said no" from "the gate in front of it said no".
const (
	CodeUnknownTool       = "TERRA_UNKNOWN_TOOL"
	CodeInvalidArguments  = "TERRA_INVALID_ARGUMENTS"
	CodeApprovalRequired  = "TERRA_APPROVAL_REQUIRED"
	CodeApprovalDenied    = "TERRA_APPROVAL_DENIED"
	CodeStreamUnsupported = "TERRA_STREAM_UNSUPPORTED"
	CodeRetryRefused      = "TERRA_RETRY_REFUSED"
	CodeReachExceeded     = "TERRA_REACH_EXCEEDED"
	CodeExternalFailed    = "TERRA_EXTERNAL_FAILED"
)

// Reaches a credential can be issued with. They mirror the Gateway's own
// vocabulary; agentcore only ever compares them, never orders them.
const (
	ReachLocal   = "local"
	ReachNode    = "node"
	ReachCluster = "cluster"
	ReachGlobal  = "global"
)

// Call dispatches one tool call.
func (a *Agent) Call(ctx context.Context, name string, arguments json.RawMessage) ToolResult {
	// External names are checked FIRST, and they can only match the external
	// namespace: the four Terra names have no prefix, so a server that calls
	// its tool terra_invoke is projected as ext__…__terra_invoke and lands
	// here, never in the switch below.
	if server, tool, isExternal := ParseExternalToolName(name); isExternal {
		return a.callExternal(ctx, server, tool, arguments)
	}
	switch name {
	case ToolSearch:
		return a.record(name, "", a.search(ctx, arguments))
	case ToolDescribe:
		return a.record(name, "", a.describe(ctx, arguments))
	case ToolInvoke:
		return a.invoke(ctx, arguments)
	case ToolSession:
		return a.record(name, "", a.session(ctx))
	case ToolNodes:
		return a.record(name, "", a.nodes(ctx))
	}
	return a.record(name, "", ToolResult{Err: &Error{
		Code:    CodeUnknownTool,
		Message: "unknown tool: " + name,
	}})
}

type searchArguments struct {
	Query      string   `json:"query"`
	Category   string   `json:"category"`
	Tag        string   `json:"tag"`
	Permission []string `json:"permission"`
	Limit      int      `json:"limit"`
}

// operationSummary is what a search result says. It is deliberately smaller than
// the catalog entry: a listing is for choosing, and the schema needed to CALL
// the chosen one comes from describe.
type operationSummary struct {
	OperationID string   `json:"operationId"`
	Title       string   `json:"title,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Category    string   `json:"category,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Risk        string   `json:"risk,omitempty"`
	OutputMode  string   `json:"outputMode,omitempty"`
	Approval    Decision `json:"approval"`
}

func (a *Agent) search(ctx context.Context, arguments json.RawMessage) ToolResult {
	var parsed searchArguments
	if err := decodeArguments(arguments, &parsed); err != nil {
		return ToolResult{Err: err}
	}
	operations, err := a.client.Search(ctx, Query{
		Text: parsed.Query, Category: parsed.Category, Tag: parsed.Tag,
		Permissions: parsed.Permission, Limit: parsed.Limit,
	})
	if err != nil {
		return ToolResult{Err: asError(err)}
	}
	summaries := make([]operationSummary, 0, len(operations))
	for _, operation := range operations {
		summary := operationSummary{
			OperationID: operation.OperationID,
			Title:       operation.Title,
			Summary:     operation.Summary,
			Category:    operation.Category,
			Permissions: operation.Permissions,
			Approval:    Decide(operation, a.policy(ctx)).Decision,
		}
		if operation.Execution != nil {
			summary.Risk = operation.Execution.Risk
		}
		if operation.Output != nil {
			summary.OutputMode = operation.Output.Mode
		}
		summaries = append(summaries, summary)
	}
	return marshalResult(map[string]any{"count": len(summaries), "operations": summaries})
}

type describeArguments struct {
	OperationID    string `json:"operationId"`
	IncludeExample bool   `json:"includeExample"`
}

func (a *Agent) describe(ctx context.Context, arguments json.RawMessage) ToolResult {
	var parsed describeArguments
	if err := decodeArguments(arguments, &parsed); err != nil {
		return ToolResult{Err: err}
	}
	if strings.TrimSpace(parsed.OperationID) == "" {
		return ToolResult{Err: &Error{Code: CodeInvalidArguments, Message: "operationId is required"}}
	}
	operation, err := a.client.Describe(ctx, parsed.OperationID)
	if err != nil {
		return ToolResult{Err: asError(err)}
	}
	judgement := Decide(operation, a.policy(ctx))
	payload := map[string]any{
		"operation":    operation,
		"approval":     judgement.Decision,
		"approvalWhy":  judgement.Reason,
		"retryAllowed": AllowsRetry(operation),
		"autonomyMode": string(a.autonomy),
	}
	if parsed.IncludeExample {
		if example, exampleErr := a.client.Example(ctx, parsed.OperationID); exampleErr == nil {
			payload["example"] = example
		}
	}
	return marshalResult(payload)
}

type invokeArguments struct {
	OperationID string          `json:"operationId"`
	Input       json.RawMessage `json:"input"`
	Reason      string          `json:"reason"`
}

func (a *Agent) invoke(ctx context.Context, arguments json.RawMessage) ToolResult {
	var parsed invokeArguments
	if err := decodeArguments(arguments, &parsed); err != nil {
		return a.record(ToolInvoke, "", ToolResult{Err: err})
	}
	operationID := strings.TrimSpace(parsed.OperationID)
	if operationID == "" {
		return a.record(ToolInvoke, "", ToolResult{Err: &Error{
			Code: CodeInvalidArguments, Message: "operationId is required",
		}})
	}

	// 계약을 먼저 읽는다. 승인 판정의 입력이 전부 계약에 있고, 그것을 읽지 않고
	// 부르는 경로가 있으면 그 경로가 게이트를 우회하는 경로가 된다.
	operation, err := a.client.Describe(ctx, operationID)
	if err != nil {
		return a.recordCall(ToolInvoke, operationID, "", parsed.Reason, ToolResult{Err: asError(err)})
	}

	judgement := Decide(operation, a.policy(ctx))

	// 계획만 하는 실행에서는 판정까지만 하고 부르지 않는다. 판정을 건너뛰면
	// "실제로 돌렸다면 물어봤을 것인가"를 계획이 말하지 못한다.
	if a.dryRun {
		return a.recordPlanned(operationID, judgement, parsed.Reason, marshalResult(map[string]any{
			"dryRun":      true,
			"operationId": operationID,
			"input":       rawOrEmpty(parsed.Input),
			"approval":    judgement.Decision,
			"approvalWhy": judgement.Reason,
			"wouldRun":    judgement.Decision == DecisionRun,
			"hint":        "dry run: nothing was called; this is what the call would have met",
		}))
	}

	// 스트림은 도구 결과 하나로 돌려주지 않는다. 로그 스트림 전문을 컨텍스트에
	// 붓는 것은 노드가 출력하는 임의의 텍스트를 프롬프트에 넣는 것이고, 커서
	// 방식이 생기기 전까지는 거절이 정직한 답이다. 사람에게 묻기 전에 거절한다
	// — 어차피 부를 수 없는 것을 승인해 달라고 묻지 않는다.
	if operation.Output != nil && operation.Output.Mode == "stream" {
		return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
			Code:    CodeStreamUnsupported,
			Message: "operation " + operationID + " answers as a stream, which this tool surface does not carry",
		}})
	}

	switch judgement.Decision {
	case DecisionRun:
	case DecisionConfirm:
		if a.approver == nil {
			return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
				Code: CodeApprovalRequired,
				Message: "this operation needs a person's approval and this session has nobody to ask (" +
					judgement.Reason + "); a human can run it with `terra module call " + operationID + "`",
			}})
		}
		approved, askErr := a.approver.Approve(ctx, ApprovalRequest{
			OperationID: operationID, Input: rawOrEmpty(parsed.Input), Reason: parsed.Reason,
			Judgement: judgement, Operation: operation,
		})
		if askErr != nil {
			return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
				Code:    CodeApprovalRequired,
				Message: "approval for " + operationID + " was not given: " + askErr.Error(),
			}})
		}
		if !approved {
			return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
				Code:    CodeApprovalDenied,
				Message: "a person declined " + operationID + "; do not retry it, explain what you would have done instead",
			}})
		}
	default:
		// plan: 제안만 한다. refuse: 물을 사람이 없는 모드다. 둘 다 부르지 않는다.
		return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
			Code: CodeApprovalRequired,
			Message: "this operation needs a person's approval and this session has nobody to ask (" +
				judgement.Reason + "); a human can run it with `terra module call " + operationID + "`",
		}})
	}

	// 같은 호출을 되풀이하지 않는다 — 계약이 "다시 부르지 말라"고 한 operation은.
	// 앞선 시도가 Gateway에 닿은 뒤 실패했다면(전송 오류·5xx) 그것이 실행됐는지
	// 아무도 모르고, 되풀이는 그 모르는 것을 두 번 하는 일이다.
	repeatKey := invokeKey(operationID, parsed.Input)
	if previous, failed := a.dispatched[repeatKey]; failed && !AllowsRetry(operation) {
		return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: &Error{
			Code: CodeRetryRefused,
			Message: "an identical call to " + operationID + " already failed after it may have reached " +
				"the provider (" + previous.Code + "), and the contract declares it must not be retried; " +
				"a person has to check whether it ran",
		}})
	}

	response, traceID, invokeErr := a.client.Invoke(ctx, operationID, parsed.Input)
	if invokeErr != nil {
		failure := asError(invokeErr)
		if failure.Status == 0 || failure.Status >= 500 {
			a.dispatched[repeatKey] = failure
		}
		return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, ToolResult{Err: failure, TraceID: failure.TraceID})
	}
	delete(a.dispatched, repeatKey)

	payload := map[string]any{"operationId": operationID, "result": response}
	if traceID != "" {
		// The model sees it too: an answer that quotes the trace id of the call
		// it is reporting is one an operator can check.
		payload["traceId"] = traceID
	}
	if operation.Output != nil {
		payload["outputMode"] = operation.Output.Mode
		// accepted-job은 "받아들였다"이지 "끝났다"가 아니다. 그 차이를 말하지
		// 않으면 호출자가 큐에 들어간 작업을 완료로 읽고 다음 단계로 넘어간다.
		if operation.Output.Mode == "accepted-job" {
			payload["accepted"] = true
			payload["hint"] = "accepted, not finished: find this provider's task operation with " +
				ToolSearch + " (query \"task\") and poll it with the returned id"
			if taskID := findTaskID(response); taskID != "" {
				payload["taskId"] = taskID
			}
		}
	}
	result := marshalResult(payload)
	result.TraceID = traceID
	return a.recordCall(ToolInvoke, operationID, judgement, parsed.Reason, result)
}

// nodes answers the fleet vocabulary. A credential that cannot reach past its
// own node is refused here as well as at the Gateway: the tool was not offered
// to it, so a call to it is the model reaching for something it was not given.
func (a *Agent) nodes(ctx context.Context) ToolResult {
	if !a.fleetReach(ctx) {
		return ToolResult{Err: &Error{
			Code: CodeReachExceeded,
			Message: "this credential reaches its own node only; ask a person for a grant with " +
				"cluster reach (`terra agent grant --level fleet`) if the task needs another node",
		}}
	}
	nodes, err := a.client.Nodes(ctx)
	if err != nil {
		return ToolResult{Err: asError(err)}
	}
	return marshalResult(map[string]any{"count": len(nodes.Nodes), "nodes": nodes.Nodes})
}

// callExternal runs one allowlisted external tool through the same gate every
// other call goes through, and returns its answer fenced as data.
func (a *Agent) callExternal(ctx context.Context, server, tool string, arguments json.RawMessage) ToolResult {
	registered, known := a.externalTool(server, tool)
	if !known || a.externalCaller == nil {
		// A name in the external shape that names nothing registered. It is
		// not "not found" in the ordinary sense — it is a call to a server
		// nobody allowlisted, and saying so is the useful answer.
		return a.record(ExternalToolPrefix+server+"__"+tool, "", ToolResult{Err: &Error{
			Code:    CodeUnknownTool,
			Message: "no external server named " + quoted(server) + " is registered for this session",
		}})
	}
	qualified := registered.QualifiedName()
	judgement := DecideExternal(registered, a.policy(ctx))

	if a.dryRun {
		return a.recordExternal(qualified, judgement, StatusPlanned, 0, marshalResult(map[string]any{
			"dryRun": true, "server": server, "tool": tool,
			"arguments":   rawOrEmpty(arguments),
			"approval":    judgement.Decision,
			"approvalWhy": judgement.Reason,
			"hint":        "dry run: the external server was not called",
		}))
	}

	switch judgement.Decision {
	case DecisionRun:
	case DecisionConfirm:
		if a.approver == nil {
			return a.recordExternal(qualified, judgement, StatusRefused, 0, ToolResult{Err: &Error{
				Code: CodeApprovalRequired,
				Message: "calling " + qualified + " needs a person's approval (" + judgement.Reason +
					") and this session has nobody to ask",
			}})
		}
		approved, askErr := a.approver.Approve(ctx, ApprovalRequest{
			OperationID: qualified, Input: rawOrEmpty(arguments), Judgement: judgement,
		})
		if askErr != nil {
			return a.recordExternal(qualified, judgement, StatusRefused, 0, ToolResult{Err: &Error{
				Code: CodeApprovalRequired, Message: "approval for " + qualified + " was not given: " + askErr.Error(),
			}})
		}
		if !approved {
			return a.recordExternal(qualified, judgement, StatusRefused, 0, ToolResult{Err: &Error{
				Code: CodeApprovalDenied, Message: "a person declined " + qualified,
			}})
		}
	default:
		return a.recordExternal(qualified, judgement, StatusRefused, 0, ToolResult{Err: &Error{
			Code: CodeApprovalRequired, Message: judgement.Reason,
		}})
	}

	answer, err := a.externalCaller.CallExternal(ctx, server, tool, arguments)
	if err != nil {
		return a.recordExternal(qualified, judgement, StatusError, 0, ToolResult{Err: &Error{
			Code: CodeExternalFailed, Message: "the external server " + quoted(server) + " did not answer: " + err.Error(),
		}})
	}
	// Fenced whether or not the server called it an error: a failure message
	// from a third party is third-party text too (R2).
	payload := map[string]any{
		"server": server, "tool": tool,
		"content":   FenceExternal(server, tool, answer),
		"isError":   answer.IsError,
		"truncated": answer.Truncated,
	}
	status := StatusOK
	if answer.IsError {
		status = StatusError
	}
	return a.recordExternal(qualified, judgement, status, len(answer.Content), marshalResult(payload))
}

// externalTool finds a registered tool by server and name.
func (a *Agent) externalTool(server, tool string) (ExternalTool, bool) {
	for _, candidate := range a.external {
		if candidate.Server == server && candidate.Name == tool {
			return candidate, true
		}
	}
	return ExternalTool{}, false
}

// recordExternal writes the record for an external call. It carries the
// qualified name so a reader can tell at a glance that the call left Terra —
// and the Gateway audit will have no line for it (R8).
func (a *Agent) recordExternal(qualified string, judgement Judgement, status string, resultBytes int, result ToolResult) ToolResult {
	a.calls++
	a.recorder.Record(Record{
		Time: a.now().UTC(), Tool: qualified, OperationID: qualified,
		Decision: judgement.Decision, Judgement: judgement.Reason,
		Status: status, ErrorCode: errorCodeOf(result),
		External: true, ResultBytes: resultBytes,
	})
	return result
}

func errorCodeOf(result ToolResult) string {
	if result.Err == nil {
		return ""
	}
	return result.Err.Code
}

func (a *Agent) session(ctx context.Context) ToolResult {
	session, err := a.client.Whoami(ctx)
	if err != nil {
		return ToolResult{Err: asError(err)}
	}
	return marshalResult(map[string]any{
		"session":      session,
		"autonomyMode": string(a.autonomy),
		"toolCalls":    a.calls,
	})
}

// findTaskID looks for a task id in a provider's accepted-job envelope without
// assuming whose envelope it is. A hardcoded path would be one provider's shape
// baked into the agent, which is the coupling this whole surface avoids.
func findTaskID(response json.RawMessage) string {
	var decoded any
	if json.Unmarshal(response, &decoded) != nil {
		return ""
	}
	return searchTaskID(decoded, 0)
}

func searchTaskID(value any, depth int) string {
	if depth > 4 {
		return ""
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"task_id", "taskId", "id"} {
		if found, isString := object[key].(string); isString && strings.TrimSpace(found) != "" {
			return found
		}
	}
	for _, nested := range object {
		if found := searchTaskID(nested, depth+1); found != "" {
			return found
		}
	}
	return ""
}

func decodeArguments(arguments json.RawMessage, out any) *Error {
	if len(strings.TrimSpace(string(arguments))) == 0 {
		return nil
	}
	if err := json.Unmarshal(arguments, out); err != nil {
		return &Error{Code: CodeInvalidArguments, Message: err.Error()}
	}
	return nil
}

func marshalResult(payload any) ToolResult {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ToolResult{Err: &Error{Code: "TERRA_ENCODE_FAILED", Message: err.Error()}}
	}
	return ToolResult{Payload: encoded}
}

func asError(err error) *Error {
	if typed, ok := err.(*Error); ok {
		return typed
	}
	return &Error{Code: "TERRA_INTERNAL", Message: err.Error()}
}

// rawOrEmpty makes an absent input an empty object, so a record and a plan
// never carry a nil where a caller expects JSON.
func rawOrEmpty(input json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(input))) == 0 {
		return json.RawMessage("{}")
	}
	return input
}

// invokeKey identifies one call by operation and canonical input, so a repeat
// is recognised whatever key order the model used the second time.
func invokeKey(operationID string, input json.RawMessage) string {
	var decoded any
	if json.Unmarshal(rawOrEmpty(input), &decoded) == nil {
		if canonical, err := json.Marshal(decoded); err == nil {
			return operationID + "\n" + string(canonical)
		}
	}
	return operationID + "\n" + string(input)
}

func (a *Agent) record(tool, operationID string, result ToolResult) ToolResult {
	return a.recordCall(tool, operationID, nil, "", result)
}

// recordPlanned writes the record of a call that was deliberately not made.
func (a *Agent) recordPlanned(operationID string, judgement Judgement, reason string, result ToolResult) ToolResult {
	a.calls++
	a.recorder.Record(Record{
		Time: a.now().UTC(), Tool: ToolInvoke, OperationID: operationID, Reason: reason,
		Decision: judgement.Decision, Judgement: judgement.Reason, Status: StatusPlanned,
	})
	return result
}

// recordCall writes one record. judgement may be a Judgement or nil.
func (a *Agent) recordCall(tool, operationID string, judgement any, reason string, result ToolResult) ToolResult {
	a.calls++
	record := Record{
		Time:        a.now().UTC(),
		Tool:        tool,
		OperationID: operationID,
		Reason:      reason,
		TraceID:     result.TraceID,
		Status:      StatusOK,
	}
	if typed, ok := judgement.(Judgement); ok {
		record.Decision = typed.Decision
		record.Judgement = typed.Reason
	}
	if result.Err != nil {
		record.ErrorCode = result.Err.Code
		record.Status = StatusError
		switch result.Err.Code {
		case CodeApprovalRequired, CodeApprovalDenied, CodeStreamUnsupported, CodeRetryRefused:
			record.Status = StatusRefused
		}
	}
	a.recorder.Record(record)
	return result
}
