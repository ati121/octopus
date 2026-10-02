package op

import (
	"github.com/bestruirui/octopus/internal/model"
	"testing"
)

func TestChannelHistorySurvivesDeletionRestartAndClear(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := &model.Channel{Name: "已删除渠道", Enabled: true}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	metrics := model.StatsMetrics{InputToken: 123456, OutputToken: 789, RequestSuccess: 2}
	if err := StatsChannelUpdate(channel.ID, metrics); err != nil {
		t.Fatal(err)
	}
	if err := StatsModelNameUpdate("historical-model", channel.ID, metrics); err != nil {
		t.Fatal(err)
	}
	// 未到定时保存周期即删除，也必须保留缓存与之后的持久化。
	if err := ChannelDel(channel.ID, ctx); err != nil {
		t.Fatal(err)
	}
	if err := StatsSaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	if err := InitCache(); err != nil {
		t.Fatal(err)
	}
	rows := StatsChannelList()
	if len(rows) != 1 || rows[0].Name != channel.Name || rows[0].StatsMetrics != metrics || rows[0].HistoryID == "" {
		t.Fatalf("history lost: %+v", rows)
	}
	if rows := StatsModelList(); len(rows) != 1 || rows[0].StatsMetrics != metrics {
		t.Fatalf("model history lost: %+v", rows)
	}
	newChannel := &model.Channel{Name: channel.Name, Enabled: true}
	if err := ChannelCreate(newChannel, ctx); err != nil {
		t.Fatal(err)
	}
	if newChannel.ID == channel.ID || StatsChannelGet(newChannel.ID).RequestSuccess != 0 {
		t.Fatal("new channel inherited deleted channel history")
	}
	if err := StatsClear(ctx); err != nil {
		t.Fatal(err)
	}
	_ = StatsChannelGet(newChannel.ID)
	if err := StatsSaveDB(ctx); err != nil {
		t.Fatal(err)
	}
	if err := InitCache(); err != nil {
		t.Fatal(err)
	}
	if len(StatsChannelList()) != 0 || len(StatsModelList()) != 0 {
		t.Fatal("cleared history came back")
	}
}

func TestChannelHistoryKeepsInflightRequestName(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	channel := &model.Channel{Name: "请求结束前删除", Enabled: true}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	if err := ChannelDel(channel.ID, ctx); err != nil {
		t.Fatal(err)
	}
	if err := StatsChannelUpdate(channel.ID, model.StatsMetrics{RequestSuccess: 1}, channel.Name); err != nil {
		t.Fatal(err)
	}
	if rows := StatsChannelList(); len(rows) != 1 || rows[0].Name != channel.Name {
		t.Fatalf("missing inflight name: %+v", rows)
	}
}
