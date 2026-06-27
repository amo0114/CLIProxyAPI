package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/opencodegoquota"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type managementRoundTripDoer func(*http.Request) (*http.Response, error)

func (f managementRoundTripDoer) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenCodeGoCredentialCreateListAndPatchDoNotExposeCookie(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	createBody := `{"name":"Work","workspace_id":"wrk_abc","auth_cookie":"Fe26.abcdef1234567890"}`
	createRec := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRec)
	createReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/opencode-go", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createCtx.Request = createReq
	h.CreateOpenCodeGoCredential(createCtx)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "abcdef1234567890") {
		t.Fatalf("create response exposed raw cookie: %s", createRec.Body.String())
	}

	var createResp struct {
		Credential opencodegoquota.Credential `json:"credential"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResp.Credential.ID == "" {
		t.Fatalf("expected credential id in create response: %s", createRec.Body.String())
	}

	auth, ok := manager.GetByID(createResp.Credential.ID)
	if !ok || auth == nil {
		t.Fatalf("created auth %q not registered", createResp.Credential.ID)
	}
	if got, _ := auth.Metadata["auth_cookie"].(string); got != "Fe26.abcdef1234567890" {
		t.Fatalf("stored auth_cookie = %q", got)
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	h.ListAuthFiles(listCtx)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "abcdef1234567890") {
		t.Fatalf("list response exposed raw cookie: %s", listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), "Fe26.abcd...7890") {
		t.Fatalf("list response missing masked cookie: %s", listRec.Body.String())
	}

	patchRec := httptest.NewRecorder()
	patchCtx, _ := gin.CreateTestContext(patchRec)
	patchCtx.Params = gin.Params{{Key: "id", Value: createResp.Credential.ID}}
	patchReq := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/opencode-go/"+createResp.Credential.ID, strings.NewReader(`{"name":"Renamed","workspace_id":"wrk_def","auth_cookie":""}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchCtx.Request = patchReq
	h.PatchOpenCodeGoCredential(patchCtx)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	updated, ok := manager.GetByID(createResp.Credential.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if got, _ := updated.Metadata["auth_cookie"].(string); got != "Fe26.abcdef1234567890" {
		t.Fatalf("empty patch auth_cookie overwrote stored cookie: %q", got)
	}
	if got, _ := updated.Metadata["workspace_id"].(string); got != "wrk_def" {
		t.Fatalf("workspace_id = %q, want wrk_def", got)
	}
}

func TestSanitizeOpenCodeGoAuthFileDownloadMasksCookie(t *testing.T) {
	raw := []byte(`{"type":"opencode_go","name":"Work","auth_cookie":"Fe26.abcdef1234567890"}`)
	sanitized := sanitizeOpenCodeGoAuthFileDownload(raw)
	if strings.Contains(string(sanitized), "abcdef1234567890") {
		t.Fatalf("sanitized download exposed raw cookie: %s", sanitized)
	}
	if !strings.Contains(string(sanitized), "Fe26.abcd...7890") {
		t.Fatalf("sanitized download missing masked cookie: %s", sanitized)
	}
}

func TestOpenCodeGoPatchCanExplicitlyClearCookie(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       "opencode-go-test.json",
		FileName: "opencode-go-test.json",
		Provider: opencodegoquota.ProviderType,
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"type":                 opencodegoquota.ProviderType,
			"name":                 "Work",
			"workspace_id":         "wrk_abc",
			"auth_cookie":          "Fe26.abcdef1234567890",
			"show_rolling":         true,
			"show_weekly":          true,
			"show_monthly":         true,
			"refresh_interval_sec": 60,
			"cookie_renewed_at":    "2026-06-27T01:00:00Z",
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: record.ID}}
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/opencode-go/"+record.ID, strings.NewReader(`{"clear_auth_cookie":true}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchOpenCodeGoCredential(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, ok := manager.GetByID(record.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if got, _ := updated.Metadata["auth_cookie"].(string); got != "" {
		t.Fatalf("auth_cookie = %q, want empty", got)
	}
	if got, ok := updated.Metadata["cookie_renewed_at"]; ok && got != "" {
		t.Fatalf("cookie_renewed_at = %v, want absent or empty", got)
	}
}

func TestOpenCodeGoQuotaCredentialListKeepsCookieInternalOnly(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       "opencode-go-test.json",
		FileName: "opencode-go-test.json",
		Provider: opencodegoquota.ProviderType,
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"type":                 opencodegoquota.ProviderType,
			"name":                 "Work",
			"workspace_id":         "wrk_abc",
			"auth_cookie":          "Fe26.abcdef1234567890",
			"show_rolling":         true,
			"show_weekly":          true,
			"show_monthly":         true,
			"refresh_interval_sec": 60,
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	credentials := h.listOpenCodeGoCredentials()
	if len(credentials) != 1 {
		t.Fatalf("credentials len = %d, want 1", len(credentials))
	}
	if got := credentials[0].AuthCookie; got != "Fe26.abcdef1234567890" {
		t.Fatalf("internal auth cookie = %q", got)
	}

	publicCredentials := publicOpenCodeGoCredentials(credentials)
	if len(publicCredentials) != 1 {
		t.Fatalf("public credentials len = %d, want 1", len(publicCredentials))
	}
	if got := publicCredentials[0].AuthCookie; got != "" {
		t.Fatalf("public auth cookie = %q, want empty", got)
	}
	if got := publicCredentials[0].MaskedAuthCookie; got != "Fe26.abcd...7890" {
		t.Fatalf("masked auth cookie = %q", got)
	}
}

func TestOpenCodeGoCookieRenewedAtZeroIsOmitted(t *testing.T) {
	credentialBody, errMarshal := json.Marshal(publicOpenCodeGoCredential(opencodegoquota.Credential{
		ID:      "opencode-go-test.json",
		Type:    opencodegoquota.ProviderType,
		Name:    "Work",
		Enabled: true,
	}))
	if errMarshal != nil {
		t.Fatalf("marshal credential: %v", errMarshal)
	}
	if strings.Contains(string(credentialBody), "cookie_renewed_at") || strings.Contains(string(credentialBody), "0001-01-01") {
		t.Fatalf("zero credential CookieRenewedAt was exposed: %s", credentialBody)
	}

	resultBody, errMarshal := json.Marshal(opencodegoquota.QuotaResult{
		CredentialID: "opencode-go-test.json",
		Name:         "Work",
		FetchedAt:    time.Date(2026, 6, 27, 1, 0, 0, 0, time.UTC),
	})
	if errMarshal != nil {
		t.Fatalf("marshal quota result: %v", errMarshal)
	}
	if strings.Contains(string(resultBody), "cookie_renewed_at") || strings.Contains(string(resultBody), "0001-01-01") {
		t.Fatalf("zero result CookieRenewedAt was exposed: %s", resultBody)
	}
}

func TestOpenCodeGoQuotaRefreshPersistsRenewedCookie(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:        "opencode-go-test.json",
		FileName:  "opencode-go-test.json",
		Provider:  opencodegoquota.ProviderType,
		Status:    coreauth.StatusActive,
		CreatedAt: time.Date(2026, 6, 27, 1, 0, 0, 0, time.UTC),
		Metadata: map[string]any{
			"type":                 opencodegoquota.ProviderType,
			"name":                 "Work",
			"workspace_id":         "wrk_abc",
			"auth_cookie":          "Fe26.old-cookie-value",
			"show_rolling":         true,
			"show_weekly":          true,
			"show_monthly":         true,
			"refresh_interval_sec": 60,
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.opencodeGoQuotaClient = opencodegoquota.NewClient(managementRoundTripDoer(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != "auth=Fe26.old-cookie-value" {
			t.Fatalf("Cookie header = %q", req.Header.Get("Cookie"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": []string{"auth=Fe26.new-cookie-value; Path=/; Expires=Sat, 27 Jun 2099 02:00:00 GMT; HttpOnly"},
			},
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}` +
					`weeklyUsage:$R[2]={usagePercent:30,resetInSec:120}` +
					`monthlyUsage:$R[3]={usagePercent:40,resetInSec:180}</script>`,
			)),
		}, nil
	}))

	results := h.fetchOpenCodeGoQuotaResults(context.Background(), []opencodegoquota.Credential{
		h.openCodeGoCredentialFromAuth(record),
	}, true)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Fatalf("quota error = %+v", results[0].Error)
	}
	if results[0].CookieRenewedAt.IsZero() {
		t.Fatal("expected result CookieRenewedAt")
	}
	body, errMarshal := json.Marshal(results[0])
	if errMarshal != nil {
		t.Fatalf("marshal result: %v", errMarshal)
	}
	if strings.Contains(string(body), "new-cookie-value") || strings.Contains(string(body), "old-cookie-value") {
		t.Fatalf("quota result exposed cookie: %s", body)
	}

	updated, ok := manager.GetByID(record.ID)
	if !ok || updated == nil {
		t.Fatal("updated auth not found")
	}
	if got, _ := updated.Metadata["auth_cookie"].(string); got != "Fe26.new-cookie-value" {
		t.Fatalf("stored auth_cookie = %q, want renewed cookie", got)
	}
	if got, _ := updated.Metadata["cookie_renewed_at"].(string); got == "" {
		t.Fatal("cookie_renewed_at was not stored")
	}
}

func TestOpenCodeGoQuotaResultKeepsStoredCookieRenewedAt(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	renewedAt := time.Date(2026, 6, 27, 1, 30, 0, 0, time.UTC)
	record := &coreauth.Auth{
		ID:       "opencode-go-test.json",
		FileName: "opencode-go-test.json",
		Provider: opencodegoquota.ProviderType,
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{
			"type":                 opencodegoquota.ProviderType,
			"name":                 "Work",
			"workspace_id":         "wrk_abc",
			"auth_cookie":          "Fe26.old-cookie-value",
			"cookie_renewed_at":    renewedAt.Format(time.RFC3339),
			"show_rolling":         true,
			"show_weekly":          true,
			"show_monthly":         true,
			"refresh_interval_sec": 60,
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	h.opencodeGoQuotaClient = opencodegoquota.NewClient(managementRoundTripDoer(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != "auth=Fe26.old-cookie-value" {
			t.Fatalf("Cookie header = %q", req.Header.Get("Cookie"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}` +
					`weeklyUsage:$R[2]={usagePercent:30,resetInSec:120}` +
					`monthlyUsage:$R[3]={usagePercent:40,resetInSec:180}</script>`,
			)),
		}, nil
	}))

	results := h.fetchOpenCodeGoQuotaResults(context.Background(), []opencodegoquota.Credential{
		h.openCodeGoCredentialFromAuth(record),
	}, true)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Fatalf("quota error = %+v", results[0].Error)
	}
	if !results[0].CookieRenewedAt.Equal(renewedAt) {
		t.Fatalf("CookieRenewedAt = %s, want %s", results[0].CookieRenewedAt.Format(time.RFC3339), renewedAt.Format(time.RFC3339))
	}
}
