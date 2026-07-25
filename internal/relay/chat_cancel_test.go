package relay

import (
	"testing"

	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestChatStreamResponseCompleted(t *testing.T) {
	stop := "stop"
	empty := ""

	tests := []struct {
		name     string
		response *transformerModel.InternalLLMResponse
		want     bool
	}{
		{name: "nil response", response: nil, want: false},
		{name: "no choices", response: &transformerModel.InternalLLMResponse{}, want: false},
		{
			name: "missing finish reason",
			response: &transformerModel.InternalLLMResponse{
				Choices: []transformerModel.Choice{{Index: 0}},
			},
			want: false,
		},
		{
			name: "empty finish reason",
			response: &transformerModel.InternalLLMResponse{
				Choices: []transformerModel.Choice{{Index: 0, FinishReason: &empty}},
			},
			want: false,
		},
		{
			name: "completed choice",
			response: &transformerModel.InternalLLMResponse{
				Choices: []transformerModel.Choice{{Index: 0, FinishReason: &stop}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamResponseCompleted(tt.response); got != tt.want {
				t.Fatalf("streamResponseCompleted() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestInboundStreamTerminalEventsForOpenAIResponses(t *testing.T) {
	events := inboundStreamTerminalEvents(&openaiInbound.ResponseInbound{})
	for _, eventType := range []string{"response.completed", "response.incomplete", "response.failed"} {
		if _, ok := events[eventType]; !ok {
			t.Fatalf("missing Responses terminal event %q", eventType)
		}
	}
}
