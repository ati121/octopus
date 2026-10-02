package model

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelTypeForModelPrefersModelType(t *testing.T) {
	channel := &Channel{
		Type:       outbound.OutboundTypeOpenAIChat,
		ModelTypes: ChannelModelTypes{"claude-sonnet": outbound.OutboundTypeAnthropic},
	}

	if got := channel.TypeForModel("claude-sonnet"); got != outbound.OutboundTypeAnthropic {
		t.Fatalf("expected model type Anthropic to override channel type, got %d", got)
	}
	if got := channel.TypeForModel("gpt-4o"); got != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected model without its own type to follow channel type, got %d", got)
	}
}

func TestChannelModelTypesNormalize(t *testing.T) {
	normalized, err := ChannelModelTypes{
		" claude-sonnet ": outbound.OutboundTypeAnthropic,
		"   ":             outbound.OutboundTypeGemini,
	}.Normalize()
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if len(normalized) != 1 || normalized["claude-sonnet"] != outbound.OutboundTypeAnthropic {
		t.Fatalf("expected trimmed model name and dropped blank entry, got %#v", normalized)
	}

	if empty, err := (ChannelModelTypes{"  ": outbound.OutboundTypeGemini}).Normalize(); err != nil || empty != nil {
		t.Fatalf("expected nil for effectively empty model types, got %#v err=%v", empty, err)
	}

	if _, err := (ChannelModelTypes{"claude-sonnet": outbound.OutboundType(99)}).Normalize(); err == nil {
		t.Fatalf("expected unknown channel type to be rejected")
	}
}

func TestGetChannelKeyPrefersPreferredKeyID(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "first"},
			{ID: 2, Enabled: true, ChannelKey: "preferred"},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesPreferredKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "fallback"},
			{ID: 2, Enabled: true, ChannelKey: "preferred", StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2 despite recent 429, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesLeastRecentlyUsedKey(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "recent-429", StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
			{ID: 2, Enabled: true, ChannelKey: "other"},
		},
	}

	selected := channel.GetChannelKey()
	if selected.ID != 2 {
		t.Fatalf("expected least recently used key 2, got %d", selected.ID)
	}
}

func TestGetChannelKeyRoundRobinRotatesKeys(t *testing.T) {
	channel := &Channel{
		ID:         42,
		RoundRobin: true,
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "a"},
			{ID: 2, Enabled: true, ChannelKey: "b"},
			{ID: 3, Enabled: true, ChannelKey: "c"},
		},
	}
	roundRobinKeyIndexes.Delete(42)

	got := []int{}
	for i := 0; i < 6; i++ {
		got = append(got, channel.GetChannelKey().ID)
	}
	// 连续 6 次应严格按 1,2,3,1,2,3 轮转
	expected := []int{1, 2, 3, 1, 2, 3}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected %v at position %d, got %v", expected[i], i, got)
		}
	}
	roundRobinKeyIndexes.Delete(42)
}

func TestGetChannelKeyRoundRobinSkipsDisabledAndExcluded(t *testing.T) {
	channel := &Channel{
		ID:         43,
		RoundRobin: true,
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "a"},
			{ID: 2, Enabled: false, ChannelKey: "disabled"},
			{ID: 3, Enabled: true, ChannelKey: "b"},
			{ID: 4, Enabled: true, ChannelKey: ""},
		},
	}
	roundRobinKeyIndexes.Store(43, new(uint64))

	got := []int{}
	for i := 0; i < 4; i++ {
		got = append(got, channel.GetChannelKey().ID)
	}
	expected := []int{1, 3, 1, 3}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected %v at position %d, got %v", expected[i], i, got)
		}
	}
	roundRobinKeyIndexes.Delete(43)
}

func TestGetChannelKeyRoundRobinRespectsPreferredKeyID(t *testing.T) {
	channel := &Channel{
		ID:         44,
		RoundRobin: true,
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "a"},
			{ID: 2, Enabled: true, ChannelKey: "preferred"},
			{ID: 3, Enabled: true, ChannelKey: "c"},
		},
	}
	roundRobinKeyIndexes.Delete(44)

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2, got %d", selected.ID)
	}
	roundRobinKeyIndexes.Delete(44)
}
