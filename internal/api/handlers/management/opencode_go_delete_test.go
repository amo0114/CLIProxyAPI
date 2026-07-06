package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/opencodegoquota"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func setupOpenCodeGoDeleteHandler(t *testing.T, cred opencodegoquota.Credential) (*Handler, *coreauth.Manager, string, string) {
	t.Helper()
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}

	now := time.Now().UTC()
	auth := &coreauth.Auth{
		ID:       cred.ID,
		Provider: opencodegoquota.ProviderType,
		FileName: cred.ID,
		Label:    cred.Name,
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":           filepath.Join(authDir, cred.ID+".json"),
			"source":         filepath.Join(authDir, cred.ID+".json"),
			"source_backend": coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":             opencodegoquota.ProviderType,
			"name":             cred.Name,
			"workspace_id":     cred.WorkspaceID,
			"auth_cookie":      cred.AuthCookie,
			"enabled":          cred.Enabled,
			"show_rolling":     cred.ShowRolling,
			"show_weekly":      cred.ShowWeekly,
			"show_monthly":     cred.ShowMonthly,
			"refresh_interval": cred.RefreshIntervalSec,
			"created_at":       now,
			"updated_at":       now,
		},
	}
	authPath := filepath.Join(authDir, cred.ID+".json")
	if errWrite := os.WriteFile(authPath, []byte(`{"type":"opencode_go","name":"`+cred.Name+`"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write auth file: %v", errWrite)
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("failed to register auth: %v", errRegister)
	}
	return h, manager, authDir, authPath
}

func callDeleteOpenCodeGoCredential(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: id}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/auth-files/opencode-go/x", nil)
	h.DeleteOpenCodeGoCredential(ctx)
	return rec
}

func TestOpenCodeGoDeleteCredential_SucceedsAndRemovesFromList(t *testing.T) {
	cred := opencodegoquota.Credential{
		ID:          "wrk_delete_ok",
		Name:        "Deletable",
		WorkspaceID: "wrk_delete_ok",
		AuthCookie:  "Fe26.abcdef1234567890",
		Enabled:     true,
	}
	h, manager, _, authPath := setupOpenCodeGoDeleteHandler(t, cred)

	rec := callDeleteOpenCodeGoCredential(h, cred.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("delete response = %s, want status=ok", rec.Body.String())
	}
	if _, errStat := os.Stat(authPath); !os.IsNotExist(errStat) {
		t.Fatalf("expected auth file removed, stat err: %v", errStat)
	}
	if _, ok := manager.GetByID(cred.ID); ok {
		t.Fatalf("expected auth removed from manager")
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	h.ListAuthFiles(listCtx)
	if !strings.Contains(listRec.Body.String(), `"files":[]`) && !strings.Contains(listRec.Body.String(), `"files": []`) {
		if strings.Contains(listRec.Body.String(), cred.ID) {
			t.Fatalf("deleted credential still appears in list: %s", listRec.Body.String())
		}
	}
}

func TestOpenCodeGoDeleteCredential_MissingIDReturnsNotFound(t *testing.T) {
	cred := opencodegoquota.Credential{
		ID:          "wrk_delete_exists",
		Name:        "Present",
		WorkspaceID: "wrk_delete_exists",
		AuthCookie:  "Fe26.abcdef1234567890",
		Enabled:     true,
	}
	h, _, _, _ := setupOpenCodeGoDeleteHandler(t, cred)

	rec := callDeleteOpenCodeGoCredential(h, "does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing id status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenCodeGoDeleteCredential_RejectsNonOpenCodeGoAuth(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)
	authDir := t.TempDir()
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}

	authPath := filepath.Join(authDir, "codex-user.json")
	if errWrite := os.WriteFile(authPath, []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write auth file: %v", errWrite)
	}
	other := &coreauth.Auth{
		ID:       "codex-user.json",
		Provider: "codex",
		FileName: "codex-user.json",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":           authPath,
			"source":         authPath,
			"source_backend": coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":  "codex",
			"email": "user@example.com",
		},
	}
	if _, errRegister := manager.Register(context.Background(), other); errRegister != nil {
		t.Fatalf("failed to register codex auth: %v", errRegister)
	}

	rec := callDeleteOpenCodeGoCredential(h, "codex-user.json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete non-opencode-go status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, errStat := os.Stat(authPath); errStat != nil {
		t.Fatalf("non-opencode-go auth file should remain, stat err: %v", errStat)
	}
	if _, ok := manager.GetByID(other.ID); !ok {
		t.Fatalf("non-opencode-go auth should still be registered")
	}
}

func TestOpenCodeGoDeleteCredential_EmptyIDReturnsBadRequest(t *testing.T) {
	cred := opencodegoquota.Credential{
		ID:          "wrk_delete_present",
		Name:        "Present",
		WorkspaceID: "wrk_delete_present",
		AuthCookie:  "Fe26.abcdef1234567890",
		Enabled:     true,
	}
	h, _, _, _ := setupOpenCodeGoDeleteHandler(t, cred)

	rec := callDeleteOpenCodeGoCredential(h, "   ")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete empty id status = %d body=%s", rec.Code, rec.Body.String())
	}
}
