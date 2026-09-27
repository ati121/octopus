package relay

import (
	"fmt"
	"testing"
)

func TestPublicRelayErrorMessagePreservesUpstreamJSONMessage(t *testing.T) {
	err := newUpstreamHTTPError(400, []byte(`{"error":{"message":"thinking.budget_tokens must be less than max_tokens","type":"invalid_request_error"},"type":"error"}`))
	wrapped := fmt.Errorf("channel claude failed: %w", err)

	if got, want := publicRelayErrorMessage(wrapped), "thinking.budget_tokens must be less than max_tokens"; got != want {
		t.Fatalf("publicRelayErrorMessage() = %q, want %q", got, want)
	}
}

func TestExtractUpstreamErrorMessageVariants(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "nested error", body: `{"error":{"message":"nested"}}`, want: "nested"},
		{name: "top level", body: `{"message":"top-level"}`, want: "top-level"},
		{name: "string error", body: `{"error":"plain"}`, want: "plain"},
		{name: "non json", body: `<html>bad gateway</html>`, want: ""},
		{name: "fastapi string detail", body: `{"detail":"Not authenticated"}`, want: "Not authenticated"},
		{name: "fastapi validation detail", body: `{"detail":[{"loc":["body","questions","is_urgent","criteria",0],"msg":"Input should be a valid string","type":"string_type"},{"loc":["body","state"],"msg":"Field required","type":"missing"}]}`, want: "body.questions.is_urgent.criteria.0: Input should be a valid string; body.state: Field required"},
		{name: "fastapi detail without loc", body: `{"detail":[{"msg":"Overloaded"}]}`, want: "Overloaded"},
		{name: "error takes precedence over detail", body: `{"error":{"message":"nested"},"detail":"ignored"}`, want: "nested"},
		{name: "unrecognized detail", body: `{"detail":{"code":1}}`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractUpstreamErrorMessage([]byte(tt.body)); got != tt.want {
				t.Fatalf("extractUpstreamErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicRelayErrorMessageFallsBackForUnstructuredErrors(t *testing.T) {
	if got := publicRelayErrorMessage(fmt.Errorf("dial failed")); got != "channel failed" {
		t.Fatalf("publicRelayErrorMessage() = %q, want channel failed", got)
	}
	if got := publicRelayErrorMessage(newUpstreamHTTPError(502, []byte(`<html>bad gateway</html>`))); got != "channel failed" {
		t.Fatalf("unstructured upstream error should stay private, got %q", got)
	}
}
