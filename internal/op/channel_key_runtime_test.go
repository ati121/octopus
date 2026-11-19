package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestChannelKeyRuntimeUpdatePreservesAdminChanges(t *testing.T) {
	for _, action := range []string{"disable", "delete", "rotate"} {
		t.Run(action, func(t *testing.T) {
			ctx := setupSiteOpTestDB(t)
			if err := channelRefreshCache(ctx); err != nil {
				t.Fatal(err)
			}
			channel := &model.Channel{
				Name: "runtime-key", Enabled: true,
				Keys: []model.ChannelKey{{ChannelKey: "test-old-key", Enabled: true}},
			}
			if err := ChannelCreate(channel, ctx); err != nil {
				t.Fatal(err)
			}
			stale := channel.Keys[0]
			off, remark, replacement := false, "updated remark", "test-new-key"
			req := &model.ChannelUpdateRequest{ID: channel.ID}
			switch action {
			case "disable":
				req.KeysToUpdate = []model.ChannelKeyUpdateRequest{{ID: stale.ID, Enabled: &off, Remark: &remark}}
			case "delete":
				req.KeysToDelete = []int{stale.ID}
			case "rotate":
				req.KeysToUpdate = []model.ChannelKeyUpdateRequest{{ID: stale.ID, ChannelKey: &replacement}}
			}
			if _, err := ChannelUpdate(req, ctx); err != nil {
				t.Fatal(err)
			}
			stale.StatusCode, stale.LastUseTimeStamp = 401, 100
			if err := ChannelKeyUpdate(stale); err != nil {
				t.Fatal(err)
			}
			if err := ChannelKeySaveDB(ctx); err != nil {
				t.Fatal(err)
			}
			// 同时检查即时路由缓存和重载后的持久化状态。
			for _, reload := range []bool{false, true} {
				if reload {
					if err := channelRefreshCache(ctx); err != nil {
						t.Fatal(err)
					}
				}
				cached, err := ChannelGet(channel.ID, ctx)
				if err != nil {
					t.Fatal(err)
				}
				if action == "delete" {
					if len(cached.Keys) != 0 {
						t.Fatal("已删除的 Key 被在途请求重新加入")
					}
					continue
				}
				key := cached.Keys[0]
				if action == "disable" {
					if key.Enabled || key.Remark != remark || cached.GetChannelKey().ID != 0 {
						t.Fatal("在途请求覆盖了停用状态或备注")
					}
					if key.StatusCode != 401 || key.LastUseTimeStamp != 100 {
						t.Fatal("相同凭据的运行时状态未正常保存")
					}
				} else if key.ChannelKey != replacement || key.StatusCode != 0 || key.LastUseTimeStamp != 0 {
					t.Fatal("旧凭据的运行状态污染了新凭据")
				}
			}
		})
	}
}

func TestChannelKeySaveDBDoesNotInsertDeletedSnapshot(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := channelRefreshCache(ctx); err != nil {
		t.Fatal(err)
	}
	channel := &model.Channel{Name: "deleted-snapshot", Enabled: true, Keys: []model.ChannelKey{{ChannelKey: "test-key", Enabled: true}}}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	key := channel.Keys[0]
	key.StatusCode, key.LastUseTimeStamp = 200, 100
	if err := ChannelKeyUpdate(key); err != nil {
		t.Fatal(err)
	}
	// 模拟缓存已取到待保存快照，但删除事务先提交，缓存失效尚未完成。
	if err := dbpkg.GetDB().Delete(&model.ChannelKey{}, key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := dbpkg.GetDB().Model(&model.ChannelKey{}).Where("id = ?", key.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("保存缓存重新插入了已删除的 Key")
	}
}

func TestChannelKeyRuntimeUpdateIgnoresOlderCompletion(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := channelRefreshCache(ctx); err != nil {
		t.Fatal(err)
	}
	channel := &model.Channel{Name: "out-of-order", Enabled: true, Keys: []model.ChannelKey{{ChannelKey: "test-key", Enabled: true}}}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	key := channel.Keys[0]
	key.StatusCode, key.LastUseTimeStamp = 200, 200
	if err := ChannelKeyUpdate(key); err != nil {
		t.Fatal(err)
	}
	key.StatusCode, key.LastUseTimeStamp = 401, 100
	if err := ChannelKeyUpdate(key); err != nil {
		t.Fatal(err)
	}
	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	row := channelKeyRowInDB(t, ctx, channel.ID, key.ChannelKey)
	if row.StatusCode != 200 || row.LastUseTimeStamp != 200 {
		t.Fatal("较早结束的请求覆盖了较新的运行状态")
	}
}
