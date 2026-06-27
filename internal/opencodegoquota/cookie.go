package opencodegoquota

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BuildOpenCodeGoCookieHeader normalizes user-provided cookie material into
// the minimal Cookie header required by opencode.ai.
func BuildOpenCodeGoCookieHeader(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", newError(ErrorAuthCookieEmpty, "auth cookie is required", nil)
	}
	if strings.HasPrefix(strings.ToLower(value), "cookie:") {
		value = strings.TrimSpace(value[len("cookie:"):])
	}
	if value == "" {
		return "", newError(ErrorAuthCookieEmpty, "auth cookie is required", nil)
	}
	if !strings.Contains(value, "=") {
		if strings.HasPrefix(value, "Fe26.") {
			return "auth=" + value, nil
		}
		return "", newError(ErrorAuthCookieEmpty, "auth cookie must contain an auth value", nil)
	}

	for _, part := range strings.Split(value, ";") {
		name, cookieValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "auth") {
			cookieValue = strings.TrimSpace(cookieValue)
			if cookieValue == "" {
				break
			}
			return "auth=" + cookieValue, nil
		}
	}

	return "", newError(ErrorAuthCookieEmpty, "auth cookie must contain an auth value", nil)
}

func MaskOpenCodeGoCookie(raw string) string {
	header, err := BuildOpenCodeGoCookieHeader(raw)
	if err != nil {
		return ""
	}
	_, value, ok := strings.Cut(header, "=")
	if !ok {
		return "configured"
	}
	value = strings.TrimSpace(value)
	if len(value) < 16 {
		return "configured"
	}
	prefixLen := 9
	if len(value) < prefixLen+4 {
		return "configured"
	}
	return fmt.Sprintf("%s...%s", value[:prefixLen], value[len(value)-4:])
}

func ExtractRenewedOpenCodeGoAuthCookie(setCookieHeaders []string, currentCookieHeader string, now time.Time) (string, bool) {
	if len(setCookieHeaders) == 0 {
		return "", false
	}
	if now.IsZero() {
		now = time.Now()
	}
	currentHeader, errCurrent := BuildOpenCodeGoCookieHeader(currentCookieHeader)
	if errCurrent != nil {
		return "", false
	}
	_, currentValue, okCurrent := strings.Cut(currentHeader, "=")
	if !okCurrent {
		return "", false
	}

	resp := http.Response{Header: http.Header{"Set-Cookie": setCookieHeaders}}
	var candidate string
	for _, cookie := range resp.Cookies() {
		if cookie == nil || !strings.EqualFold(strings.TrimSpace(cookie.Name), "auth") {
			continue
		}
		value := strings.TrimSpace(cookie.Value)
		if value == "" || cookie.MaxAge < 0 {
			continue
		}
		if !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			continue
		}
		candidate = value
	}
	if candidate == "" || candidate == strings.TrimSpace(currentValue) {
		return "", false
	}
	return candidate, true
}
