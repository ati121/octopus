package relay

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/tidwall/gjson"
)

func TestNativeReplayClearsConversationAndKeepsOriginal(t *testing.T) {
	original, err := axon.NewInbound(1).TransformRequest(t.Context(), []byte(`{"model":"group","input":[{"role":"user","content":"next"}],"previous_response_id":"resp_old","conversation":{"id":"conv_1"},"tools":[{"type":"apply_patch"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	state := &wsConversationState{LastResponseID: "resp_old", ReplayWindowItems: []byte(`[{"role":"user","content":"first"}]`)}
	replay := state.BuildReplayRequest(original)
	if replay == nil || replay.HasOpenAIConversation() || replay.PreviousResponseID != nil {
		t.Fatal("replay retained native continuation")
	}
	wire, err := axon.NewOutbound(1).WireRequest(t.Context(), replay, "https://unused.invalid/v1", "key")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(wire.Body, "conversation").Exists() || gjson.GetBytes(wire.Body, "previous_response_id").Exists() {
		t.Fatal("continuation leaked into replay wire")
	}
	if len(gjson.GetBytes(wire.Body, "input").Array()) != 2 || gjson.GetBytes(wire.Body, "tools.0.type").String() != "apply_patch" {
		t.Fatalf("replay lost native fields: %s", wire.Body)
	}
	if !original.HasOpenAIConversation() || original.OpenAIPreviousResponseID() != "resp_old" {
		t.Fatal("replay mutated original")
	}
}
