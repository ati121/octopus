package model_test

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/looplj/axonhub/llm"
)

func TestNativeProtocolObjectsRetainPrivateFields(t *testing.T) {
	request, err := axon.NewInbound(6).TransformRequest(t.Context(), []byte(`{"model":"group","input":"hello","questions":[{"type":"noul","name":"probe","instructions":"ping?"}],"extension":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Decisions == nil || len(request.Decisions.Body) == 0 {
		t.Fatal("native Decisions body lost")
	}
	request.Decisions = &llm.DecisionsRequest{Body: []byte(`{"input":"native"}`)}
	native, err := axon.NativeRequest(t.Context(), request)
	if err != nil || string(native.Decisions.Body) != `{"input":"native"}` {
		t.Fatalf("native object not authoritative: %v", err)
	}
}

func TestNativeConversationExtension(t *testing.T) {
	request, err := axon.NewInbound(1).TransformRequest(t.Context(), []byte(`{"model":"group","input":"hello","conversation":{"id":"conv_1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !request.HasOpenAIConversation() {
		t.Fatal("conversation extension not detected")
	}
}
