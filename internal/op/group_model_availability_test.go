package op

import (
	"reflect"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestGroupAvailabilityFollowsChannelModels(t *testing.T) {
	setupAutoGroupTestDB(t)
	ctx := t.Context()
	channel := autoGroupTestChannel("removed-model-source", "step-5-preview,other", model.AutoGroupTypeNone)
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	group := &model.Group{
		Name: "public-model-alias", Mode: model.GroupModeRoundRobin,
		Items: []model.GroupItem{{ChannelID: channel.ID, ModelName: "step-5-preview", Priority: 1, Weight: 1}},
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatal(err)
	}
	originalItems := append([]model.GroupItem(nil), group.Items...)

	for _, tc := range []struct {
		name, models, custom string
		wantAvailable        bool
	}{
		{"removed", "other", "", false},
		{"prefix-is-not-match", "step-5-preview-plus", "", false},
		{"case-is-not-match", "STEP-5-PREVIEW", "", false},
		{"custom-retains-model", "other", " , step-5-preview, ", true},
		{"custom-removed", "other", "", false},
		{"empty-channel", "", "", false},
		{"restored", "other, step-5-preview, ", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, Model: &tc.models, CustomModel: &tc.custom}, ctx); err != nil {
				t.Fatal(err)
			}
			listed, err := ChannelLLMList(ctx)
			if err != nil {
				t.Fatal(err)
			}
			visible := false
			for _, item := range listed {
				if item.ChannelID == channel.ID && item.Name == "step-5-preview" {
					visible = true
				}
			}
			if visible != tc.wantAvailable {
				t.Fatalf("model list visibility = %t, want %t", visible, tc.wantAvailable)
			}
			routed, err := GroupGetEnabledMap(group.Name, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if (len(routed.Items) > 0) != tc.wantAvailable {
				t.Errorf("routing availability = %t, want %t; items=%+v", len(routed.Items) > 0, tc.wantAvailable, routed.Items)
			}
			available, err := GroupListAvailable(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(available) != 1 || (len(available[0].Items) > 0) != tc.wantAvailable {
				t.Errorf("available groups = %+v, want availability %t", available, tc.wantAvailable)
			}
			publicModels, err := GroupListModel(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if (len(publicModels) > 0) != tc.wantAvailable {
				t.Errorf("public models = %v, want availability %t", publicModels, tc.wantAvailable)
			}
			cached, err := GroupGet(group.ID, ctx)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := GroupItemList(group.ID, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cached.Items, originalItems) || !reflect.DeepEqual(persisted, originalItems) {
				t.Fatalf("availability filtering changed stored configuration: cached=%+v persisted=%+v", cached.Items, persisted)
			}
		})
	}
}
