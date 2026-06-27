package opencodegoquota

import (
	"testing"
	"time"
)

func TestParseOpenCodeGoQuotaHTML(t *testing.T) {
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	html := `<html><script>` +
		`rollingUsage:$R[10]={usagePercent:57.5,resetInSec:3600}` +
		`weeklyUsage:$R[11]={resetInSec:7200,usagePercent:23}` +
		`monthlyUsage:$R[12]={"usagePercent":11,"resetInSec":86400}` +
		`plan:$R[13]="go"` +
		`</script></html>`

	got, err := ParseOpenCodeGoQuotaHTML(html, now)
	if err != nil {
		t.Fatalf("ParseOpenCodeGoQuotaHTML() error = %v", err)
	}
	if got.Plan != "go" {
		t.Fatalf("Plan = %q, want go", got.Plan)
	}
	if got.Rolling == nil || got.Rolling.UsagePercent != 57.5 || got.Rolling.ResetInSec != 3600 {
		t.Fatalf("unexpected rolling = %+v", got.Rolling)
	}
	if got.Weekly == nil || got.Weekly.UsagePercent != 23 || got.Weekly.ResetAt != now.Add(2*time.Hour) {
		t.Fatalf("unexpected weekly = %+v", got.Weekly)
	}
	if got.Monthly == nil || got.Monthly.RemainingPercent != 89 {
		t.Fatalf("unexpected monthly = %+v", got.Monthly)
	}
}

func TestParseOpenCodeGoQuotaHTMLNoUsage(t *testing.T) {
	_, err := ParseOpenCodeGoQuotaHTML(`<html></html>`, time.Now())
	if err == nil {
		t.Fatal("expected parse error")
	}
	quotaErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if quotaErr.Code != ErrorDashboardParseFailed {
		t.Fatalf("error code = %s, want %s", quotaErr.Code, ErrorDashboardParseFailed)
	}
}

func TestParseOpenCodeGoQuotaHTMLJSONLikeUsage(t *testing.T) {
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	html := `{"rollingUsage":{"usagePercent":10,"resetInSec":20},` +
		`"weeklyUsage":{"resetInSec":30,"usagePercent":40},` +
		`"monthlyUsage":{"usagePercent":50,"resetInSec":60}}`

	got, err := ParseOpenCodeGoQuotaHTML(html, now)
	if err != nil {
		t.Fatalf("ParseOpenCodeGoQuotaHTML() error = %v", err)
	}
	if got.Rolling == nil || got.Rolling.UsagePercent != 10 {
		t.Fatalf("unexpected rolling = %+v", got.Rolling)
	}
	if got.Weekly == nil || got.Weekly.ResetInSec != 30 {
		t.Fatalf("unexpected weekly = %+v", got.Weekly)
	}
	if got.Monthly == nil || got.Monthly.UsagePercent != 50 {
		t.Fatalf("unexpected monthly = %+v", got.Monthly)
	}
}
