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
