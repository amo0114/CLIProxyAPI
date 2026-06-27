package usagestats

import (
	"context"
	"net/http"
	"testing"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestStoreRecordNormalizesTokensWithoutDoubleCountingCache(t *testing.T) {
	store := NewStore(10)
	ctx := internallogging.WithEndpoint(context.Background(), "POST /v1/chat/completions")

	store.Record(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.4",
		Alias:       "client-model",
		AuthIndex:   "auth-1",
		AuthType:    "apikey",
		RequestedAt: time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:         10,
			OutputTokens:        20,
			ReasoningTokens:     5,
			CachedTokens:        100,
			CacheReadTokens:     7,
			CacheCreationTokens: 11,
			TotalTokens:         35,
		},
	})

	snapshot := store.Snapshot(SnapshotOptions{Range: "all"})
	if snapshot.Totals.Requests != 1 {
		t.Fatalf("requests = %d, want 1", snapshot.Totals.Requests)
	}
	if snapshot.Totals.Tokens.TotalTokens != 35 {
		t.Fatalf("total tokens = %d, want 35", snapshot.Totals.Tokens.TotalTokens)
	}
	if snapshot.Totals.Tokens.CachedTokens != 100 {
		t.Fatalf("cached tokens = %d, want 100", snapshot.Totals.Tokens.CachedTokens)
	}
	if len(snapshot.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(snapshot.Groups))
	}
	if snapshot.Groups[0].Endpoint != "POST /v1/chat/completions" {
		t.Fatalf("endpoint = %q, want request endpoint", snapshot.Groups[0].Endpoint)
	}
}

func TestNormalizeDetailFallsBackLikeUsageQueue(t *testing.T) {
	tokens := NormalizeDetail(coreusage.Detail{
		InputTokens:     3,
		OutputTokens:    4,
		ReasoningTokens: 5,
		CachedTokens:    99,
	})
	if tokens.TotalTokens != 12 {
		t.Fatalf("total tokens = %d, want input+output+reasoning fallback", tokens.TotalTokens)
	}

	onlyCached := NormalizeDetail(coreusage.Detail{CachedTokens: 9})
	if onlyCached.TotalTokens != 9 {
		t.Fatalf("cached-only total tokens = %d, want 9", onlyCached.TotalTokens)
	}
}

func TestStoreSnapshotRangeFiltersRecentEvents(t *testing.T) {
	store := NewStore(10)
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)

	store.Record(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "old-model",
		RequestedAt: now.Add(-25 * time.Hour),
		Detail:      coreusage.Detail{InputTokens: 100, OutputTokens: 1},
	})
	store.Record(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "new-model",
		RequestedAt: now.Add(-2 * time.Hour),
		Detail:      coreusage.Detail{InputTokens: 10, OutputTokens: 2},
	})

	snapshot := store.Snapshot(SnapshotOptions{Range: "24h", Now: now})
	if snapshot.Totals.Requests != 1 {
		t.Fatalf("24h requests = %d, want 1", snapshot.Totals.Requests)
	}
	if snapshot.Totals.Tokens.TotalTokens != 12 {
		t.Fatalf("24h total tokens = %d, want 12", snapshot.Totals.Tokens.TotalTokens)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].Model != "new-model" {
		t.Fatalf("24h groups = %#v, want new-model only", snapshot.Groups)
	}

	all := store.Snapshot(SnapshotOptions{Range: "all", Now: now})
	if all.Totals.Requests != 2 {
		t.Fatalf("all requests = %d, want 2", all.Totals.Requests)
	}
}

func TestPluginRespectsEnabledFlagAndFailureStatus(t *testing.T) {
	store := NewStore(10)
	p := &plugin{store: store}
	ctx := internallogging.WithResponseStatusHolder(context.Background())
	internallogging.SetResponseStatus(ctx, http.StatusTooManyRequests)

	SetEnabled(false)
	p.HandleUsage(ctx, coreusage.Record{
		Provider: "openai",
		Model:    "gpt-5.4",
		Detail:   coreusage.Detail{InputTokens: 1, OutputTokens: 2},
	})
	if got := store.Snapshot(SnapshotOptions{Range: "all"}).Totals.Requests; got != 0 {
		t.Fatalf("disabled requests = %d, want 0", got)
	}

	SetEnabled(true)
	defer SetEnabled(false)
	p.HandleUsage(ctx, coreusage.Record{
		Provider: "openai",
		Model:    "gpt-5.4",
		Detail:   coreusage.Detail{InputTokens: 1, OutputTokens: 2},
	})

	snapshot := store.Snapshot(SnapshotOptions{Range: "all"})
	if snapshot.Totals.Failed != 1 {
		t.Fatalf("failed = %d, want 1", snapshot.Totals.Failed)
	}
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Fail.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("recent fail = %#v, want status %d", snapshot.Recent, http.StatusTooManyRequests)
	}
}
