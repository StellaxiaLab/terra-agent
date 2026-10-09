package agentcore

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// Answer is one Gateway response as a transport hands it back: the status, the
// raw body, and the Gateway's own id for the call.
//
// The trace id is here rather than left to the caller to dig out because it is
// the only thing that can join what an agent recorded to what the Gateway
// audited, and it does not travel in the body of a successful call — only in
// the response header, which a transport is the last place that still sees.
// A refusal repeats it inside its error envelope; a success would carry
// nothing at all.
type Answer struct {
	Status  int
	Body    []byte
	TraceID string
}

// Transport is the one thing agentcore needs from its host: the ability to make
// a request against the local Gateway.
//
// It returns the answer and translates nothing. That is deliberate — the
// Gateway answers failures with the contract's own error code (§21), and a
// transport that mapped those onto its own error type would erase exactly what
// the caller has to see. Only a transport FAILURE — nothing was answered at
// all — is an err.
type Transport interface {
	Do(ctx context.Context, method, path string, body []byte) (Answer, error)
}

// Error is a Gateway refusal, carried whole.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"traceId,omitempty"`
	Status    int    `json:"status,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// Client reads the Gateway catalog and invokes operations through it.
//
// Every path here is the Gateway's. There is no second route to a provider —
// not to the daemon's Local API, not to the Master's REST — because a second
// route is a second permission gate, and the one that drifts later is the one
// that lets something through.
type Client struct {
	transport Transport
}

// NewClient returns a Client over transport.
func NewClient(transport Transport) *Client { return &Client{transport: transport} }

// Query narrows a catalog search. The Gateway intersects the caller's own
// grants with it, so a query can only ever look at less than the caller holds.
type Query struct {
	Text        string
	Category    string
	Tag         string
	Permissions []string
	Limit       int
}

func (q Query) values() url.Values {
	values := url.Values{}
	if text := strings.TrimSpace(q.Text); text != "" {
		values.Set("search", text)
	}
	if category := strings.TrimSpace(q.Category); category != "" {
		values.Set("category", category)
	}
	if tag := strings.TrimSpace(q.Tag); tag != "" {
		values.Set("tag", tag)
	}
	for _, permission := range q.Permissions {
		if permission = strings.TrimSpace(permission); permission != "" {
			values.Add("permission", permission)
		}
	}
	return values
}

type catalogListing struct {
	Generation string             `json:"generation"`
	Count      int                `json:"count"`
	Operations []CatalogOperation `json:"operations"`
}

// Search returns the operations this caller may see, narrowed by query.
func (c *Client) Search(ctx context.Context, query Query) ([]CatalogOperation, error) {
	path := "/api/v1/catalog"
	if encoded := query.values().Encode(); encoded != "" {
		path += "?" + encoded
	}
	var listing catalogListing
	if err := c.get(ctx, path, &listing); err != nil {
		return nil, err
	}
	operations := listing.Operations
	if query.Limit > 0 && len(operations) > query.Limit {
		operations = operations[:query.Limit]
	}
	return operations, nil
}

// Describe returns one operation with its input schema.
func (c *Client) Describe(ctx context.Context, operationID string) (CatalogOperation, error) {
	var operation CatalogOperation
	err := c.get(ctx, "/api/v1/catalog/operations/"+url.PathEscape(operationID), &operation)
	return operation, err
}

// Example returns the operation's declared input/output examples, if any.
func (c *Client) Example(ctx context.Context, operationID string) (json.RawMessage, error) {
	body, _, err := c.raw(ctx, "GET", "/api/v1/catalog/operations/"+url.PathEscape(operationID)+"/example", nil)
	return body, err
}

// Session describes the credential this client presents.
type Session struct {
	Principal      string   `json:"principal"`
	Permissions    []string `json:"permissions"`
	AllPermissions bool     `json:"allPermissions"`
	Delegate       string   `json:"delegate,omitempty"`
	Reach          string   `json:"reach,omitempty"`
	ReachLimited   bool     `json:"reachLimited"`
	AgentSessionID string   `json:"agentSessionId,omitempty"`
	// Unattended says this credential was issued to run with nobody watching,
	// and PreApproved is what a person said yes to in advance (A7). A session
	// that can read its own limits plans more accurately than one that guesses.
	Unattended  bool     `json:"unattended,omitempty"`
	PreApproved []string `json:"preApproved,omitempty"`
	IssuedAt    string   `json:"issuedAt,omitempty"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
}

// Node is one node this credential may address.
type Node struct {
	NodeID      string   `json:"nodeId"`
	DisplayName string   `json:"displayName,omitempty"`
	Status      string   `json:"status,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	ClusterID   string   `json:"clusterId,omitempty"`
}

// NodeListing is the answer to "what can I reach".
type NodeListing struct {
	Count int    `json:"count"`
	Nodes []Node `json:"nodes"`
}

// Nodes asks the Gateway which nodes this credential may address (A6). The
// Gateway relays the question to the Master as a Core peer with the caller's
// session id, so no user bearer is needed and none is held.
func (c *Client) Nodes(ctx context.Context) (NodeListing, error) {
	var listing NodeListing
	err := c.get(ctx, "/api/v1/agent/nodes", &listing)
	if listing.Nodes == nil {
		listing.Nodes = []Node{}
	}
	return listing, err
}

// Whoami asks the Gateway what the presented credential can actually do.
func (c *Client) Whoami(ctx context.Context) (Session, error) {
	var session Session
	err := c.get(ctx, "/api/v1/agent/whoami", &session)
	return session, err
}

// Invoke calls one operation. It performs NO approval check — Agent does that,
// once, before it gets here — and no retry: whether a failed call may be tried
// again is a property of the operation (AllowsRetry), not of the transport.
// It returns the provider's answer and the Gateway's trace id for the call —
// the id that has to end up in the record for the call to be attributable
// afterwards.
func (c *Client) Invoke(ctx context.Context, operationID string, input json.RawMessage) (json.RawMessage, string, error) {
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	return c.raw(ctx, "POST", "/api/v1/operations/"+url.PathEscape(operationID)+"/invoke", input)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	body, _, err := c.raw(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Code: "GATEWAY_MALFORMED_RESPONSE", Message: path + ": " + err.Error()}
	}
	return nil
}

func (c *Client) raw(ctx context.Context, method, path string, body []byte) (json.RawMessage, string, error) {
	answer, err := c.transport.Do(ctx, method, path, body)
	if err != nil {
		return nil, answer.TraceID, &Error{Code: "GATEWAY_UNREACHABLE", Message: err.Error(), TraceID: answer.TraceID, Retryable: true}
	}
	if answer.Status >= 400 {
		return nil, answer.TraceID, decodeGatewayError(answer)
	}
	if len(answer.Body) == 0 {
		return json.RawMessage("null"), answer.TraceID, nil
	}
	return answer.Body, answer.TraceID, nil
}

// decodeGatewayError reads the Gateway's {"error":{code,message,traceId}} shape.
// A body that is not that shape still has to say something actionable, so the
// status stands in rather than being swallowed.
func decodeGatewayError(answer Answer) *Error {
	status, body := answer.Status, answer.Body
	var envelope struct {
		Error *Error `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &envelope) == nil && envelope.Error != nil && envelope.Error.Code != "" {
		envelope.Error.Status = status
		if envelope.Error.TraceID == "" {
			// The envelope usually repeats it; when it does not, the header
			// still saw it and a refusal without a trace id is a refusal
			// nobody can look up.
			envelope.Error.TraceID = answer.TraceID
		}
		// 5xx와 429만 다시 불러서 답이 달라질 수 있는 자리다. 403을 retryable로
		// 표시하면 모델이 같은 거부를 반복한다.
		envelope.Error.Retryable = status >= 500 || status == 429
		return envelope.Error
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = "gateway request failed"
	}
	return &Error{
		Code:      "HTTP_" + strconv.Itoa(status),
		Message:   message,
		TraceID:   answer.TraceID,
		Status:    status,
		Retryable: status >= 500 || status == 429,
	}
}
