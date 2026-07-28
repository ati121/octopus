package relay

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm/clause"
)

const (
	wsAffinityMaxTTL      = time.Hour
	wsAffinityHotMaxItems = 10000
)

type wsAffinityScope struct {
	APIKeyID     int
	GroupID      int
	RequestModel string
	ResponseID   string
}

type wsAffinityEntry struct {
	ChannelID     int
	ChannelKeyID  int
	UpstreamModel string
	ExpiresAt     time.Time
}

type wsAffinityStore interface {
	Get(ctx context.Context, scope wsAffinityScope) (*wsAffinityEntry, bool)
	Set(ctx context.Context, scope wsAffinityScope, entry wsAffinityEntry, ttl time.Duration) error
	Delete(ctx context.Context, scope wsAffinityScope) error
}

type wsAffinityHotItem struct {
	key   string
	entry wsAffinityEntry
}

type wsAffinityHotCache struct {
	mu         sync.Mutex
	maxEntries int
	items      map[string]*list.Element
	lru        list.List
}

func newWSAffinityHotCache(maxEntries int) *wsAffinityHotCache {
	if maxEntries <= 0 {
		maxEntries = wsAffinityHotMaxItems
	}
	return &wsAffinityHotCache{
		maxEntries: maxEntries,
		items:      make(map[string]*list.Element, maxEntries),
	}
}

func (c *wsAffinityHotCache) Get(key string, now time.Time) (wsAffinityEntry, bool) {
	if c == nil {
		return wsAffinityEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return wsAffinityEntry{}, false
	}
	item := element.Value.(*wsAffinityHotItem)
	if !item.entry.ExpiresAt.IsZero() && !now.Before(item.entry.ExpiresAt) {
		c.removeElement(element)
		return wsAffinityEntry{}, false
	}
	c.lru.MoveToFront(element)
	return item.entry, true
}

func (c *wsAffinityHotCache) Set(key string, entry wsAffinityEntry) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		element.Value.(*wsAffinityHotItem).entry = entry
		c.lru.MoveToFront(element)
		return
	}
	element := c.lru.PushFront(&wsAffinityHotItem{key: key, entry: entry})
	c.items[key] = element
	for len(c.items) > c.maxEntries {
		c.removeElement(c.lru.Back())
	}
}

func (c *wsAffinityHotCache) Del(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		c.removeElement(element)
	}
}

func (c *wsAffinityHotCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *wsAffinityHotCache) removeElement(element *list.Element) {
	if element == nil {
		return
	}
	item := element.Value.(*wsAffinityHotItem)
	delete(c.items, item.key)
	c.lru.Remove(element)
}

type dbWSAffinityStore struct {
	hot *wsAffinityHotCache
}

func newDBWSAffinityStore() wsAffinityStore {
	return &dbWSAffinityStore{hot: newWSAffinityHotCache(wsAffinityHotMaxItems)}
}

var defaultWSAffinityStore wsAffinityStore = newDBWSAffinityStore()

func getWSAffinityStore() wsAffinityStore {
	if defaultWSAffinityStore == nil {
		defaultWSAffinityStore = newDBWSAffinityStore()
	}
	return defaultWSAffinityStore
}

func (s *dbWSAffinityStore) Get(ctx context.Context, scope wsAffinityScope) (*wsAffinityEntry, bool) {
	key, hash, ok := normalizeWSAffinityScope(scope)
	if !ok {
		return nil, false
	}
	now := time.Now()
	if s != nil && s.hot != nil {
		if entry, found := s.hot.Get(key, now); found {
			cloned := entry
			return &cloned, true
		}
	}

	dbConn := db.GetDB()
	if dbConn == nil {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var record model.WSResponseAffinity
	if err := dbConn.WithContext(ctx).
		Where("api_key_id = ? AND group_id = ? AND request_model = ? AND response_id_hash = ?", scope.APIKeyID, scope.GroupID, strings.TrimSpace(scope.RequestModel), hash).
		First(&record).Error; err != nil {
		return nil, false
	}
	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		_ = dbConn.WithContext(ctx).Delete(&record).Error
		return nil, false
	}
	entry := wsAffinityEntry{
		ChannelID:     record.ChannelID,
		ChannelKeyID:  record.ChannelKeyID,
		UpstreamModel: strings.TrimSpace(record.UpstreamModel),
		ExpiresAt:     record.ExpiresAt,
	}
	if s != nil && s.hot != nil {
		s.hot.Set(key, entry)
	}
	return &entry, true
}

func (s *dbWSAffinityStore) Set(ctx context.Context, scope wsAffinityScope, entry wsAffinityEntry, ttl time.Duration) error {
	key, hash, ok := normalizeWSAffinityScope(scope)
	if !ok || entry.ChannelID <= 0 || entry.ChannelKeyID <= 0 {
		return nil
	}
	if ttl <= 0 || ttl > wsAffinityMaxTTL {
		ttl = wsAffinityMaxTTL
	}
	expiresAt := time.Now().Add(ttl)
	entry.ExpiresAt = expiresAt
	entry.UpstreamModel = strings.TrimSpace(entry.UpstreamModel)
	if s != nil && s.hot != nil {
		s.hot.Set(key, entry)
	}

	dbConn := db.GetDB()
	if dbConn == nil {
		return fmt.Errorf("db is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record := model.WSResponseAffinity{
		APIKeyID:       scope.APIKeyID,
		GroupID:        scope.GroupID,
		RequestModel:   strings.TrimSpace(scope.RequestModel),
		ResponseIDHash: hash,
		ChannelID:      entry.ChannelID,
		ChannelKeyID:   entry.ChannelKeyID,
		UpstreamModel:  entry.UpstreamModel,
		ExpiresAt:      expiresAt,
	}
	return dbConn.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "api_key_id"},
			{Name: "group_id"},
			{Name: "request_model"},
			{Name: "response_id_hash"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"channel_id", "channel_key_id", "upstream_model", "expires_at", "updated_at"}),
	}).Create(&record).Error
}

func (s *dbWSAffinityStore) Delete(ctx context.Context, scope wsAffinityScope) error {
	key, hash, ok := normalizeWSAffinityScope(scope)
	if !ok {
		return nil
	}
	if s != nil && s.hot != nil {
		s.hot.Del(key)
	}
	dbConn := db.GetDB()
	if dbConn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return dbConn.WithContext(ctx).
		Where("api_key_id = ? AND group_id = ? AND request_model = ? AND response_id_hash = ?", scope.APIKeyID, scope.GroupID, strings.TrimSpace(scope.RequestModel), hash).
		Delete(&model.WSResponseAffinity{}).Error
}

func normalizeWSAffinityScope(scope wsAffinityScope) (cacheKey string, responseHash string, ok bool) {
	requestModel := strings.TrimSpace(scope.RequestModel)
	responseID := strings.TrimSpace(scope.ResponseID)
	if scope.APIKeyID <= 0 || scope.GroupID <= 0 || requestModel == "" || responseID == "" {
		return "", "", false
	}
	responseHash = hashWSResponseID(responseID)
	requestModelHash := sha256.Sum256([]byte(requestModel))
	cacheKey = fmt.Sprintf("%d:%d:%x:%s", scope.APIKeyID, scope.GroupID, requestModelHash, responseHash)
	return cacheKey, responseHash, true
}

func hashWSResponseID(responseID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(responseID)))
	return hex.EncodeToString(sum[:])
}

func wsAffinityTTL(groupSessionKeepTimeSec int) time.Duration {
	if groupSessionKeepTimeSec <= 0 {
		return wsAffinityMaxTTL
	}
	ttl := time.Duration(groupSessionKeepTimeSec) * time.Second
	if ttl <= 0 || ttl > wsAffinityMaxTTL {
		return wsAffinityMaxTTL
	}
	return ttl
}

func resetWSAffinityStoreForTest() {
	defaultWSAffinityStore = newDBWSAffinityStore()
}

func setWSAffinityStoreForTest(store wsAffinityStore) {
	defaultWSAffinityStore = store
}

var _ wsAffinityStore = (*dbWSAffinityStore)(nil)
