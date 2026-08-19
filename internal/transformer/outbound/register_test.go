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
