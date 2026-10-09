package agentcore

import (
	"context"
	"encoding/json"
	"time"
)

// 이 파일은 컴파일로 지키는 공개 API 보증이다. 심볼 하나가 사라지거나 모양이
// 바뀌면 이 패키지가 빌드되지 않는다. 소비자는 둘이다 — terra-cli와 modules의
// io.terra.agent 모듈. 목록과 근거는 docs/api.md.

// terra-cli: internal/{mcp/server.go, app/mcp.go, client/client.go}.
var (
	_ *Agent                                                    = (*Agent)(nil)
	_ func(Transport, Options) *Agent                           = New
	_ Options                                                   = Options{Autonomy: AutonomyAsk, Recorder: NopRecorder{}}
	_ Recorder                                                  = RecorderFunc(func(Record) {})
	_ Recorder                                                  = NopRecorder{}
	_ Record                                                    = Record{}
	_ Autonomy                                                  = AutonomyPlan
	_ Autonomy                                                  = AutonomyAsk
	_ Autonomy                                                  = AutonomyAuto
	_ Autonomy                                                  = AutonomyUnattended
	_ func(string) bool                                         = KnownAutonomy
	_ Answer                                                    = Answer{Status: 0, Body: nil, TraceID: ""}
	_ []string                                                  = []string{ToolDescribe, ToolInvoke, ToolSearch, ToolSession}
	_ func() []Tool                                             = Tools
	_ func(context.Context, string, json.RawMessage) ToolResult = (*Agent)(nil).Call
	_ Transport                                                 = transportFunc(nil)
)

// modules: io.terra.agent.
var (
	_ Approver                            = ApproverFunc(nil)
	_ ApprovalRequest                     = ApprovalRequest{Operation: CatalogOperation{}}
	_ *Error                              = (*Error)(nil)
	_ ExternalResult                      = ExternalResult{}
	_ ExternalTool                        = ExternalTool{}
	_ func(Transport) *Client             = NewClient
	_ func(string) (string, string, bool) = ParseExternalToolName
	_ string                              = StatusPlanned
	_ Tool                                = Tool{}
	_ ToolResult                          = ToolResult{}
	_ func(string) bool                   = ValidExternalName
	_ func() time.Time                    = Options{}.Now
)

type transportFunc func(ctx context.Context, method, path string, body []byte) (Answer, error)

func (f transportFunc) Do(ctx context.Context, method, path string, body []byte) (Answer, error) {
	return f(ctx, method, path, body)
}

// modules io.terra.agent가 추가로 쓰는 심볼 (modules a78819d 실측).
var (
	_ Decision                             = DecisionRun
	_ Decision                             = DecisionPlan
	_ Decision                             = DecisionConfirm
	_ Decision                             = DecisionRefuse
	_ func(ExternalTool, Policy) Judgement = DecideExternal
	_ Policy                               = Policy{}
	_ []string                             = []string{StatusOK, StatusRefused, StatusError, ToolNodes, ExternalToolPrefix, CodeUnknownTool, CodeApprovalRequired, CodeApprovalDenied, CodeRetryRefused}
)
