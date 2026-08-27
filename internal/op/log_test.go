package op

import (
	"errors"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func resetRelayLogStateForTest() {
	relayLogRecentLock.Lock()
	relayLogRecent = make([]model.RelayLog, 0, relayLogRecentMaxSize)
	relayLogRecentLock.Unlock()

	relayLogInFlightLock.Lock()
	relayLogInFlight = make(map[int64]model.RelayLog)
	relayLogInFlightLock.Unlock()

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]time.Time)
	relayLogStreamTokensLock.Unlock()
}

func TestFillRelayLogProtocolsCoversOrdinaryChannelsAndAttempts(t *testing.T) {
	const chatChannelID = 991101
	const responseChannelID = 991102
	channelCache.Set(chatChannelID, model.Channel{ID: chatChannelID, Name: "chat-channel", Type: outbound.OutboundTypeOpenAIChat})
	channelCache.Set(responseChannelID, model.Channel{ID: responseChannelID, Name: "response-channel", Type: outbound.OutboundTypeOpenAIResponse})
	t.Cleanup(func() {
		channelCache.Del(chatChannelID)
		channelCache.Del(responseChannelID)
	})

	relayLog := model.RelayLog{
		ChannelId: responseChannelID,
		Attempts: []model.ChannelAttempt{
			{ChannelID: chatChannelID, ChannelName: "chat-channel", Status: model.AttemptFailed},
			{ChannelID: responseChannelID, ChannelName: "response-channel", Status: model.AttemptSuccess},
		},
	}
	fillRelayLogProtocols(&relayLog)

	if relayLog.Protocol != "Response" {
		t.Fatalf("expected final protocol Response, got %q", relayLog.Protocol)
	}
	if relayLog.Attempts[0].Protocol != "Chat" || relayLog.Attempts[1].Protocol != "Response" {
		t.Fatalf("unexpected attempt protocols: %#v", relayLog.Attempts)
	}
}

func TestRelayLogStreamTokenExpires(t *testing.T) {
	resetRelayLogStateForTest()
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens["expired"] = time.Now().Add(-time.Second)
	relayLogStreamTokensLock.Unlock()
	if RelayLogStreamTokenVerify("expired") {
		t.Fatal("expired stream token should be rejected")
	}
}

func TestRelayLogStreamTokenStoreIsBounded(t *testing.T) {
	resetRelayLogStateForTest()
	for range relayLogStreamTokenMaxEntries + 10 {
		if _, err := RelayLogStreamTokenCreate(); err != nil {
			t.Fatalf("create stream token: %v", err)
		}
	}
	relayLogStreamTokensLock.RLock()
	count := len(relayLogStreamTokens)
	relayLogStreamTokensLock.RUnlock()
	if count > relayLogStreamTokenMaxEntries {
		t.Fatalf("stream token store exceeded limit: %d", count)
	}
}

func TestRelayLogStartAndUpdateReuseID(t *testing.T) {
	resetRelayLogStateForTest()
	events := RelayLogSubscribe()
	defer RelayLogUnsubscribe(events)

	id := RelayLogStart(model.RelayLog{Time: 100, RequestModelName: "gpt-test"})
	startedEvent := <-events
	if startedEvent.ID != id || !startedEvent.Processing {
		t.Fatalf("expected processing stream event, got %+v", startedEvent)
	}
	listed, err := RelayLogListWithFilter(nil, RelayLogListFilter{Page: 1, PageSize: 10, IncludeContent: true, WithTotal: true})
	if err != nil || len(listed.Logs) != 1 || listed.Logs[0].ID != id || !listed.Logs[0].Processing {
		t.Fatalf("expected active relay log in list, result=%+v err=%v", listed, err)
	}

	if err := RelayLogUpdate(nil, model.RelayLog{ID: id, Time: 100, RequestModelName: "gpt-test", Success: true, UseTime: 123}); err != nil {
		t.Fatalf("RelayLogUpdate failed: %v", err)
	}
	completedEvent := <-events
	if completedEvent.ID != id || completedEvent.Processing || !completedEvent.Success {
		t.Fatalf("expected completed stream event for same ID, got %+v", completedEvent)
	}
	if RelayLogInFlightLen() != 0 {
		t.Fatal("completed relay log remained in flight")
	}
}

func TestRelayLogListFiltersInMemoryAndKeepsContentOnDetail(t *testing.T) {
	resetRelayLogStateForTest()
	rows := []model.RelayLog{
		{ID: 101, Time: 101, RequestModelName: "gpt-visible", RequestAPIKeyName: "key-a", ChannelId: 1, ChannelName: "primary", ActualModelName: "gpt-visible", RequestContent: "hidden-needle", ResponseContent: "hidden-response", Success: true},
		{ID: 102, Time: 102, RequestModelName: "claude", RequestAPIKeyName: "key-b", ChannelId: 1, ChannelName: "secondary", ActualModelName: "claude", Error: "visible failure", RequestContent: "plain", Success: false},
	}
	for _, row := range rows {
		if err := RelayLogAdd(nil, row); err != nil {
			t.Fatalf("RelayLogAdd failed: %v", err)
		}
	}

	result, err := RelayLogListWithFilter(nil, RelayLogListFilter{Page: 1, PageSize: 10, WithTotal: true})
	if err != nil || result.Total != 2 || len(result.Logs) != 2 {
		t.Fatalf("unexpected list result: %+v err=%v", result, err)
	}
	for _, item := range result.Logs {
		if item.RequestContent != "" || item.ResponseContent != "" {
			t.Fatalf("expected list to omit content fields by default, got %+v", item)
		}
	}

	contentResult, err := RelayLogListWithFilter(nil, RelayLogListFilter{Keyword: "hidden-needle", KeywordScope: RelayLogKeywordScopeContent, StartTime: intPtr(0), EndTime: intPtr(200), Page: 1, PageSize: 10, WithTotal: true, IncludeContent: true})
	if err != nil || contentResult.Total != 1 || len(contentResult.Logs) != 1 || contentResult.Logs[0].ID != 101 {
		t.Fatalf("content keyword did not find expected row: %+v err=%v", contentResult, err)
	}
	got, err := RelayLogGet(nil, 101)
	if err != nil || got.RequestContent != "hidden-needle" || got.ResponseContent != "hidden-response" {
		t.Fatalf("detail did not retain full content: %+v err=%v", got, err)
	}
}

func intPtr(v int) *int { return &v }

func TestRelayLogListContainsKeywordRequiresMinLength(t *testing.T) {
	resetRelayLogStateForTest()
	_, err := RelayLogListWithFilter(nil, RelayLogListFilter{Keyword: "ab", KeywordMode: RelayLogKeywordModeContains, Page: 1, PageSize: 10})
	if !errors.Is(err, ErrRelayLogContainsKeywordTooShort) {
		t.Fatalf("expected ErrRelayLogContainsKeywordTooShort, got %v", err)
	}
}

func TestRelayLogListCursorReturnsNextCursorWithoutTotal(t *testing.T) {
	resetRelayLogStateForTest()
	for _, row := range []model.RelayLog{
		{ID: 201, Time: 201, RequestModelName: "a", Success: true},
		{ID: 202, Time: 202, RequestModelName: "b", Success: true},
		{ID: 203, Time: 203, RequestModelName: "c", Success: true},
	} {
		_ = RelayLogAdd(nil, row)
	}
	first, err := RelayLogListWithFilter(nil, RelayLogListFilter{Limit: 2})
	if err != nil || first.Total != 0 || !first.HasMore || first.NextCursor == nil || len(first.Logs) != 2 || first.Logs[0].ID != 203 || first.Logs[1].ID != 202 {
		t.Fatalf("unexpected first cursor page: %+v err=%v", first, err)
	}
	second, err := RelayLogListWithFilter(nil, RelayLogListFilter{Limit: 2, BeforeTime: &first.NextCursor.Time, BeforeID: &first.NextCursor.ID})
	if err != nil || second.HasMore || second.NextCursor != nil || len(second.Logs) != 1 || second.Logs[0].ID != 201 {
		t.Fatalf("unexpected second cursor page: %+v err=%v", second, err)
	}
}

func TestRelayLogClearKeepsActiveAndCapsCompleted(t *testing.T) {
	resetRelayLogStateForTest()
	activeID := RelayLogStart(model.RelayLog{Time: 1, RequestModelName: "active"})
	for i := 0; i < relayLogRecentMaxSize+10; i++ {
		_ = RelayLogAdd(nil, model.RelayLog{ID: int64(1000 + i), Time: int64(1000 + i), Success: true})
	}
	// 直接查内存：列表接口单页最多 100 条，撑不到 relayLogRecentMaxSize。
	relayLogRecentLock.RLock()
	completed := 0
	for _, entry := range relayLogRecent {
		if !entry.Processing {
			completed++
		}
	}
	total := len(relayLogRecent)
	relayLogRecentLock.RUnlock()
	if completed != relayLogRecentMaxSize || total != relayLogRecentMaxSize+1 {
		t.Fatalf("expected %d completed plus 1 active, got completed=%d total=%d", relayLogRecentMaxSize, completed, total)
	}
	if _, err := RelayLogGet(nil, activeID); err != nil {
		t.Fatalf("active log was evicted: %v", err)
	}
	if err := RelayLogClear(nil); err != nil {
		t.Fatalf("RelayLogClear failed: %v", err)
	}
	cleared, err := RelayLogListWithFilter(nil, RelayLogListFilter{Limit: 100})
	if err != nil || len(cleared.Logs) != 1 || cleared.Logs[0].ID != activeID || !cleared.Logs[0].Processing {
		t.Fatalf("clear should preserve active request, got %+v err=%v", cleared, err)
	}
}
