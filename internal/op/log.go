package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

// Relay logs are intentionally process-local.  Request and response bodies
// can be very large, and persisting every relay to SQLite made the database
// grow without bound.  Keep at most 50 completed records while retaining all
// active requests until they finish.
const relayLogRecentMaxSize = 50

var (
	relayLogRecent     = make([]model.RelayLog, 0, relayLogRecentMaxSize)
	relayLogRecentLock sync.RWMutex

	relayLogInFlight     = make(map[int64]model.RelayLog)
	relayLogInFlightLock sync.RWMutex

	relayLogSubscribers     = make(map[chan model.RelayLog]struct{})
	relayLogSubscribersLock sync.RWMutex
)

const (
	relayLogStreamTokenTTL        = 5 * time.Minute
	relayLogStreamTokenMaxEntries = 1024
)

var relayLogStreamTokens = make(map[string]time.Time)
var relayLogStreamTokensLock sync.RWMutex

func RelayLogStreamTokenCreate() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	relayLogStreamTokensLock.Lock()
	now := time.Now()
	pruneRelayLogStreamTokensLocked(now)
	for len(relayLogStreamTokens) >= relayLogStreamTokenMaxEntries {
		oldestToken := ""
		var oldestExpiry time.Time
		for candidate, expiresAt := range relayLogStreamTokens {
			if oldestToken == "" || expiresAt.Before(oldestExpiry) {
				oldestToken = candidate
				oldestExpiry = expiresAt
			}
		}
		if oldestToken == "" {
			break
		}
		delete(relayLogStreamTokens, oldestToken)
	}
	relayLogStreamTokens[token] = now.Add(relayLogStreamTokenTTL)
	relayLogStreamTokensLock.Unlock()

	return token, nil
}

func RelayLogStreamTokenVerify(token string) bool {
	relayLogStreamTokensLock.Lock()
	expiresAt, ok := relayLogStreamTokens[token]
	if ok && !time.Now().Before(expiresAt) {
		delete(relayLogStreamTokens, token)
		ok = false
	}
	relayLogStreamTokensLock.Unlock()
	return ok
}

func RelayLogStreamTokenRevoke(token string) {
	relayLogStreamTokensLock.Lock()
	delete(relayLogStreamTokens, token)
	relayLogStreamTokensLock.Unlock()
}

func pruneRelayLogStreamTokensLocked(now time.Time) {
	for token, expiresAt := range relayLogStreamTokens {
		if !now.Before(expiresAt) {
			delete(relayLogStreamTokens, token)
		}
	}
}

func RelayLogSubscribe() chan model.RelayLog {
	ch := make(chan model.RelayLog, 64)
	relayLogSubscribersLock.Lock()
	relayLogSubscribers[ch] = struct{}{}
	relayLogSubscribersLock.Unlock()
	return ch
}

func RelayLogUnsubscribe(ch chan model.RelayLog) {
	relayLogSubscribersLock.Lock()
	if _, ok := relayLogSubscribers[ch]; ok {
		delete(relayLogSubscribers, ch)
		close(ch)
	}
	relayLogSubscribersLock.Unlock()
}

func notifySubscribers(relayLog model.RelayLog) {
	relayLogSubscribersLock.RLock()
	defer relayLogSubscribersLock.RUnlock()
	for ch := range relayLogSubscribers {
		select {
		case ch <- relayLog:
		default:
			// Slow clients reconnect and reload a snapshot; never block a request.
		}
	}
}

// RelayLogWriterRun is retained for shutdown compatibility.  Relay logs no
// longer have a persistence writer, so it only waits for cancellation.
func RelayLogWriterRun(ctx context.Context) {
	if ctx != nil {
		<-ctx.Done()
	}
}

// RelayLogFlushPending is a compatibility no-op because the pending DB queue
// is no longer populated.
func RelayLogFlushPending(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func RelayLogPendingLen() int { return 0 }

func RelayLogInFlightLen() int {
	relayLogInFlightLock.RLock()
	defer relayLogInFlightLock.RUnlock()
	return len(relayLogInFlight)
}

func RelayLogDroppedTotal() uint64 { return 0 }

func appendRelayLogRecent(relayLog model.RelayLog) {
	relayLogRecentLock.Lock()
	defer relayLogRecentLock.Unlock()
	for i := len(relayLogRecent) - 1; i >= 0; i-- {
		if relayLogRecent[i].ID == relayLog.ID {
			relayLogRecent[i] = relayLog
			pruneFinishedRelayLogsLocked()
			return
		}
	}
	relayLogRecent = append(relayLogRecent, relayLog)
	pruneFinishedRelayLogsLocked()
}

// fillRelayLogProtocols backfills protocol metadata for logs created by older
// relay code (or by a direct caller). Keeping this at the log boundary makes
// the API consistent even when a request started before the new metadata was
// introduced, and it also covers non-HTTP relay paths.
func fillRelayLogProtocols(relayLog *model.RelayLog) {
	if relayLog == nil {
		return
	}
	protocolForChannel := func(channelID int) string {
		if channelID == 0 {
			return ""
		}
		channel, ok := channelCache.Get(channelID)
		if !ok {
			return ""
		}
		return model.CompactOutboundProtocolName(channel.Type)
	}
	if strings.TrimSpace(relayLog.Protocol) == "" {
		relayLog.Protocol = protocolForChannel(relayLog.ChannelId)
	}
	for i := range relayLog.Attempts {
		if strings.TrimSpace(relayLog.Attempts[i].Protocol) == "" {
			relayLog.Attempts[i].Protocol = protocolForChannel(relayLog.Attempts[i].ChannelID)
		}
	}
	if strings.TrimSpace(relayLog.Protocol) == "" {
		for i := len(relayLog.Attempts) - 1; i >= 0; i-- {
			if relayLog.Attempts[i].Status == model.AttemptSuccess && strings.TrimSpace(relayLog.Attempts[i].Protocol) != "" {
				relayLog.Protocol = relayLog.Attempts[i].Protocol
				break
			}
		}
	}
	if strings.TrimSpace(relayLog.Protocol) == "" {
		for i := len(relayLog.Attempts) - 1; i >= 0; i-- {
			if strings.TrimSpace(relayLog.Attempts[i].Protocol) != "" {
				relayLog.Protocol = relayLog.Attempts[i].Protocol
				break
			}
		}
	}
}

// pruneFinishedRelayLogsLocked removes the oldest completed records only.
// Active records do not count toward the 50-record limit.
func pruneFinishedRelayLogsLocked() {
	finished := 0
	for _, entry := range relayLogRecent {
		if !entry.Processing {
			finished++
		}
	}
	remove := finished - relayLogRecentMaxSize
	if remove <= 0 {
		return
	}
	kept := make([]model.RelayLog, 0, len(relayLogRecent)-remove)
	for _, entry := range relayLogRecent {
		if remove > 0 && !entry.Processing {
			remove--
			continue
		}
		kept = append(kept, entry)
	}
	relayLogRecent = kept
}

func RelayLogAdd(ctx context.Context, relayLog model.RelayLog) error {
	if relayLog.ID == 0 {
		relayLog.ID = snowflake.GenerateID()
	}
	fillRelayLogProtocols(&relayLog)
	relayLog.Processing = false
	appendRelayLogRecent(relayLog)
	notifySubscribers(relayLog)
	return nil
}

func RelayLogStart(relayLog model.RelayLog) int64 {
	if relayLog.ID == 0 {
		relayLog.ID = snowflake.GenerateID()
	}
	fillRelayLogProtocols(&relayLog)
	relayLog.Processing = true

	relayLogInFlightLock.Lock()
	relayLogInFlight[relayLog.ID] = relayLog
	relayLogInFlightLock.Unlock()
	appendRelayLogRecent(relayLog)
	notifySubscribers(relayLog)
	return relayLog.ID
}

func RelayLogUpdate(ctx context.Context, relayLog model.RelayLog) error {
	if relayLog.ID == 0 {
		return RelayLogAdd(ctx, relayLog)
	}
	fillRelayLogProtocols(&relayLog)
	relayLog.Processing = false
	relayLogInFlightLock.Lock()
	delete(relayLogInFlight, relayLog.ID)
	relayLogInFlightLock.Unlock()
	appendRelayLogRecent(relayLog)
	notifySubscribers(relayLog)
	return nil
}

// RelayLogProgress publishes an intermediate active snapshot, such as the
// first-token timestamp. Unknown IDs are ignored to avoid ghost records.
func RelayLogProgress(relayLog model.RelayLog) {
	if relayLog.ID == 0 {
		return
	}
	fillRelayLogProtocols(&relayLog)
	relayLog.Processing = true
	relayLogInFlightLock.Lock()
	if _, ok := relayLogInFlight[relayLog.ID]; !ok {
		relayLogInFlightLock.Unlock()
		return
	}
	relayLogInFlight[relayLog.ID] = relayLog
	relayLogInFlightLock.Unlock()
	appendRelayLogRecent(relayLog)
	notifySubscribers(relayLog)
}

func RelayLogSaveDBTask(ctx context.Context) error {
	trimRelayLogRecent()
	return nil
}

func trimRelayLogRecent() {
	relayLogRecentLock.Lock()
	pruneFinishedRelayLogsLocked()
	relayLogRecentLock.Unlock()
}

type RelayLogStatusFilter string

const (
	RelayLogStatusAll     RelayLogStatusFilter = ""
	RelayLogStatusSuccess RelayLogStatusFilter = "success"
	RelayLogStatusError   RelayLogStatusFilter = "error"
)

type RelayLogKeywordScope string

const (
	RelayLogKeywordScopeDefault RelayLogKeywordScope = ""
	RelayLogKeywordScopeContent RelayLogKeywordScope = "content"
)

type RelayLogCursor struct {
	Time int64 `json:"time"`
	ID   int64 `json:"id"`
}

type RelayLogKeywordMode string

const (
	RelayLogKeywordModeDefault  RelayLogKeywordMode = ""
	RelayLogKeywordModePrefix   RelayLogKeywordMode = "prefix"
	RelayLogKeywordModeExact    RelayLogKeywordMode = "exact"
	RelayLogKeywordModeContains RelayLogKeywordMode = "contains"
)

type RelayLogListFilter struct {
	StartTime      *int
	EndTime        *int
	ChannelIDs     []int
	Status         RelayLogStatusFilter
	Keyword        string
	KeywordScope   RelayLogKeywordScope
	KeywordMode    RelayLogKeywordMode
	Page           int
	PageSize       int
	IncludeContent bool
	WithTotal      bool
	Limit          int
	BeforeTime     *int64
	BeforeID       *int64
	Pagination     string
}

type RelayLogListResult struct {
	Logs       []model.RelayLog `json:"logs"`
	Total      int              `json:"total"`
	HasMore    bool             `json:"has_more"`
	NextCursor *RelayLogCursor  `json:"next_cursor,omitempty"`
	SearchMode string           `json:"search_mode,omitempty"`
	Warning    string           `json:"warning,omitempty"`
}

const (
	relayLogKeywordContainsMinLen     = 3
	relayLogKeywordContainsMaxWindow  = int64(7 * 24 * 60 * 60)
	relayLogKeywordContainsDefaultWin = int64(24 * 60 * 60)
)

var (
	ErrRelayLogContainsKeywordTooShort = &RelayLogFilterError{Code: "keyword_too_short", Message: "contains search requires keyword of at least 3 characters"}
	ErrRelayLogContainsWindowMissing   = &RelayLogFilterError{Code: "time_window_required", Message: "contains search requires an explicit time range"}
	ErrRelayLogContainsWindowTooWide   = &RelayLogFilterError{Code: "time_window_too_wide", Message: "contains search time window must be at most 7 days"}
)

type RelayLogFilterError struct {
	Code    string
	Message string
}

func (e *RelayLogFilterError) Error() string { return e.Message }

func RelayLogList(ctx context.Context, startTime, endTime *int, channelIDs []int, page, pageSize int) ([]model.RelayLog, error) {
	result, err := RelayLogListWithFilter(ctx, RelayLogListFilter{
		StartTime: startTime, EndTime: endTime, ChannelIDs: channelIDs,
		Page: page, PageSize: pageSize, IncludeContent: true, WithTotal: true,
	})
	return result.Logs, err
}

func RelayLogListWithFilter(ctx context.Context, filter RelayLogListFilter) (RelayLogListResult, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return RelayLogListResult{}, ctx.Err()
		default:
		}
	}

	cursorMode := filter.BeforeTime != nil || filter.BeforeID != nil || filter.Limit > 0
	switch filter.Pagination {
	case "cursor":
		cursorMode = true
	case "page":
		cursorMode = false
	}
	if filter.Limit < 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	if filter.Limit == 0 {
		filter.Limit = filter.PageSize
	}
	if filter.Limit < 1 {
		filter.Limit = 20
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	resolvedMode, warning, err := resolveRelayLogKeywordMode(&filter)
	if err != nil {
		return RelayLogListResult{}, err
	}
	filter.KeywordMode = resolvedMode

	channelSet := make(map[int]struct{}, len(filter.ChannelIDs))
	for _, id := range filter.ChannelIDs {
		channelSet[id] = struct{}{}
	}
	logs := relayLogCollectRecent(filter, channelSet, strings.ToLower(filter.Keyword), !filter.IncludeContent)
	if cursorMode {
		result := relayLogListCursor(filter, logs)
		result.SearchMode = relayLogSearchMode(filter)
		result.Warning = warning
		return result, nil
	}

	total := 0
	if filter.WithTotal {
		total = len(logs)
	}
	offset := (filter.Page - 1) * filter.PageSize
	if offset >= len(logs) {
		return RelayLogListResult{Logs: []model.RelayLog{}, Total: total, SearchMode: relayLogSearchMode(filter), Warning: warning}, nil
	}
	end := offset + filter.PageSize
	if end > len(logs) {
		end = len(logs)
	}
	return RelayLogListResult{Logs: logs[offset:end], Total: total, HasMore: end < len(logs), SearchMode: relayLogSearchMode(filter), Warning: warning}, nil
}

func relayLogListCursor(filter RelayLogListFilter, logs []model.RelayLog) RelayLogListResult {
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	selected := make([]model.RelayLog, 0, limit+1)
	for _, entry := range logs {
		if !relayLogBeforeCursor(entry, filter.BeforeTime, filter.BeforeID) {
			continue
		}
		selected = append(selected, entry)
		if len(selected) >= limit+1 {
			break
		}
	}
	hasMore := len(selected) > limit
	if hasMore {
		selected = selected[:limit]
	}
	var nextCursor *RelayLogCursor
	if hasMore && len(selected) > 0 {
		last := selected[len(selected)-1]
		nextCursor = &RelayLogCursor{Time: last.Time, ID: last.ID}
	}
	return RelayLogListResult{Logs: selected, HasMore: hasMore, NextCursor: nextCursor}
}

func relayLogBeforeCursor(entry model.RelayLog, beforeTime, beforeID *int64) bool {
	if beforeTime == nil && beforeID == nil {
		return true
	}
	if beforeTime != nil && beforeID != nil {
		return entry.Time < *beforeTime || (entry.Time == *beforeTime && entry.ID < *beforeID)
	}
	if beforeTime != nil {
		return entry.Time < *beforeTime
	}
	return beforeID == nil || entry.ID < *beforeID
}

func RelayLogGet(ctx context.Context, id int64) (*model.RelayLog, error) {
	if item, ok := relayLogFindInFlight(id); ok {
		fillRelayLogProtocols(&item)
		return &item, nil
	}
	if item, ok := relayLogFindRecent(id); ok {
		fillRelayLogProtocols(&item)
		return &item, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func relayLogCollectRecent(filter RelayLogListFilter, channelSet map[int]struct{}, keyword string, light bool) []model.RelayLog {
	relayLogRecentLock.RLock()
	result := make([]model.RelayLog, 0, len(relayLogRecent))
	for _, entry := range relayLogRecent {
		fillRelayLogProtocols(&entry)
		if !relayLogMatchesFilter(entry, filter, channelSet, keyword) {
			continue
		}
		if light {
			entry = relayLogLightCopy(entry)
		}
		result = append(result, entry)
	}
	relayLogRecentLock.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Time == result[j].Time {
			return result[i].ID > result[j].ID
		}
		return result[i].Time > result[j].Time
	})
	return result
}

func relayLogFindInFlight(id int64) (model.RelayLog, bool) {
	relayLogInFlightLock.RLock()
	entry, ok := relayLogInFlight[id]
	relayLogInFlightLock.RUnlock()
	return entry, ok
}

func relayLogFindRecent(id int64) (model.RelayLog, bool) {
	relayLogRecentLock.RLock()
	defer relayLogRecentLock.RUnlock()
	for i := len(relayLogRecent) - 1; i >= 0; i-- {
		if relayLogRecent[i].ID == id {
			return relayLogRecent[i], true
		}
	}
	return model.RelayLog{}, false
}

func relayLogLightCopy(entry model.RelayLog) model.RelayLog {
	entry.RequestContent = ""
	entry.ResponseContent = ""
	return entry
}

func relayLogMatchesFilter(relayLog model.RelayLog, filter RelayLogListFilter, channelSet map[int]struct{}, keyword string) bool {
	if filter.StartTime != nil && relayLog.Time < int64(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && relayLog.Time > int64(*filter.EndTime) {
		return false
	}
	if len(channelSet) > 0 {
		if _, ok := channelSet[relayLog.ChannelId]; !ok {
			return false
		}
	}
	if filter.Status == RelayLogStatusSuccess && !relayLog.Success {
		return false
	}
	if filter.Status == RelayLogStatusError && (relayLog.Success || relayLog.Processing) {
		return false
	}
	if keyword != "" && !logMatchesKeyword(relayLog, keyword, filter.KeywordScope, filter.KeywordMode) {
		return false
	}
	return true
}

func logMatchesKeyword(relayLog model.RelayLog, keyword string, scope RelayLogKeywordScope, mode RelayLogKeywordMode) bool {
	fields := []string{relayLog.RequestModelName, relayLog.ActualModelName, relayLog.RequestAPIKeyName, relayLog.ChannelName}
	if mode == RelayLogKeywordModeContains {
		fields = append(fields, relayLog.Error)
		if scope == RelayLogKeywordScopeContent {
			fields = append(fields, relayLog.RequestContent, relayLog.ResponseContent)
		}
	}
	for _, field := range fields {
		lower := strings.ToLower(field)
		switch mode {
		case RelayLogKeywordModeExact:
			if lower == keyword {
				return true
			}
		case RelayLogKeywordModeContains:
			if strings.Contains(lower, keyword) {
				return true
			}
		default:
			if strings.HasPrefix(lower, keyword) {
				return true
			}
		}
	}
	return false
}

func resolveRelayLogKeywordMode(filter *RelayLogListFilter) (RelayLogKeywordMode, string, error) {
	if filter.Keyword == "" {
		return RelayLogKeywordModeDefault, "", nil
	}
	mode := filter.KeywordMode
	if filter.KeywordScope == RelayLogKeywordScopeContent {
		mode = RelayLogKeywordModeContains
	}
	switch mode {
	case RelayLogKeywordModePrefix, RelayLogKeywordModeExact, RelayLogKeywordModeDefault:
		if mode == RelayLogKeywordModeDefault {
			mode = RelayLogKeywordModePrefix
		}
		return mode, "", nil
	case RelayLogKeywordModeContains:
		if len([]rune(filter.Keyword)) < relayLogKeywordContainsMinLen {
			return mode, "", ErrRelayLogContainsKeywordTooShort
		}
		now := time.Now().Unix()
		warning := ""
		if filter.StartTime == nil && filter.EndTime == nil {
			start := int(now - relayLogKeywordContainsDefaultWin)
			filter.StartTime = &start
			warning = "applied default 24h time window for contains search"
		} else {
			end := now
			if filter.EndTime != nil {
				end = int64(*filter.EndTime)
			}
			var start int64
			if filter.StartTime != nil {
				start = int64(*filter.StartTime)
			} else {
				start = end - relayLogKeywordContainsMaxWindow
				if start < 0 {
					start = 0
				}
				startInt := int(start)
				filter.StartTime = &startInt
			}
			if end-start > relayLogKeywordContainsMaxWindow {
				return mode, "", ErrRelayLogContainsWindowTooWide
			}
		}
		return mode, warning, nil
	default:
		return RelayLogKeywordModePrefix, "", nil
	}
}

func relayLogSearchMode(filter RelayLogListFilter) string {
	if filter.Keyword == "" {
		return ""
	}
	if filter.KeywordMode == RelayLogKeywordModeContains {
		return "slow"
	}
	return "fast"
}

func RelayLogClear(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	relayLogRecentLock.Lock()
	kept := make([]model.RelayLog, 0, len(relayLogRecent))
	for _, entry := range relayLogRecent {
		if entry.Processing {
			kept = append(kept, entry)
		}
	}
	relayLogRecent = kept
	relayLogRecentLock.Unlock()

	return nil
}
