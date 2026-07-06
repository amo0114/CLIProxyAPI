package opencodegoquota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	dashboardBaseURL = "https://opencode.ai/workspace"
	serverURL        = "https://opencode.ai/_server"
	workspacesID     = "def39973159c7f0483d8793a822b8dbb10d067e12c65455fcb4608459ba0234f"
	maxDashboardSize = 4 << 20
	requestTimeout   = 10 * time.Second
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	doer             HTTPDoer
	mu               sync.Mutex
	cache            map[string]cacheEntry
	serverFunctionID string
}

type FetchOptions struct {
	Force           bool
	TTL             time.Duration
	OnCookieRenewed func(authCookie string, renewedAt time.Time)
}

type cacheEntry struct {
	result  *QuotaResult
	err     error
	expires time.Time
}

func NewClient(doer HTTPDoer) *Client {
	if doer == nil {
		doer = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{
		doer:  doer,
		cache: make(map[string]cacheEntry),
	}
}

// NewClientWithOptions creates a client with an optional server function ID override.
// When serverFunctionID is empty, the built-in default workspacesID is used.
func NewClientWithOptions(doer HTTPDoer, serverFunctionID string) *Client {
	c := NewClient(doer)
	c.serverFunctionID = strings.TrimSpace(serverFunctionID)
	return c
}

// ServerFunctionID returns the effective server function identifier, falling back
// to the built-in default when no override is configured.
func (c *Client) ServerFunctionID() string {
	if c == nil {
		return workspacesID
	}
	if id := strings.TrimSpace(c.serverFunctionID); id != "" {
		return id
	}
	return workspacesID
}

func (c *Client) Fetch(ctx context.Context, cred Credential, opts FetchOptions) (*QuotaResult, error) {
	if c == nil {
		c = NewClient(nil)
	}
	cookieHeader, errCookie := BuildOpenCodeGoCookieHeader(cred.AuthCookie)
	if errCookie != nil {
		return nil, errCookie
	}
	var renewedCookie string
	var renewedAt time.Time
	handleCookieRenewal := func(resp *http.Response) {
		if resp == nil {
			return
		}
		candidateRenewedAt := time.Now().UTC()
		candidate, ok := ExtractRenewedOpenCodeGoAuthCookie(resp.Header.Values("Set-Cookie"), cookieHeader, candidateRenewedAt)
		if !ok {
			return
		}
		cookieHeader = "auth=" + candidate
		renewedCookie = candidate
		renewedAt = candidateRenewedAt
	}
	workspaceID, ok := ExtractWorkspaceID(cred.WorkspaceID)
	if !ok {
		var errWorkspace error
		workspaceID, errWorkspace = c.fetchWorkspaceID(ctx, cred.WorkspaceID, cookieHeader, handleCookieRenewal)
		if errWorkspace != nil {
			return nil, errWorkspace
		}
	}

	key := cacheKey(cred.ID, workspaceID, cookieHeader)
	now := time.Now()
	if !opts.Force {
		if result, errCached, ok := c.cached(key, now); ok {
			return result, errCached
		}
	}

	result, errFetch := c.fetchDashboard(ctx, workspaceID, cookieHeader, handleCookieRenewal)
	ttl := opts.TTL
	if errFetch != nil {
		ttl = time.Duration(failedRefreshIntervalSec) * time.Second
	} else if ttl <= 0 {
		ttl = time.Duration(NormalizeRefreshInterval(cred.RefreshIntervalSec)) * time.Second
	}
	if result != nil {
		result.CredentialID = cred.ID
		result.Name = cred.Name
		result.WorkspaceID = workspaceID
		filterWindows(result, cred)
	}
	if errFetch == nil && renewedCookie != "" && opts.OnCookieRenewed != nil {
		opts.OnCookieRenewed(renewedCookie, renewedAt)
	}
	c.store(key, result, errFetch, now.Add(ttl))
	return cloneResult(result, false), errFetch
}

type cookieRenewalHandler func(*http.Response)

func (c *Client) fetchWorkspaceID(ctx context.Context, rawWorkspace, cookieHeader string, handleCookieRenewal cookieRenewalHandler) (string, error) {
	text, errFetch := c.fetchServerFunction(ctx, http.MethodGet, nil, cookieHeader, handleCookieRenewal)
	if errFetch != nil {
		return "", errFetch
	}
	if looksLikeExpiredSession(text) {
		return "", newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
	}
	workspaces := ParseWorkspaceList(text)
	if len(workspaces) == 0 {
		fallback, errFallback := c.fetchServerFunction(ctx, http.MethodPost, []any{}, cookieHeader, handleCookieRenewal)
		if errFallback != nil {
			return "", errFallback
		}
		if looksLikeExpiredSession(fallback) {
			return "", newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
		}
		workspaces = ParseWorkspaceList(fallback)
	}
	return ResolveWorkspaceIDFromList(rawWorkspace, workspaces)
}

func (c *Client) fetchServerFunction(ctx context.Context, method string, args []any, cookieHeader string, handleCookieRenewal cookieRenewalHandler) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := serverURL
	var body io.Reader
	if strings.EqualFold(method, http.MethodGet) {
		values := url.Values{}
		values.Set("id", c.ServerFunctionID())
		if len(args) > 0 {
			argsJSON, errMarshal := json.Marshal(args)
			if errMarshal != nil {
				return "", newError(ErrorWorkspaceLookupFailed, "failed to encode OpenCode workspace request", errMarshal)
			}
			values.Set("args", string(argsJSON))
		}
		endpoint += "?" + values.Encode()
	} else {
		argsJSON, errMarshal := json.Marshal(args)
		if errMarshal != nil {
			return "", newError(ErrorWorkspaceLookupFailed, "failed to encode OpenCode workspace request", errMarshal)
		}
		body = bytes.NewReader(argsJSON)
	}

	req, errRequest := http.NewRequestWithContext(reqCtx, method, endpoint, body)
	if errRequest != nil {
		return "", newError(ErrorWorkspaceLookupFailed, "failed to build OpenCode workspace request", errRequest)
	}
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, */*;q=0.8")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Origin", "https://opencode.ai")
	req.Header.Set("Referer", "https://opencode.ai")
	req.Header.Set("User-Agent", "CLIProxyAPI-opencode-go-quota/1.0")
	req.Header.Set("X-Server-Id", c.ServerFunctionID())
	req.Header.Set("X-Server-Instance", "server-fn:cliproxy-opencode-go")
	if !strings.EqualFold(method, http.MethodGet) {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, errDo := c.doer.Do(req)
	if errDo != nil {
		return "", newError(ErrorWorkspaceLookupFailed, "failed to request OpenCode workspaces", errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if handleCookieRenewal != nil {
		handleCookieRenewal(resp)
	}

	if errStatus := classifyWorkspaceStatus(resp); errStatus != nil {
		return "", errStatus
	}
	data, errRead := readLimited(resp.Body, maxDashboardSize)
	if errRead != nil {
		return "", newError(ErrorWorkspaceLookupFailed, "failed to read OpenCode workspace response", errRead)
	}
	return string(data), nil
}

func (c *Client) cached(key string, now time.Time) (*QuotaResult, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || now.After(entry.expires) {
		if ok {
			delete(c.cache, key)
		}
		return nil, nil, false
	}
	return cloneResult(entry.result, true), entry.err, true
}

func (c *Client) store(key string, result *QuotaResult, err error, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{
		result:  cloneResult(result, false),
		err:     err,
		expires: expires,
	}
}

func (c *Client) fetchDashboard(ctx context.Context, workspaceID, cookieHeader string, handleCookieRenewal cookieRenewalHandler) (*QuotaResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	dashboardURL := dashboardBaseURL + "/" + url.PathEscape(workspaceID) + "/go"
	req, errRequest := http.NewRequestWithContext(reqCtx, http.MethodGet, dashboardURL, nil)
	if errRequest != nil {
		return nil, newError(ErrorDashboardFetchFailed, "failed to build OpenCode Go dashboard request", errRequest)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("User-Agent", "CLIProxyAPI-opencode-go-quota/1.0")

	resp, errDo := c.doer.Do(req)
	if errDo != nil {
		return nil, newError(ErrorDashboardFetchFailed, "failed to request OpenCode Go dashboard", errDo)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if handleCookieRenewal != nil {
		handleCookieRenewal(resp)
	}

	if errStatus := classifyDashboardStatus(resp); errStatus != nil {
		return nil, errStatus
	}

	body, errRead := readLimited(resp.Body, maxDashboardSize)
	if errRead != nil {
		return nil, newError(ErrorDashboardFetchFailed, "failed to read OpenCode Go dashboard response", errRead)
	}
	html := string(body)
	if looksLikeExpiredSession(html) {
		return nil, newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
	}
	result, errParse := ParseOpenCodeGoQuotaHTML(html, time.Now())
	if errParse != nil {
		return nil, errParse
	}
	result.WorkspaceID = workspaceID
	return result, nil
}

func classifyDashboardStatus(resp *http.Response) error {
	if resp == nil {
		return newError(ErrorDashboardFetchFailed, "OpenCode Go dashboard returned an empty response", nil)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location := strings.ToLower(resp.Header.Get("Location"))
		if strings.Contains(location, "sign-in") || strings.Contains(location, "login") {
			return newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
		}
		return newError(ErrorDashboardFetchFailed, "OpenCode Go dashboard redirected unexpectedly", nil)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
	case http.StatusNotFound:
		return newError(ErrorWorkspaceLookupFailed, "OpenCode Go workspace was not found", nil)
	default:
		return newError(
			ErrorDashboardFetchFailed,
			fmt.Sprintf("OpenCode Go dashboard returned HTTP %d", resp.StatusCode),
			nil,
		)
	}
}

func classifyWorkspaceStatus(resp *http.Response) error {
	if resp == nil {
		return newError(ErrorWorkspaceLookupFailed, "OpenCode workspace lookup returned an empty response", nil)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location := strings.ToLower(resp.Header.Get("Location"))
		if strings.Contains(location, "sign-in") || strings.Contains(location, "login") {
			return newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
		}
		return newError(ErrorWorkspaceLookupFailed, "OpenCode workspace lookup redirected unexpectedly", nil)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(ErrorAuthFailed, "authentication failed; update the OpenCode auth cookie", nil)
	default:
		return newError(
			ErrorWorkspaceLookupFailed,
			fmt.Sprintf("OpenCode workspace lookup returned HTTP %d", resp.StatusCode),
			nil,
		)
	}
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("dashboard response exceeds %d bytes", limit)
	}
	return body, nil
}

func looksLikeExpiredSession(html string) bool {
	lower := strings.ToLower(html)
	if strings.Contains(html, "rollingUsage") || strings.Contains(html, "weeklyUsage") || strings.Contains(html, "monthlyUsage") {
		return false
	}
	return strings.Contains(lower, "/sign-in") || strings.Contains(lower, "/login")
}

func filterWindows(result *QuotaResult, cred Credential) {
	if result == nil {
		return
	}
	if !cred.ShowRolling {
		result.Rolling = nil
	}
	if !cred.ShowWeekly {
		result.Weekly = nil
	}
	if !cred.ShowMonthly {
		result.Monthly = nil
	}
}

func cacheKey(id, workspaceID, cookieHeader string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{id, workspaceID, cookieHeader}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func cloneResult(result *QuotaResult, cached bool) *QuotaResult {
	if result == nil {
		return nil
	}
	out := *result
	out.Cached = cached
	if result.Error != nil {
		errCopy := *result.Error
		out.Error = &errCopy
	}
	out.Rolling = cloneWindow(result.Rolling)
	out.Weekly = cloneWindow(result.Weekly)
	out.Monthly = cloneWindow(result.Monthly)
	return &out
}

func cloneWindow(window *QuotaWindow) *QuotaWindow {
	if window == nil {
		return nil
	}
	out := *window
	return &out
}
