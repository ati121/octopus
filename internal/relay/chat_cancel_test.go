package relay

import (
	"testing"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestChatResponseProtocolCompleted(t *testing.T) {
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
			if got := chatResponseProtocolCompleted(tt.response); got != tt.want {
				t.Fatalf("chatResponseProtocolCompleted() = %t, want %t", got, tt.want)
			}
		})
	}
}
