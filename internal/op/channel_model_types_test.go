package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelModelTypesPersistAndResolvePerModel(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:       "mixed-model-types",
		Type:       outbound.OutboundTypeOpenAIChat,
		Enabled:    true,
		Model:      "gpt-4o,claude-sonnet",
		ModelTypes: model.ChannelModelTypes{"claude-sonnet": outbound.OutboundTypeAnthropic},
		Keys:       []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if got := channelRowInDB(t, ctx, channel.ID).ModelTypes["claude-sonnet"]; got != outbound.OutboundTypeAnthropic {
		t.Fatalf("expected model type to be persisted as Anthropic, got %d", got)
	}

	resolved, err := ChannelGetForModel(channel.ID, "claude-sonnet", ctx)
	if err != nil {
		t.Fatalf("ChannelGetForModel failed: %v", err)
	}
	if resolved.Type != outbound.OutboundTypeAnthropic {
		t.Fatalf("expected claude-sonnet to use its own type, got %d", resolved.Type)
	}
	fallback, err := ChannelGetForModel(channel.ID, "gpt-4o", ctx)
	if err != nil {
		t.Fatalf("ChannelGetForModel failed: %v", err)
	}
	if fallback.Type != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected gpt-4o to follow channel type, got %d", fallback.Type)
	}
	// 按模型解析只改返回的副本，缓存里的渠道类型必须保持不变。
	cached, err := ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if cached.Type != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("resolving a model type must not change the cached channel type, got %d", cached.Type)
	}

	next := model.ChannelModelTypes{"gpt-4o": outbound.OutboundTypeOpenAIResponse}
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, ModelTypes: &next}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}
	if updated.TypeForModel("gpt-4o") != outbound.OutboundTypeOpenAIResponse ||
		updated.TypeForModel("claude-sonnet") != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected model types to be replaced, got %#v", updated.ModelTypes)
	}

	var cleared model.ChannelModelTypes
	updated, err = ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, ModelTypes: &cleared}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}
	if len(updated.ModelTypes) != 0 {
		t.Fatalf("expected cached model types to be cleared, got %#v", updated.ModelTypes)
	}
	if row := channelRowInDB(t, ctx, channel.ID); len(row.ModelTypes) != 0 {
		t.Fatalf("expected persisted model types to be cleared, got %#v", row.ModelTypes)
	}
}
