package agentcore

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// summaryLimit bounds InputSummary and ResultSummary in bytes. A record is a
// line of a transcript, not a copy of the traffic.
const summaryLimit = 480

// NOTE: this masking list is a deliberate COPY of the terra CLI's, not an
// import. The core promises zero external dependencies (imports_test.go), so it
// cannot link the CLI's output package; the two lists must be kept in step by
// hand. If they drift, the CLI's list is the one that is right.
//
// secretMarkers are the key-name fragments that make a value a secret. The
// first six are the terra CLI's own list (products/common/apps/terra-cli,
// output.secretKey); api_key and apikey are added because this module's
// contract declares /api_key a secret pointer and the CLI's list does not
// match it. "env" is matched as a whole key, not a fragment: it is the
// contract's /env pointer, and a fragment would swallow "environment".
var secretMarkers = []string{
	"token", "secret", "password", "private_key", "authorization", "credential",
	"api_key", "apikey",
}

const redacted = "***"

func secretKey(key string) bool {
	key = strings.ToLower(key)
	if key == "env" {
		return true
	}
	for _, marker := range secretMarkers {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func maskSecrets(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		masked := make(map[string]any, len(typed))
		for key, item := range typed {
			if secretKey(key) {
				masked[key] = redacted
			} else {
				masked[key] = maskSecrets(item)
			}
		}
		return masked
	case []any:
		masked := make([]any, len(typed))
		for i, item := range typed {
			masked[i] = maskSecrets(item)
		}
		return masked
	default:
		return value
	}
}

// summarizeInput renders a call's input as bounded JSON text with secret-named
// values masked. It returns "" for input that is absent or is not JSON: a value
// that cannot be walked cannot be masked, so it is not carried.
func summarizeInput(input json.RawMessage) string {
	if len(strings.TrimSpace(string(input))) == 0 {
		return ""
	}
	var decoded any
	if json.Unmarshal(input, &decoded) != nil {
		return ""
	}
	encoded, err := json.Marshal(maskSecrets(decoded))
	if err != nil {
		return ""
	}
	return bound(string(encoded))
}

// summarizeResult is summarizeInput for a Gateway answer, with a non-JSON body
// dropped for the same reason.
func summarizeResult(result ToolResult, response json.RawMessage) string {
	if result.Err != nil {
		return bound(strings.TrimSpace(result.Err.Code + ": " + result.Err.Message))
	}
	return summarizeInput(response)
}

func bound(text string) string {
	if len(text) <= summaryLimit {
		return text
	}
	cut := summaryLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
