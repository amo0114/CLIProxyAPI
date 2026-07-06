package management

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/opencodegoquota"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type openCodeGoCredentialRequest struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	WorkspaceID        string `json:"workspace_id"`
	AuthCookie         string `json:"auth_cookie"`
	ClearAuthCookie    bool   `json:"clear_auth_cookie"`
	Enabled            *bool  `json:"enabled"`
	ShowRolling        *bool  `json:"show_rolling"`
	ShowWeekly         *bool  `json:"show_weekly"`
	ShowMonthly        *bool  `json:"show_monthly"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
}

func (h *Handler) CreateOpenCodeGoCredential(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}

	var req openCodeGoCredentialRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	cred, errBuild := openCodeGoCredentialFromCreateRequest(req)
	if errBuild != nil {
		writeOpenCodeGoError(c, errBuild)
		return
	}

	now := time.Now().UTC()
	fileName := newOpenCodeGoFileName(cred.Name)
	metadata := openCodeGoCredentialMetadata(cred, now, now)
	data, errMarshal := json.Marshal(metadata)
	if errMarshal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode credential"})
		return
	}
	if errWrite := h.writeAuthFile(c.Request.Context(), fileName, data); errWrite != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errWrite.Error()})
		return
	}

	auth := h.findOpenCodeGoAuth(fileName)
	if auth == nil {
		c.JSON(http.StatusCreated, gin.H{"status": "ok", "file": fileName})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"status":     "ok",
		"credential": h.publicOpenCodeGoCredential(auth),
	})
}

func (h *Handler) openCodeGoQuotaClient() *opencodegoquota.Client {
	if h == nil {
		return opencodegoquota.NewClient(nil)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.opencodeGoQuotaClient == nil {
		h.opencodeGoQuotaClient = newOpenCodeGoQuotaClient(h.cfg)
	}
	return h.opencodeGoQuotaClient
}

func newOpenCodeGoQuotaClient(cfg *config.Config) *opencodegoquota.Client {
	if cfg != nil {
		if id := strings.TrimSpace(cfg.OpencodeGo.ServerFunctionID); id != "" {
			return opencodegoquota.NewClientWithOptions(nil, id)
		}
	}
	return opencodegoquota.NewClient(nil)
}

func (h *Handler) PatchOpenCodeGoCredential(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	var req openCodeGoCredentialRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	auth := h.findOpenCodeGoAuth(id)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OpenCode Go credential not found"})
		return
	}
	if coreauth.IsPluginVirtualAuth(auth) {
		c.JSON(http.StatusConflict, gin.H{"error": errPluginVirtualAuth.Error()})
		return
	}

	cred := h.openCodeGoCredentialFromAuth(auth)
	if strings.TrimSpace(req.Name) != "" {
		cred.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.WorkspaceID) != "" {
		cred.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	}
	if req.ClearAuthCookie {
		cred.AuthCookie = ""
		cred.CookieRenewedAt = time.Time{}
	} else if strings.TrimSpace(req.AuthCookie) != "" {
		cred.AuthCookie = strings.TrimSpace(req.AuthCookie)
		cred.CookieRenewedAt = time.Time{}
	}
	if req.Enabled != nil {
		cred.Enabled = *req.Enabled
	}
	if req.ShowRolling != nil {
		cred.ShowRolling = *req.ShowRolling
	}
	if req.ShowWeekly != nil {
		cred.ShowWeekly = *req.ShowWeekly
	}
	if req.ShowMonthly != nil {
		cred.ShowMonthly = *req.ShowMonthly
	}
	if req.RefreshIntervalSec > 0 {
		cred.RefreshIntervalSec = opencodegoquota.NormalizeRefreshInterval(req.RefreshIntervalSec)
	}

	now := time.Now().UTC()
	createdAt := auth.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	auth.Provider = opencodegoquota.ProviderType
	auth.Label = cred.Name
	auth.Disabled = !cred.Enabled
	auth.Status = coreauth.StatusActive
	auth.StatusMessage = ""
	if auth.Disabled {
		auth.Status = coreauth.StatusDisabled
		auth.StatusMessage = "disabled via management API"
	}
	auth.Metadata = openCodeGoCredentialMetadata(cred, createdAt, now)
	auth.UpdatedAt = now

	if _, errUpdate := h.authManager.Update(c.Request.Context(), auth); errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", errUpdate)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"credential": h.publicOpenCodeGoCredential(auth),
	})
}

func (h *Handler) DeleteOpenCodeGoCredential(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	auth := h.findOpenCodeGoAuth(id)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OpenCode Go credential not found"})
		return
	}
	if coreauth.IsPluginVirtualAuth(auth) {
		c.JSON(http.StatusConflict, gin.H{"error": errPluginVirtualAuth.Error()})
		return
	}
	ctx := c.Request.Context()
	targetPath := strings.TrimSpace(authAttribute(auth, "path"))
	if targetPath == "" {
		targetPath = strings.TrimSpace(auth.FileName)
	}
	if targetPath != "" {
		if errRemove := os.Remove(targetPath); errRemove != nil && !os.IsNotExist(errRemove) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete auth file: %v", errRemove)})
			return
		}
	}
	if h.tokenStore != nil && targetPath != "" {
		if errDelete := h.tokenStore.Delete(ctx, targetPath); errDelete != nil {
			log.Errorf("opencode go delete: token store cleanup failed for %s: %v", auth.ID, errDelete)
		}
	}
	h.authManager.Remove(ctx, auth.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) TestOpenCodeGoCredential(c *gin.Context) {
	var req openCodeGoCredentialRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	cred, errBuild := openCodeGoCredentialFromCreateRequest(req)
	if errBuild != nil {
		writeOpenCodeGoError(c, errBuild)
		return
	}
	cred.ID = "test"
	cred.Enabled = true
	result, errFetch := h.openCodeGoQuotaClient().Fetch(c.Request.Context(), cred, opencodegoquota.FetchOptions{Force: true})
	if errFetch != nil {
		writeOpenCodeGoError(c, errFetch)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"quota":  result,
	})
}

func (h *Handler) ListOpenCodeGoQuotas(c *gin.Context) {
	h.respondOpenCodeGoQuotas(c, false, nil)
}

func (h *Handler) RefreshOpenCodeGoQuotas(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if c.Request != nil && c.Request.Body != nil {
		_ = c.ShouldBindJSON(&req)
	}
	h.respondOpenCodeGoQuotas(c, true, req.IDs)
}

func (h *Handler) RefreshOpenCodeGoQuota(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	h.respondOpenCodeGoQuotas(c, true, []string{id})
}

func (h *Handler) respondOpenCodeGoQuotas(c *gin.Context, force bool, ids []string) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	credentials := h.listOpenCodeGoCredentials()
	var filter map[string]struct{}
	if len(ids) > 0 {
		filter = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				filter[id] = struct{}{}
			}
		}
		credentials = filterOpenCodeGoCredentials(credentials, filter)
	}
	results := h.fetchOpenCodeGoQuotaResults(c.Request.Context(), credentials, force)
	credentials = h.listOpenCodeGoCredentials()
	if len(filter) > 0 {
		credentials = filterOpenCodeGoCredentials(credentials, filter)
	}
	c.JSON(http.StatusOK, gin.H{
		"credentials": publicOpenCodeGoCredentials(credentials),
		"quotas":      results,
	})
}

func (h *Handler) fetchOpenCodeGoQuotaResults(ctx context.Context, credentials []opencodegoquota.Credential, force bool) []opencodegoquota.QuotaResult {
	results := make([]opencodegoquota.QuotaResult, len(credentials))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i := range credentials {
		i := i
		cred := credentials[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !cred.Enabled {
				results[i] = opencodegoquota.QuotaResult{
					CredentialID:    cred.ID,
					Name:            cred.Name,
					WorkspaceID:     cred.WorkspaceID,
					CookieRenewedAt: cred.CookieRenewedAt,
					FetchedAt:       time.Now().UTC(),
					Error: &opencodegoquota.Error{
						Code:    opencodegoquota.ErrorDashboardFetchFailed,
						Message: "credential is disabled",
					},
				}
				return
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			var cookieRenewedAt time.Time
			result, errFetch := h.openCodeGoQuotaClient().Fetch(ctx, cred, opencodegoquota.FetchOptions{
				Force: force,
				OnCookieRenewed: func(authCookie string, renewedAt time.Time) {
					if h.updateOpenCodeGoAuthCookie(ctx, cred.ID, authCookie, renewedAt) {
						cookieRenewedAt = renewedAt
					}
				},
			})
			if result != nil {
				results[i] = *result
			} else {
				results[i] = opencodegoquota.QuotaResult{
					CredentialID: cred.ID,
					Name:         cred.Name,
					WorkspaceID:  cred.WorkspaceID,
					FetchedAt:    time.Now().UTC(),
				}
			}
			if !cookieRenewedAt.IsZero() {
				results[i].CookieRenewedAt = cookieRenewedAt
			} else if !cred.CookieRenewedAt.IsZero() {
				results[i].CookieRenewedAt = cred.CookieRenewedAt
			}
			if errFetch != nil {
				results[i].Error = openCodeGoTypedError(errFetch)
			}
		}()
	}
	wg.Wait()
	return results
}

func filterOpenCodeGoCredentials(credentials []opencodegoquota.Credential, filter map[string]struct{}) []opencodegoquota.Credential {
	if len(filter) == 0 {
		return credentials
	}
	out := make([]opencodegoquota.Credential, 0, len(credentials))
	for _, cred := range credentials {
		if _, ok := filter[cred.ID]; ok {
			out = append(out, cred)
			continue
		}
		if _, ok := filter[cred.Name]; ok {
			out = append(out, cred)
			continue
		}
	}
	return out
}

func (h *Handler) listOpenCodeGoCredentials() []opencodegoquota.Credential {
	if h == nil || h.authManager == nil {
		return nil
	}
	auths := h.authManager.List()
	out := make([]opencodegoquota.Credential, 0)
	for _, auth := range auths {
		if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), opencodegoquota.ProviderType) {
			continue
		}
		out = append(out, h.openCodeGoCredentialFromAuth(auth))
	}
	return out
}

func (h *Handler) findOpenCodeGoAuth(id string) *coreauth.Auth {
	if h == nil || h.authManager == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if auth, ok := h.authManager.GetByID(id); ok && isOpenCodeGoAuth(auth) {
		return auth
	}
	for _, auth := range h.authManager.List() {
		if !isOpenCodeGoAuth(auth) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(auth.FileName), id) {
			return auth
		}
		if strings.EqualFold(filepath.Base(strings.TrimSpace(authAttribute(auth, "path"))), id) {
			return auth
		}
		if strings.EqualFold(openCodeGoMetadataString(auth.Metadata, "name"), id) {
			return auth
		}
	}
	return nil
}

func isOpenCodeGoAuth(auth *coreauth.Auth) bool {
	return auth != nil && strings.EqualFold(strings.TrimSpace(auth.Provider), opencodegoquota.ProviderType)
}

func openCodeGoCredentialFromCreateRequest(req openCodeGoCredentialRequest) (opencodegoquota.Credential, error) {
	cred := opencodegoquota.Credential{
		Type:               opencodegoquota.ProviderType,
		Name:               strings.TrimSpace(req.Name),
		WorkspaceID:        strings.TrimSpace(req.WorkspaceID),
		AuthCookie:         strings.TrimSpace(req.AuthCookie),
		Enabled:            true,
		ShowRolling:        true,
		ShowWeekly:         true,
		ShowMonthly:        true,
		RefreshIntervalSec: opencodegoquota.NormalizeRefreshInterval(req.RefreshIntervalSec),
	}
	if cred.Name == "" {
		cred.Name = "OpenCode Go"
	}
	if cred.WorkspaceID == "" {
		cred.WorkspaceID = "Default"
	}
	if req.Enabled != nil {
		cred.Enabled = *req.Enabled
	}
	if req.ShowRolling != nil {
		cred.ShowRolling = *req.ShowRolling
	}
	if req.ShowWeekly != nil {
		cred.ShowWeekly = *req.ShowWeekly
	}
	if req.ShowMonthly != nil {
		cred.ShowMonthly = *req.ShowMonthly
	}
	if _, errCookie := opencodegoquota.BuildOpenCodeGoCookieHeader(cred.AuthCookie); errCookie != nil {
		return cred, errCookie
	}
	return cred, nil
}

func (h *Handler) openCodeGoCredentialFromAuth(auth *coreauth.Auth) opencodegoquota.Credential {
	if auth == nil {
		return opencodegoquota.Credential{}
	}
	cred := opencodegoquota.Credential{
		ID:                 auth.ID,
		Type:               opencodegoquota.ProviderType,
		Name:               strings.TrimSpace(openCodeGoMetadataString(auth.Metadata, "name")),
		Enabled:            !auth.Disabled && auth.Status != coreauth.StatusDisabled,
		WorkspaceID:        strings.TrimSpace(openCodeGoMetadataString(auth.Metadata, "workspace_id")),
		AuthCookie:         strings.TrimSpace(openCodeGoMetadataString(auth.Metadata, "auth_cookie")),
		CookieRenewedAt:    openCodeGoMetadataTime(auth.Metadata, "cookie_renewed_at"),
		ShowRolling:        openCodeGoMetadataBool(auth.Metadata, "show_rolling", true),
		ShowWeekly:         openCodeGoMetadataBool(auth.Metadata, "show_weekly", true),
		ShowMonthly:        openCodeGoMetadataBool(auth.Metadata, "show_monthly", true),
		RefreshIntervalSec: opencodegoquota.NormalizeRefreshInterval(openCodeGoMetadataInt(auth.Metadata, "refresh_interval_sec", 60)),
		CreatedAt:          auth.CreatedAt,
		UpdatedAt:          auth.UpdatedAt,
	}
	if cred.Name == "" {
		cred.Name = strings.TrimSpace(auth.Label)
	}
	if cred.Name == "" {
		cred.Name = strings.TrimSpace(auth.FileName)
	}
	if cred.WorkspaceID == "" {
		cred.WorkspaceID = "Default"
	}
	return cred
}

func (h *Handler) publicOpenCodeGoCredential(auth *coreauth.Auth) opencodegoquota.Credential {
	cred := h.openCodeGoCredentialFromAuth(auth)
	return publicOpenCodeGoCredential(cred)
}

func publicOpenCodeGoCredentials(credentials []opencodegoquota.Credential) []opencodegoquota.Credential {
	out := make([]opencodegoquota.Credential, 0, len(credentials))
	for _, cred := range credentials {
		out = append(out, publicOpenCodeGoCredential(cred))
	}
	return out
}

func publicOpenCodeGoCredential(cred opencodegoquota.Credential) opencodegoquota.Credential {
	cred.MaskedAuthCookie = opencodegoquota.MaskOpenCodeGoCookie(cred.AuthCookie)
	cred.AuthCookie = ""
	return cred
}

func openCodeGoCredentialMetadata(cred opencodegoquota.Credential, createdAt, updatedAt time.Time) map[string]any {
	metadata := map[string]any{
		"type":                 opencodegoquota.ProviderType,
		"name":                 strings.TrimSpace(cred.Name),
		"workspace_id":         strings.TrimSpace(cred.WorkspaceID),
		"auth_cookie":          strings.TrimSpace(cred.AuthCookie),
		"disabled":             !cred.Enabled,
		"enabled":              cred.Enabled,
		"show_rolling":         cred.ShowRolling,
		"show_weekly":          cred.ShowWeekly,
		"show_monthly":         cred.ShowMonthly,
		"refresh_interval_sec": opencodegoquota.NormalizeRefreshInterval(cred.RefreshIntervalSec),
		"created_at":           createdAt.UTC().Format(time.RFC3339),
		"updated_at":           updatedAt.UTC().Format(time.RFC3339),
	}
	if !cred.CookieRenewedAt.IsZero() {
		metadata["cookie_renewed_at"] = cred.CookieRenewedAt.UTC().Format(time.RFC3339)
	}
	if metadata["name"] == "" {
		metadata["name"] = "OpenCode Go"
	}
	if metadata["workspace_id"] == "" {
		metadata["workspace_id"] = "Default"
	}
	return metadata
}

func (h *Handler) updateOpenCodeGoAuthCookie(ctx context.Context, credentialID, authCookie string, renewedAt time.Time) bool {
	if h == nil || h.authManager == nil {
		return false
	}
	authCookie = strings.TrimSpace(authCookie)
	if credentialID == "" || authCookie == "" {
		return false
	}
	auth := h.findOpenCodeGoAuth(credentialID)
	if auth == nil || coreauth.IsPluginVirtualAuth(auth) {
		return false
	}
	current := strings.TrimSpace(openCodeGoMetadataString(auth.Metadata, "auth_cookie"))
	currentHeader, errCurrent := opencodegoquota.BuildOpenCodeGoCookieHeader(current)
	newHeader, errNew := opencodegoquota.BuildOpenCodeGoCookieHeader(authCookie)
	if errNew != nil {
		return false
	}
	if errCurrent == nil && currentHeader == newHeader {
		return false
	}
	if renewedAt.IsZero() {
		renewedAt = time.Now().UTC()
	} else {
		renewedAt = renewedAt.UTC()
	}

	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["auth_cookie"] = authCookie
	updated.Metadata["cookie_renewed_at"] = renewedAt.Format(time.RFC3339)
	updated.Metadata["updated_at"] = renewedAt.Format(time.RFC3339)
	updated.UpdatedAt = renewedAt
	if _, errUpdate := h.authManager.Update(ctx, updated); errUpdate != nil {
		log.WithError(errUpdate).WithField("credential_id", credentialID).Warn("failed to persist renewed OpenCode auth cookie")
		return false
	}
	return true
}

func openCodeGoMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func openCodeGoMetadataBool(metadata map[string]any, key string, fallback bool) bool {
	if metadata == nil {
		return fallback
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, errParse := strconv.ParseBool(strings.TrimSpace(typed))
		if errParse == nil {
			return parsed
		}
	}
	return fallback
}

func openCodeGoMetadataInt(metadata map[string]any, key string, fallback int) int {
	if metadata == nil {
		return fallback
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, errParse := typed.Int64(); errParse == nil {
			return int(parsed)
		}
	case string:
		if parsed, errParse := strconv.Atoi(strings.TrimSpace(typed)); errParse == nil {
			return parsed
		}
	}
	return fallback
}

func openCodeGoMetadataTime(metadata map[string]any, key string) time.Time {
	if metadata == nil {
		return time.Time{}
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return time.Time{}
	}
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC()
	case string:
		parsed, errParse := time.Parse(time.RFC3339, strings.TrimSpace(typed))
		if errParse == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func newOpenCodeGoFileName(name string) string {
	base := sanitizeOpenCodeGoFilePart(name)
	if base == "" {
		base = "account"
	}
	return fmt.Sprintf("opencode-go-%s-%s.json", base, randomHex(4))
}

func sanitizeOpenCodeGoFilePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) && r <= unicode.MaxASCII:
			b.WriteRune(r)
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
		if b.Len() >= 32 {
			break
		}
	}
	return strings.Trim(b.String(), "-_")
}

func randomHex(bytesLen int) string {
	if bytesLen <= 0 {
		bytesLen = 4
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func writeOpenCodeGoError(c *gin.Context, err error) {
	quotaErr := openCodeGoTypedError(err)
	status := http.StatusBadRequest
	if quotaErr.Code == opencodegoquota.ErrorDashboardFetchFailed || quotaErr.Code == opencodegoquota.ErrorDashboardParseFailed {
		status = http.StatusBadGateway
	}
	if quotaErr.Code == opencodegoquota.ErrorAuthFailed {
		status = http.StatusUnauthorized
	}
	c.JSON(status, gin.H{
		"error": quotaErr.Message,
		"code":  quotaErr.Code,
	})
}

func openCodeGoTypedError(err error) *opencodegoquota.Error {
	if err == nil {
		return nil
	}
	if quotaErr, ok := err.(*opencodegoquota.Error); ok {
		return &opencodegoquota.Error{Code: quotaErr.Code, Message: quotaErr.Message}
	}
	return &opencodegoquota.Error{
		Code:    opencodegoquota.ErrorDashboardFetchFailed,
		Message: "OpenCode Go quota query failed",
	}
}
