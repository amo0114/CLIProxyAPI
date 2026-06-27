package opencodegoquota

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var (
	usagePercentPattern = regexp.MustCompile(`"?usagePercent"?\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	resetInSecPattern   = regexp.MustCompile(`"?resetInSec"?\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	planPattern         = regexp.MustCompile(`plan:\$R\[\d+\]="([^"]+)"`)
)

func ParseOpenCodeGoQuotaHTML(html string, now time.Time) (*QuotaResult, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	result := &QuotaResult{
		Rolling:   parseUsageWindow(html, "rolling", "rolling"),
		Weekly:    parseUsageWindow(html, "weekly", "weekly"),
		Monthly:   parseUsageWindow(html, "monthly", "monthly"),
		FetchedAt: now,
	}
	if match := planPattern.FindStringSubmatch(html); len(match) == 2 {
		result.Plan = match[1]
	}
	for _, window := range []*QuotaWindow{result.Rolling, result.Weekly, result.Monthly} {
		if window == nil {
			continue
		}
		if window.ResetInSec > 0 {
			window.ResetAt = now.Add(time.Duration(window.ResetInSec) * time.Second)
		}
	}
	if result.Rolling == nil && result.Weekly == nil && result.Monthly == nil {
		return nil, newError(
			ErrorDashboardParseFailed,
			"unable to parse OpenCode Go quota data from dashboard response",
			nil,
		)
	}
	return result, nil
}

func parseUsageWindow(html, key, label string) *QuotaWindow {
	objectPattern := regexp.MustCompile(fmt.Sprintf(`"?%sUsage"?\s*(?::\s*(?:\$R\[\d+\]\s*=)?|=\s*)\{([^}]*)\}`, regexp.QuoteMeta(key)))
	objectMatch := objectPattern.FindStringSubmatch(html)
	if len(objectMatch) != 2 {
		return nil
	}
	body := objectMatch[1]
	usageMatch := usagePercentPattern.FindStringSubmatch(body)
	resetMatch := resetInSecPattern.FindStringSubmatch(body)
	if len(usageMatch) != 2 || len(resetMatch) != 2 {
		return nil
	}
	usage, errUsage := strconv.ParseFloat(usageMatch[1], 64)
	if errUsage != nil {
		return nil
	}
	resetFloat, errReset := strconv.ParseFloat(resetMatch[1], 64)
	if errReset != nil {
		return nil
	}
	reset := int64(resetFloat)
	if resetFloat > float64(reset) {
		reset++
	}
	usage = clampPercent(usage)
	return &QuotaWindow{
		Label:            label,
		UsagePercent:     usage,
		RemainingPercent: clampPercent(100 - usage),
		ResetInSec:       reset,
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
