package inbound

import "testing"

func TestRerankInboundRegistration(t *testing.T) {
	if Get(InboundTypeRerank) == nil {
		t.Fatal("expected rerank inbound factory")
	}
}
