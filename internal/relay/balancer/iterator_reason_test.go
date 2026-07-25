package balancer

import (
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestIteratorRecordsRouteReasonOnAttempt(t *testing.T) {
	group := model.Group{
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "gpt-4o", Priority: 10},
			{ChannelID: 2, ModelName: "gpt-4o", Priority: 1},
		},
	}
	iterator := NewIterator(group, 7, "gpt-4o")
	if !iterator.Next() {
		t.Fatal("expected a candidate")
	}
	span := iterator.StartAttempt(1, 11, "primary")
	span.End(model.AttemptFailed, 500, "upstream 500")
	attempt := iterator.Attempts()[0]
	if !strings.Contains(attempt.Reason, "mode=failover") || !strings.Contains(attempt.Reason, "order=1/2") || !strings.Contains(attempt.Reason, "priority=") {
		t.Fatalf("unexpected route reason: %q", attempt.Reason)
	}
	if attempt.Msg != "upstream 500" {
		t.Fatalf("unexpected outcome message: %q", attempt.Msg)
	}
}

func TestIteratorPreferredStickyReason(t *testing.T) {
	group := model.Group{
		Mode: model.GroupModeRoundRobin,
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "m"},
			{ChannelID: 2, ModelName: "m"},
		},
	}
	iterator := NewIteratorWithPreference(group, 1, "m", &SessionEntry{ChannelID: 2, ChannelKeyID: 99})
	if !iterator.Next() || !iterator.IsSticky() {
		t.Fatal("expected preferred candidate first")
	}
	iterator.Skip(2, 99, "sticky", "disabled")
	reason := iterator.Attempts()[0].Reason
	if !strings.Contains(reason, "sticky=replay_or_preference") || !strings.Contains(reason, "sticky_key=99") {
		t.Fatalf("unexpected sticky reason: %q", reason)
	}
}

func TestIteratorWeightedReason(t *testing.T) {
	group := model.Group{Mode: model.GroupModeWeighted, Items: []model.GroupItem{{ChannelID: 1, ModelName: "m", Weight: 3}}}
	iterator := NewIterator(group, 1, "m")
	if !iterator.Next() {
		t.Fatal("expected a candidate")
	}
	span := iterator.StartAttempt(1, 1, "channel")
	span.End(model.AttemptSuccess, 200, "")
	reason := iterator.Attempts()[0].Reason
	if !strings.Contains(reason, "mode=weighted") || !strings.Contains(reason, "weight=3") {
		t.Fatalf("unexpected weighted reason: %q", reason)
	}
}
