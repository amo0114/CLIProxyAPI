package usagestats

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const (
	defaultRecentLimit = 5000
	defaultGroupLimit  = 200
	maxGroupLimit      = 1000
)

var (
	enabled      atomic.Bool
	defaultStore = NewStore(defaultRecentLimit)
)

func init() {
	coreusage.RegisterNamedPlugin("management-usage-statistics", &plugin{store: defaultStore})
}

// SetEnabled toggles in-memory usage aggregation for management statistics.
func SetEnabled(value bool) { enabled.Store(value) }

// Enabled reports whether management usage aggregation is enabled.
func Enabled() bool { return enabled.Load() }

// Reset clears the default in-memory usage statistics store.
func Reset() { defaultStore.Reset() }

// DefaultStore returns the shared management usage statistics store.
func DefaultStore() *Store { return defaultStore }

type plugin struct {
	store *Store
}

func (p *plugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || p.store == nil || !Enabled() {
		return
	}
	p.store.Record(ctx, record)
}

// TokenStats captures the token usage breakdown for aggregated statistics.
type TokenStats struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// Counters captures request counts and token totals.
type Counters struct {
	Requests  int64      `json:"requests"`
	Success   int64      `json:"success"`
	Failed    int64      `json:"failed"`
	LatencyMs int64      `json:"latency_ms"`
	TTFTMs    int64      `json:"ttft_ms"`
	Tokens    TokenStats `json:"tokens"`
}

// GroupSnapshot captures usage aggregated for a provider/auth/model group.
type GroupSnapshot struct {
	Provider        string    `json:"provider"`
	ExecutorType    string    `json:"executor_type"`
	Model           string    `json:"model"`
	Alias           string    `json:"alias"`
	AuthIndex       string    `json:"auth_index"`
	AuthType        string    `json:"auth_type"`
	Source          string    `json:"source"`
	Endpoint        string    `json:"endpoint"`
	ReasoningEffort string    `json:"reasoning_effort"`
	ServiceTier     string    `json:"service_tier"`
	LastRequestAt   time.Time `json:"last_request_at"`
	Counters
}

// EventSnapshot captures a single recent request event without secrets.
type EventSnapshot struct {
	Timestamp       time.Time  `json:"timestamp"`
	Provider        string     `json:"provider"`
	ExecutorType    string     `json:"executor_type"`
	Model           string     `json:"model"`
	Alias           string     `json:"alias"`
	AuthIndex       string     `json:"auth_index"`
	AuthType        string     `json:"auth_type"`
	Source          string     `json:"source"`
	Endpoint        string     `json:"endpoint"`
	ReasoningEffort string     `json:"reasoning_effort"`
	ServiceTier     string     `json:"service_tier"`
	LatencyMs       int64      `json:"latency_ms"`
	TTFTMs          int64      `json:"ttft_ms"`
	Failed          bool       `json:"failed"`
	Fail            FailDetail `json:"fail"`
	Tokens          TokenStats `json:"tokens"`
}

// FailDetail captures failure metadata for recent request events.
type FailDetail struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body,omitempty"`
}

// Snapshot is the management-facing usage statistics payload.
type Snapshot struct {
	Enabled     bool            `json:"enabled"`
	GeneratedAt time.Time       `json:"generated_at"`
	Range       string          `json:"range"`
	RecentLimit int             `json:"recent_limit"`
	Truncated   bool            `json:"truncated"`
	Totals      Counters        `json:"totals"`
	Groups      []GroupSnapshot `json:"groups"`
	Recent      []EventSnapshot `json:"recent"`
}

// SnapshotOptions controls filtering and output size for snapshots.
type SnapshotOptions struct {
	Range string
	Limit int
	Now   time.Time
}

type groupKey struct {
	provider        string
	executorType    string
	model           string
	alias           string
	authIndex       string
	authType        string
	source          string
	endpoint        string
	reasoningEffort string
	serviceTier     string
}

type Store struct {
	mu          sync.RWMutex
	recentLimit int
	totals      Counters
	groups      map[groupKey]*GroupSnapshot
	recent      []EventSnapshot
}

// NewStore constructs an in-memory usage statistics store.
func NewStore(recentLimit int) *Store {
	if recentLimit <= 0 {
		recentLimit = defaultRecentLimit
	}
	return &Store{
		recentLimit: recentLimit,
		groups:      make(map[groupKey]*GroupSnapshot),
	}
}

// Reset clears all accumulated usage statistics.
func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.totals = Counters{}
	s.groups = make(map[groupKey]*GroupSnapshot)
	s.recent = nil
	s.mu.Unlock()
}

// Record ingests a usage record and updates aggregate counters.
func (s *Store) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	event := buildEvent(ctx, record)

	s.mu.Lock()
	defer s.mu.Unlock()

	addEventToCounters(&s.totals, event)
	key := event.groupKey()
	group := s.groups[key]
	if group == nil {
		group = &GroupSnapshot{
			Provider:        key.provider,
			ExecutorType:    key.executorType,
			Model:           key.model,
			Alias:           key.alias,
			AuthIndex:       key.authIndex,
			AuthType:        key.authType,
			Source:          key.source,
			Endpoint:        key.endpoint,
			ReasoningEffort: key.reasoningEffort,
			ServiceTier:     key.serviceTier,
		}
		s.groups[key] = group
	}
	addEventToCounters(&group.Counters, event)
	if event.Timestamp.After(group.LastRequestAt) || group.LastRequestAt.IsZero() {
		group.LastRequestAt = event.Timestamp
	}

	s.recent = append(s.recent, event)
	if len(s.recent) > s.recentLimit {
		copy(s.recent, s.recent[len(s.recent)-s.recentLimit:])
		s.recent = s.recent[:s.recentLimit]
	}
}

// Snapshot returns a consistent copy of usage statistics.
func (s *Store) Snapshot(options SnapshotOptions) Snapshot {
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	rangeLabel, since, filterByRange := normalizeRange(options.Range, now)
	limit := normalizeLimit(options.Limit)

	result := Snapshot{
		Enabled:     Enabled(),
		GeneratedAt: now,
		Range:       rangeLabel,
		RecentLimit: limit,
	}
	if s == nil {
		return result
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if filterByRange {
		groups := make(map[groupKey]*GroupSnapshot)
		for _, event := range s.recent {
			if event.Timestamp.Before(since) || event.Timestamp.After(now) {
				continue
			}
			addEventToCounters(&result.Totals, event)
			key := event.groupKey()
			group := groups[key]
			if group == nil {
				group = &GroupSnapshot{
					Provider:        key.provider,
					ExecutorType:    key.executorType,
					Model:           key.model,
					Alias:           key.alias,
					AuthIndex:       key.authIndex,
					AuthType:        key.authType,
					Source:          key.source,
					Endpoint:        key.endpoint,
					ReasoningEffort: key.reasoningEffort,
					ServiceTier:     key.serviceTier,
				}
				groups[key] = group
			}
			addEventToCounters(&group.Counters, event)
			if event.Timestamp.After(group.LastRequestAt) || group.LastRequestAt.IsZero() {
				group.LastRequestAt = event.Timestamp
			}
			result.Recent = append(result.Recent, event)
		}
		result.Groups = sortedGroups(groups, limit)
		result.Truncated = len(s.recent) >= s.recentLimit
	} else {
		result.Totals = s.totals
		result.Groups = sortedGroups(s.groups, limit)
		result.Recent = copyRecent(s.recent, limit)
		result.Truncated = len(s.recent) >= s.recentLimit
	}
	sortRecentDescending(result.Recent)
	return result
}

func buildEvent(ctx context.Context, record coreusage.Record) EventSnapshot {
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	model := normalizeText(record.Model, "unknown")
	alias := strings.TrimSpace(record.Alias)
	if alias == "" {
		alias = model
	}
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}

	return EventSnapshot{
		Timestamp:       timestamp,
		Provider:        normalizeText(record.Provider, "unknown"),
		ExecutorType:    normalizeText(record.ExecutorType, "unknown"),
		Model:           model,
		Alias:           alias,
		AuthIndex:       strings.TrimSpace(record.AuthIndex),
		AuthType:        normalizeText(record.AuthType, "unknown"),
		Source:          strings.TrimSpace(record.Source),
		Endpoint:        strings.TrimSpace(internallogging.GetEndpoint(ctx)),
		ReasoningEffort: normalizeText(resolveReasoningEffort(ctx, record), ""),
		ServiceTier:     normalizeText(resolveServiceTier(ctx, record), coreusage.DefaultServiceTier),
		LatencyMs:       normalizeDuration(record.Latency),
		TTFTMs:          normalizeDuration(record.TTFT),
		Failed:          failed,
		Fail:            resolveFail(ctx, record, failed),
		Tokens:          NormalizeDetail(record.Detail),
	}
}

// NormalizeDetail converts a usage.Detail into management token counters.
func NormalizeDetail(detail coreusage.Detail) TokenStats {
	tokens := TokenStats{
		InputTokens:         nonNegative(detail.InputTokens),
		OutputTokens:        nonNegative(detail.OutputTokens),
		ReasoningTokens:     nonNegative(detail.ReasoningTokens),
		CachedTokens:        nonNegative(detail.CachedTokens),
		CacheReadTokens:     nonNegative(detail.CacheReadTokens),
		CacheCreationTokens: nonNegative(detail.CacheCreationTokens),
		TotalTokens:         nonNegative(detail.TotalTokens),
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	return tokens
}

func addEventToCounters(counters *Counters, event EventSnapshot) {
	counters.Requests++
	if event.Failed {
		counters.Failed++
	} else {
		counters.Success++
	}
	counters.LatencyMs += event.LatencyMs
	counters.TTFTMs += event.TTFTMs
	counters.Tokens.InputTokens += event.Tokens.InputTokens
	counters.Tokens.OutputTokens += event.Tokens.OutputTokens
	counters.Tokens.ReasoningTokens += event.Tokens.ReasoningTokens
	counters.Tokens.CachedTokens += event.Tokens.CachedTokens
	counters.Tokens.CacheReadTokens += event.Tokens.CacheReadTokens
	counters.Tokens.CacheCreationTokens += event.Tokens.CacheCreationTokens
	counters.Tokens.TotalTokens += event.Tokens.TotalTokens
}

func (e EventSnapshot) groupKey() groupKey {
	return groupKey{
		provider:        e.Provider,
		executorType:    e.ExecutorType,
		model:           e.Model,
		alias:           e.Alias,
		authIndex:       e.AuthIndex,
		authType:        e.AuthType,
		source:          e.Source,
		endpoint:        e.Endpoint,
		reasoningEffort: e.ReasoningEffort,
		serviceTier:     e.ServiceTier,
	}
}

type groupMap interface {
	~map[groupKey]*GroupSnapshot
}

func sortedGroups[M groupMap](groups M, limit int) []GroupSnapshot {
	result := make([]GroupSnapshot, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		if left.Tokens.TotalTokens != right.Tokens.TotalTokens {
			return left.Tokens.TotalTokens > right.Tokens.TotalTokens
		}
		if left.Requests != right.Requests {
			return left.Requests > right.Requests
		}
		if !left.LastRequestAt.Equal(right.LastRequestAt) {
			return left.LastRequestAt.After(right.LastRequestAt)
		}
		return left.Model < right.Model
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func copyRecent(events []EventSnapshot, limit int) []EventSnapshot {
	if len(events) == 0 {
		return nil
	}
	start := 0
	if len(events) > limit {
		start = len(events) - limit
	}
	result := make([]EventSnapshot, len(events[start:]))
	copy(result, events[start:])
	return result
}

func sortRecentDescending(events []EventSnapshot) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
}

func normalizeRange(value string, now time.Time) (string, time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "7h":
		return "7h", now.Add(-7 * time.Hour), true
	case "24h", "":
		return "24h", now.Add(-24 * time.Hour), true
	case "7d":
		return "7d", now.Add(-7 * 24 * time.Hour), true
	case "all":
		return "all", time.Time{}, false
	default:
		return "24h", now.Add(-24 * time.Hour), true
	}
}

func normalizeLimit(value int) int {
	if value <= 0 {
		return defaultGroupLimit
	}
	if value > maxGroupLimit {
		return maxGroupLimit
	}
	return value
}

func normalizeText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeDuration(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return value.Milliseconds()
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func resolveReasoningEffort(ctx context.Context, record coreusage.Record) string {
	if effort := strings.TrimSpace(record.ReasoningEffort); effort != "" {
		return effort
	}
	return coreusage.ReasoningEffortFromContext(ctx)
}

func resolveServiceTier(ctx context.Context, record coreusage.Record) string {
	if tier := strings.TrimSpace(record.ServiceTier); tier != "" {
		return tier
	}
	return coreusage.ServiceTierFromContext(ctx)
}

func resolveSuccess(ctx context.Context) bool {
	status := internallogging.GetResponseStatus(ctx)
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

func resolveFail(ctx context.Context, record coreusage.Record, failed bool) FailDetail {
	fail := FailDetail{
		StatusCode: record.Fail.StatusCode,
		Body:       strings.TrimSpace(record.Fail.Body),
	}
	if !failed {
		return FailDetail{StatusCode: 200}
	}
	if fail.StatusCode <= 0 {
		fail.StatusCode = internallogging.GetResponseStatus(ctx)
	}
	if fail.StatusCode <= 0 {
		fail.StatusCode = httpStatusInternalServerError
	}
	return fail
}

const (
	httpStatusBadRequest          = 400
	httpStatusInternalServerError = 500
)
