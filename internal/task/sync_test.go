package task

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

func TestSyncModelsTaskReconcilesModels(t *testing.T) {
	for _, tc := range []struct {
		name, old, custom, body, filter string
		autoGroup                       model.AutoGroupType
		disabled                        bool
		status                          int
		wantModels, wantMappings        []string
	}{
		{name: "add-and-delete", old: "removed,kept", body: `{"data":[{"id":"kept"},{"id":"added"}]}`, autoGroup: model.AutoGroupTypeExact, wantModels: []string{"added", "kept"}, wantMappings: []string{"added", "kept"}},
		{name: "auto-group-off", old: "removed,kept", body: `{"data":[{"id":"kept"},{"id":"added"}]}`, wantModels: []string{"added", "kept"}, wantMappings: []string{"kept"}},
		{name: "repair-existing-stale-mapping", old: "kept", body: `{"data":[{"id":"kept"}]}`, wantModels: []string{"kept"}, wantMappings: []string{"kept"}},
		{name: "custom-model-survives-removal", old: "removed,kept", custom: "removed", body: `{"data":[{"id":"kept"}]}`, wantModels: []string{"kept"}, wantMappings: []string{"kept", "removed"}},
		{name: "empty-upstream", old: "removed,kept", body: `{"data":[]}`, autoGroup: model.AutoGroupTypeExact, wantModels: []string{}, wantMappings: []string{}},
		{name: "empty-keeps-custom", old: "removed,kept", custom: "removed", body: `{"data":[]}`, autoGroup: model.AutoGroupTypeExact, wantModels: []string{}, wantMappings: []string{"removed"}},
		{name: "filtered-empty", old: "removed,kept", filter: "^allowed-", body: `{"data":[{"id":"added"}]}`, autoGroup: model.AutoGroupTypeExact, wantModels: []string{}, wantMappings: []string{}},
		{name: "fetch-failure", old: "removed,kept", body: `{"error":"unavailable"}`, status: 503, wantModels: []string{"kept", "removed"}, wantMappings: []string{"kept", "removed"}},
		{name: "invalid-list", old: "removed,kept", body: `{}`, wantModels: []string{"kept", "removed"}, wantMappings: []string{"kept", "removed"}},
		{name: "auto-sync-off", old: "removed,kept", body: `{"data":[{"id":"added"}]}`, disabled: true, wantModels: []string{"kept", "removed"}, wantMappings: []string{"kept", "removed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupOutlierTestDB(t)
			if err := op.InitCache(); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			channel := &model.Channel{
				Name: "sync-source", Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
				BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
				Model:    tc.old, CustomModel: tc.custom, AutoSync: !tc.disabled, AutoGroup: tc.autoGroup,
				Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
			}
			if tc.filter != "" {
				channel.MatchRegex = &tc.filter
			}
			if err := op.ChannelCreate(channel, ctx); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"removed", "kept", "added"} {
				group := &model.Group{Name: name, Mode: model.GroupModeRoundRobin}
				if name != "added" {
					group.Items = []model.GroupItem{{ChannelID: channel.ID, ModelName: name, Priority: 1, Weight: 1}}
				}
				if err := op.GroupCreate(group, ctx); err != nil {
					t.Fatal(err)
				}
			}
			// 同一快照再同步一次，确保本轮正确且下一轮不会复活旧条目。
			for round := 0; round < 2; round++ {
				SyncModelsTask()
				updated, err := op.ChannelGet(channel.ID, ctx)
				if err != nil {
					t.Fatal(err)
				}
				models := xstrings.SplitTrimCompact(",", updated.Model)
				slices.Sort(models)
				if !reflect.DeepEqual(models, tc.wantModels) || updated.CustomModel != tc.custom {
					t.Errorf("round %d: models=%v custom=%q, want %v and %q", round, models, updated.CustomModel, tc.wantModels, tc.custom)
				}
				groups, err := op.GroupList(ctx)
				if err != nil {
					t.Fatal(err)
				}
				mapped := []string{}
				for _, group := range groups {
					items, err := op.GroupItemList(group.ID, ctx)
					if err != nil {
						t.Fatal(err)
					}
					if len(items) != len(group.Items) {
						t.Fatalf("cache/DB item count differs for %s", group.Name)
					}
					for _, item := range items {
						mapped = append(mapped, item.ModelName)
					}
				}
				slices.Sort(mapped)
				if !reflect.DeepEqual(mapped, tc.wantMappings) {
					t.Errorf("round %d: mappings=%v, want %v", round, mapped, tc.wantMappings)
				}
			}
		})
	}
}

func TestSyncModelsTaskAppendsNewChannelAtGroupTail(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"kept"},{"id":"added"}]}`))
	}))
	defer server.Close()
	group := &model.Group{Name: "added", Mode: model.GroupModeRoundRobin}
	var newChannelID int
	for i, name := range []string{"first", "second", "new"} {
		channel := &model.Channel{
			Name: name, Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
			Model: "added", AutoSync: i == 2, AutoGroup: model.AutoGroupTypeExact,
			BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
			Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		}
		if i == 2 {
			channel.Model = "kept"
		}
		if err := op.ChannelCreate(channel, ctx); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			group.Items = append(group.Items, model.GroupItem{ChannelID: channel.ID, ModelName: "added", Priority: 3 + i*4, Weight: 2 + i})
		} else {
			newChannelID = channel.ID
		}
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatal(err)
	}
	original := append([]model.GroupItem(nil), group.Items...)
	for round := 0; round < 2; round++ {
		SyncModelsTask()
		updated, err := op.GroupGet(group.ID, ctx)
		if err != nil {
			t.Fatal(err)
		}
		items := append([]model.GroupItem(nil), updated.Items...)
		sort.Slice(items, func(i, j int) bool { return items[i].Priority < items[j].Priority })
		if len(items) != 3 || !reflect.DeepEqual(items[:2], original) || items[2].ChannelID != newChannelID || items[2].Priority != 8 {
			t.Fatalf("round %d: group items=%+v, want original=%+v followed by new channel %d at priority 8", round, items, original, newChannelID)
		}
	}
}
