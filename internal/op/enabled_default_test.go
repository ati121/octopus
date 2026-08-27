package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// 本文件的测试全部围绕同一个根因：GORM 对带 `default:true` 的 Go bool 字段做零值顶替，
// INSERT 时把 false 当成“调用方没赋值”，改用 tag 里的 true 落库。于是“建的时候就停用”
// 的渠道、渠道 Key、站点密钥、API Key 会在用户眼里自己变回启用状态。

func channelRowInDB(t *testing.T, ctx context.Context, id int) model.Channel {
	t.Helper()
	var row model.Channel
	if err := dbpkg.GetDB().WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("query channel %d failed: %v", id, err)
	}
	return row
}

// 这条测试盯的是 GORM 自己的行为，它是本文件其余测试有意义的前提，也是那些“Create 之后
// 再补写一列”的代码存在的唯一理由。如果升级 GORM 之后它开始失败，说明零值顶替没有了，
// 那么 db.ResetFalseBoolColumn / db.CreatePreservingFalseBools 及其调用点可以整批删掉。
func TestGormSubstitutesFalseBoolWithTagDefault(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	raw := &model.Channel{Name: "raw-create-disabled", Enabled: false}
	if err := dbpkg.GetDB().WithContext(ctx).Create(raw).Error; err != nil {
		t.Fatalf("plain Create failed: %v", err)
	}
	if !channelRowInDB(t, ctx, raw.ID).Enabled {
		t.Fatalf("GORM 不再把 `default:true` 的 bool 零值顶替成 true，可以移除所有补写逻辑")
	}
	if !raw.Enabled {
		t.Fatalf("GORM 不再回写调用者结构体，还原内存值的代码可以移除了")
	}
}

func channelKeyRowInDB(t *testing.T, ctx context.Context, channelID int, secret string) model.ChannelKey {
	t.Helper()
	var row model.ChannelKey
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("channel_id = ? AND channel_key = ?", channelID, secret).
		First(&row).Error; err != nil {
		t.Fatalf("query channel key %q failed: %v", secret, err)
	}
	return row
}

func TestChannelCreatePersistsDisabledChannelAndKeys(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:    "disabled-on-create",
		Enabled: false,
		Keys: []model.ChannelKey{
			{ChannelKey: "sk-off", Enabled: false, Remark: "off"},
			{ChannelKey: "sk-on", Enabled: true},
		},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	if channelRowInDB(t, ctx, channel.ID).Enabled {
		t.Fatalf("渠道本该停用，数据库里却是启用")
	}
	if channelKeyRowInDB(t, ctx, channel.ID, "sk-off").Enabled {
		t.Fatalf("停用的 Key 落库后变成启用")
	}
	if !channelKeyRowInDB(t, ctx, channel.ID, "sk-on").Enabled {
		t.Fatalf("启用的 Key 不该被改成停用")
	}

	// 补写数据库还不够：GORM 会把调用者结构体里的 false 也改成 true，而 ChannelCreate
	// 正是拿这个结构体去种缓存的，不还原内存值就会出现“数据库对了、缓存是错的”。
	if channel.Enabled {
		t.Fatalf("ChannelCreate 返回的结构体仍是启用状态")
	}
	for _, k := range channel.Keys {
		if k.ChannelKey == "sk-off" && k.Enabled {
			t.Fatalf("ChannelCreate 返回的停用 Key 仍是启用状态")
		}
	}
	cached, err := ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if cached.Enabled {
		t.Fatalf("缓存里的渠道仍是启用状态")
	}
}

func TestChannelUpdateAddsDisabledKeyAsDisabled(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:    "add-disabled-key",
		Enabled: true,
		Keys:    []model.ChannelKey{{ChannelKey: "sk-seed", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: channel.ID,
		KeysToAdd: []model.ChannelKeyAddRequest{
			{ChannelKey: "sk-added-off", Enabled: false, Remark: "off"},
			{ChannelKey: "sk-added-on", Enabled: true},
		},
	}, ctx); err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}

	added := channelKeyRowInDB(t, ctx, channel.ID, "sk-added-off")
	if added.Enabled {
		t.Fatalf("新增时就停用的 Key 落库后变成启用")
	}
	if added.Remark != "off" {
		t.Fatalf("expected remark %q, got %q", "off", added.Remark)
	}
	if !channelKeyRowInDB(t, ctx, channel.ID, "sk-added-on").Enabled {
		t.Fatalf("启用的 Key 不该被改成停用")
	}
}

// 中继按值拷走 ChannelKey 快照，请求结束后才把它写回缓存并在退出时落库。过去落库用的是
// OnConflict{UpdateAll: true} 全字段覆盖，于是这份陈旧快照会把用户中途改过的 enabled /
// remark 一起冲掉——每次正常重启都会把用户关掉的 Key 开关翻回启用。现在只回写运行时真正
// 会变的三列。
func TestChannelKeySaveDBOnlyWritesRuntimeColumns(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:    "save-db-scoped-columns",
		Enabled: true,
		Keys:    []model.ChannelKey{{ChannelKey: "sk-user-off", Enabled: false, Remark: "user remark"}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	keyID := channel.Keys[0].ID

	// 模拟中继：它手上是请求开始时的快照，此后用户关掉了开关、改了备注，快照里还是旧值。
	stale := channel.Keys[0]
	stale.Enabled = true
	stale.Remark = "stale snapshot"
	stale.StatusCode = 200
	stale.LastUseTimeStamp = 1717000000
	stale.TotalCost = 9.5
	if err := ChannelKeyUpdate(stale); err != nil {
		t.Fatalf("ChannelKeyUpdate failed: %v", err)
	}
	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("ChannelKeySaveDB failed: %v", err)
	}

	row := channelKeyRowInDB(t, ctx, channel.ID, "sk-user-off")
	if row.ID != keyID {
		t.Fatalf("expected key id %d, got %d", keyID, row.ID)
	}
	if row.Enabled {
		t.Fatalf("陈旧快照把用户关掉的开关又打开了")
	}
	if row.Remark != "user remark" {
		t.Fatalf("陈旧快照覆盖了用户改的备注，got %q", row.Remark)
	}
	if row.StatusCode != 200 || row.LastUseTimeStamp != 1717000000 || row.TotalCost != 9.5 {
		t.Fatalf("运行时统计三列没有照常写入: %+v", row)
	}
}

// 备份导入是逐条 Create 的，7 张表都受同一个零值顶替影响。修好之前，导入一份备份会把
// 里面所有停用的代理、渠道、渠道 Key、站点、站点账号、站点密钥和 API Key 全部改成启用。
func TestDBImportPreservesDisabledFlags(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := &model.DBDump{
		Version: 1,
		ProxyConfigurations: []model.ProxyConfiguration{
			{ID: 1, Name: "proxy-off", URL: "http://127.0.0.1:1080", Enabled: false},
		},
		Channels: []model.Channel{
			{ID: 1, Name: "channel-off", Enabled: false},
			{ID: 2, Name: "channel-on", Enabled: true},
		},
		ChannelKeys: []model.ChannelKey{
			{ID: 1, ChannelID: 1, ChannelKey: "sk-import-off", Enabled: false},
			{ID: 2, ChannelID: 2, ChannelKey: "sk-import-on", Enabled: true},
		},
		Sites: []model.Site{
			{ID: 1, Name: "site-off", Platform: model.SitePlatformNewAPI, BaseURL: "https://off.example.com", Enabled: false},
		},
		SiteAccounts: []model.SiteAccount{
			{
				ID:             1,
				SiteID:         1,
				Name:           "account-off",
				CredentialType: model.SiteCredentialTypeAPIKey,
				APIKey:         "sk-account-off",
				Enabled:        false,
				AutoSync:       false,
				AutoCheckin:    false,
			},
		},
		SiteTokens: []model.SiteToken{
			{
				ID:            1,
				SiteAccountID: 1,
				Name:          "token-off",
				Token:         "sk-token-off",
				GroupKey:      model.SiteDefaultGroupKey,
				Enabled:       false,
			},
		},
		APIKeys: []model.APIKey{
			{ID: 1, Name: "apikey-off", APIKey: "sk-api-off", Enabled: false},
			{ID: 2, Name: "apikey-on", APIKey: "sk-api-on", Enabled: true},
		},
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	conn := dbpkg.GetDB().WithContext(ctx)

	var proxyRow model.ProxyConfiguration
	if err := conn.Where("name = ?", "proxy-off").First(&proxyRow).Error; err != nil {
		t.Fatalf("query imported proxy failed: %v", err)
	}
	if proxyRow.Enabled {
		t.Fatalf("导入后停用的代理变成了启用")
	}

	var channelOff, channelOn model.Channel
	if err := conn.Where("name = ?", "channel-off").First(&channelOff).Error; err != nil {
		t.Fatalf("query imported disabled channel failed: %v", err)
	}
	if channelOff.Enabled {
		t.Fatalf("导入后停用的渠道变成了启用")
	}
	if err := conn.Where("name = ?", "channel-on").First(&channelOn).Error; err != nil {
		t.Fatalf("query imported enabled channel failed: %v", err)
	}
	if !channelOn.Enabled {
		t.Fatalf("导入后启用的渠道被误改成停用")
	}

	if channelKeyRowInDB(t, ctx, channelOff.ID, "sk-import-off").Enabled {
		t.Fatalf("导入后停用的渠道 Key 变成了启用")
	}
	if !channelKeyRowInDB(t, ctx, channelOn.ID, "sk-import-on").Enabled {
		t.Fatalf("导入后启用的渠道 Key 被误改成停用")
	}

	var siteRow model.Site
	if err := conn.Where("base_url = ?", "https://off.example.com").First(&siteRow).Error; err != nil {
		t.Fatalf("query imported site failed: %v", err)
	}
	if siteRow.Enabled {
		t.Fatalf("导入后停用的站点变成了启用")
	}

	var accountRow model.SiteAccount
	if err := conn.Where("site_id = ? AND name = ?", siteRow.ID, "account-off").First(&accountRow).Error; err != nil {
		t.Fatalf("query imported site account failed: %v", err)
	}
	if accountRow.Enabled || accountRow.AutoSync || accountRow.AutoCheckin {
		t.Fatalf("导入后站点账号的三个开关被打开: %+v", accountRow)
	}

	var tokenRow model.SiteToken
	if err := conn.Where("site_account_id = ? AND token = ?", accountRow.ID, "sk-token-off").First(&tokenRow).Error; err != nil {
		t.Fatalf("query imported site token failed: %v", err)
	}
	if tokenRow.Enabled {
		t.Fatalf("导入后停用的站点密钥变成了启用")
	}

	var apiKeyOff, apiKeyOn model.APIKey
	if err := conn.Where("api_key = ?", "sk-api-off").First(&apiKeyOff).Error; err != nil {
		t.Fatalf("query imported disabled api key failed: %v", err)
	}
	if apiKeyOff.Enabled {
		t.Fatalf("导入后停用的 API Key 变成了启用")
	}
	if err := conn.Where("api_key = ?", "sk-api-on").First(&apiKeyOn).Error; err != nil {
		t.Fatalf("query imported enabled api key failed: %v", err)
	}
	if !apiKeyOn.Enabled {
		t.Fatalf("导入后启用的 API Key 被误改成停用")
	}
}
