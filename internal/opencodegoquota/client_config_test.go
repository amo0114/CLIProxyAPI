package opencodegoquota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerFunctionID_DefaultsToConstantWhenUnset(t *testing.T) {
	client := NewClient(nil)
	if got := client.ServerFunctionID(); got != workspacesID {
		t.Fatalf("ServerFunctionID() = %q, want default %q", got, workspacesID)
	}

	serverHits := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case serverHits <- r.URL.Query().Get("id"):
		default:
		}
		select {
		case serverHits <- r.Header.Get("X-Server-Id"):
		default:
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`1:[]`))
	}))
	defer srv.Close()

	doer := roundTripDoer(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`1:[]`)),
			Header:     make(http.Header),
		}, nil
	})
	_ = doer
	_ = serverHits
	_ = context.Background()
}

func TestServerFunctionID_OverrideUsedByFetchServerFunction(t *testing.T) {
	const override = "custom-server-fn-id-12345"

	var seenQueryID, seenHeaderID string
	doer := roundTripDoer(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "_server") {
			seenQueryID = req.URL.Query().Get("id")
			seenHeaderID = req.Header.Get("X-Server-Id")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`1:[{id:"wrk_first",name:"Personal"}]`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}</script>`,
			)),
			Header: make(http.Header),
		}, nil
	})

	client := NewClientWithOptions(doer, override)
	if got := client.ServerFunctionID(); got != override {
		t.Fatalf("ServerFunctionID() = %q, want %q", got, override)
	}

	cred := Credential{
		ID:          "id_override",
		Name:        "override",
		WorkspaceID: "Default",
		AuthCookie:  "Fe26.abcdef1234567890",
		ShowRolling: true,
	}
	if _, err := client.Fetch(context.Background(), cred, FetchOptions{Force: true}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if seenQueryID != override {
		t.Fatalf("server request id query = %q, want %q", seenQueryID, override)
	}
	if seenHeaderID != override {
		t.Fatalf("X-Server-Id = %q, want %q", seenHeaderID, override)
	}
}

func TestServerFunctionID_EmptyOverrideFallsBackToDefault(t *testing.T) {
	for _, emptyValue := range []string{"", "   ", "\t\n"} {
		client := NewClientWithOptions(nil, emptyValue)
		if got := client.ServerFunctionID(); got != workspacesID {
			t.Fatalf("ServerFunctionID() for empty override %q = %q, want default %q", emptyValue, got, workspacesID)
		}
	}

	var seenQueryID, seenHeaderID string
	doer := roundTripDoer(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "_server") {
			seenQueryID = req.URL.Query().Get("id")
			seenHeaderID = req.Header.Get("X-Server-Id")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`1:[{id:"wrk_first",name:"Personal"}]`)),
				Header:     make(http.Header),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}</script>`,
			)),
			Header: make(http.Header),
		}, nil
	})

	client := NewClientWithOptions(doer, "   ")
	cred := Credential{
		ID:          "id_empty_override",
		Name:        "empty-override",
		WorkspaceID: "Default",
		AuthCookie:  "Fe26.abcdef1234567890",
		ShowRolling: true,
	}
	if _, err := client.Fetch(context.Background(), cred, FetchOptions{Force: true}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if seenQueryID != workspacesID {
		t.Fatalf("server request id query = %q, want default %q", seenQueryID, workspacesID)
	}
	if seenHeaderID != workspacesID {
		t.Fatalf("X-Server-Id = %q, want default %q", seenHeaderID, workspacesID)
	}
}
