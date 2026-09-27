package outbound

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestOutboundTypeValuesRemainPersistenceSafe(t *testing.T) {
	if OutboundTypeCodex != 6 {
		t.Fatalf("expected Codex channel type to remain 6, got %d", OutboundTypeCodex)
	}
	if OutboundTypeRerank != 7 {
		t.Fatalf("expected Rerank channel type to be appended as 7, got %d", OutboundTypeRerank)
	}
	if OutboundTypeSystemOne != 8 {
		t.Fatalf("expected SystemOne channel type to be appended as 8, got %d", OutboundTypeSystemOne)
	}
}

func TestRerankOutboundRegistration(t *testing.T) {
	if APIFormatOf(OutboundTypeRerank) != model.APIFormatRerank {
		t.Fatalf("expected rerank API format, got %q", APIFormatOf(OutboundTypeRerank))
	}
	if Get(OutboundTypeRerank) == nil {
		t.Fatal("expected rerank outbound factory")
	}
	if !IsRerankChannelType(OutboundTypeRerank) {
		t.Fatal("expected rerank channel to accept rerank requests")
	}
	if IsRerankChannelType(OutboundTypeOpenAIChat) || IsRerankChannelType(OutboundTypeOpenAIEmbedding) {
		t.Fatal("expected chat and embedding channels not to accept rerank requests")
	}
	if IsChatChannelType(OutboundTypeRerank) || IsEmbeddingChannelType(OutboundTypeRerank) {
		t.Fatal("expected rerank channel not to accept chat or embedding requests")
	}
}

func TestSystemOneOutboundRegistration(t *testing.T) {
	if APIFormatOf(OutboundTypeSystemOne) != model.APIFormatSystemOne {
		t.Fatalf("expected system one API format, got %q", APIFormatOf(OutboundTypeSystemOne))
	}
	if Get(OutboundTypeSystemOne) == nil {
		t.Fatal("expected system one outbound factory")
	}
	if !IsValidChannelType(OutboundTypeSystemOne) {
		t.Fatal("expected system one to be a valid channel type")
	}
	if !IsSystemOneChannelType(OutboundTypeSystemOne) {
		t.Fatal("expected system one channel to accept system one requests")
	}
	for _, channelType := range []OutboundType{OutboundTypeOpenAIChat, OutboundTypeOpenAIResponse, OutboundTypeOpenAIEmbedding, OutboundTypeRerank} {
		if IsSystemOneChannelType(channelType) {
			t.Fatalf("expected channel type %d not to accept system one requests", channelType)
		}
	}
	if IsChatChannelType(OutboundTypeSystemOne) || IsEmbeddingChannelType(OutboundTypeSystemOne) || IsRerankChannelType(OutboundTypeSystemOne) {
		t.Fatal("expected system one channel not to accept chat, embedding or rerank requests")
	}
}
