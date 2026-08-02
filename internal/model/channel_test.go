package model

import (
	"testing"
	"time"
)

func TestGetChannelKeyPrefersPreferredKeyID(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "first", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100},
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
			{ID: 1, Enabled: true, ChannelKey: "fallback", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2 despite recent 429, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesLowestCostKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "recent-429", TotalCost: 1, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
			{ID: 2, Enabled: true, ChannelKey: "other", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey()
	if selected.ID != 1 {
		t.Fatalf("expected lowest cost key 1 despite recent 429, got %d", selected.ID)
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
