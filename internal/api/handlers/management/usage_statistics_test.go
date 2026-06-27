package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestats"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageStatisticsReturnsAggregatedSnapshot(t *testing.T) {
	usagestats.Reset()
	usagestats.SetEnabled(true)
	defer func() {
		usagestats.Reset()
		usagestats.SetEnabled(false)
	}()

	usagestats.DefaultStore().Record(nil, coreusage.Record{
		Provider:    "openai-compatible",
		Model:       "opencode-model",
		Alias:       "client-model",
		AuthIndex:   "auth-1",
		AuthType:    "apikey",
		RequestedAt: time.Now().Add(-time.Hour),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-statistics?range=24h&limit=5", nil)

	h := &Handler{}
	h.GetUsageStatistics(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload usagestats.Snapshot
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("unmarshal response: %v", errUnmarshal)
	}
	if !payload.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if payload.Totals.Requests != 1 || payload.Totals.Tokens.TotalTokens != 30 {
		t.Fatalf("totals = %#v, want one request with 30 tokens", payload.Totals)
	}
	if len(payload.Groups) != 1 || payload.Groups[0].AuthIndex != "auth-1" {
		t.Fatalf("groups = %#v, want auth-1 group", payload.Groups)
	}
}

func TestGetUsageStatisticsRejectsInvalidLimit(t *testing.T) {
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-statistics?limit=0", nil)

	h := &Handler{}
	h.GetUsageStatistics(ginCtx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
