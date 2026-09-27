package inbound

import "testing"

func TestRerankInboundRegistration(t *testing.T) {
	if Get(InboundTypeRerank) == nil {
		t.Fatal("expected rerank inbound factory")
	}
}

func TestSystemOneInboundRegistration(t *testing.T) {
	if InboundTypeSystemOne != 5 {
		t.Fatalf("expected SystemOne inbound type to be appended as 5, got %d", InboundTypeSystemOne)
	}
	if Get(InboundTypeSystemOne) == nil {
		t.Fatal("expected system one inbound factory")
	}
}
