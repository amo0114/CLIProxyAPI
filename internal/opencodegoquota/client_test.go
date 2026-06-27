package opencodegoquota

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripDoer func(*http.Request) (*http.Response, error)

func (f roundTripDoer) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientFetchSuccessAndCache(t *testing.T) {
	calls := 0
	client := NewClient(roundTripDoer(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("Cookie") != "auth=Fe26.abcdef" {
			t.Fatalf("Cookie header = %q", req.Header.Get("Cookie"))
		}
		if !strings.Contains(req.URL.String(), "/workspace/wrk_abc/go") {
			t.Fatalf("URL = %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}` +
					`weeklyUsage:$R[2]={usagePercent:30,resetInSec:120}` +
					`monthlyUsage:$R[3]={usagePercent:40,resetInSec:180}</script>`,
			)),
			Header: make(http.Header),
		}, nil
	}))

	cred := Credential{
		ID:                 "id1",
		Name:               "account",
		WorkspaceID:        "https://opencode.ai/workspace/wrk_abc/go",
		AuthCookie:         "Fe26.abcdef",
		ShowRolling:        true,
		ShowWeekly:         true,
		ShowMonthly:        true,
		RefreshIntervalSec: 60,
	}
	first, err := client.Fetch(context.Background(), cred, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if first.Rolling == nil || first.Rolling.UsagePercent != 20 {
		t.Fatalf("unexpected first result = %+v", first)
	}
	second, err := client.Fetch(context.Background(), cred, FetchOptions{})
	if err != nil {
		t.Fatalf("cached Fetch() error = %v", err)
	}
	if !second.Cached {
		t.Fatal("expected cached result")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestClientFetchRedirectIsAuthError(t *testing.T) {
	client := NewClient(roundTripDoer(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"/sign-in"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}))

	_, err := client.Fetch(context.Background(), Credential{
		ID:          "id1",
		WorkspaceID: "wrk_abc",
		AuthCookie:  "Fe26.abcdef",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}, FetchOptions{TTL: time.Minute})
	if err == nil {
		t.Fatal("expected error")
	}
	quotaErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if quotaErr.Code != ErrorAuthFailed {
		t.Fatalf("error code = %s, want %s", quotaErr.Code, ErrorAuthFailed)
	}
}

func TestClientFetchDoesNotRenewCookieOnFailedFetch(t *testing.T) {
	client := NewClient(roundTripDoer(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header: http.Header{
				"Set-Cookie": []string{"auth=Fe26.renewed; Path=/; Expires=Sat, 27 Jun 2099 02:00:00 GMT; HttpOnly"},
			},
			Body: io.NopCloser(strings.NewReader("failed")),
		}, nil
	}))

	var renewed bool
	_, err := client.Fetch(context.Background(), Credential{
		ID:          "id1",
		WorkspaceID: "wrk_abc",
		AuthCookie:  "Fe26.abcdef",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}, FetchOptions{
		Force: true,
		OnCookieRenewed: func(string, time.Time) {
			renewed = true
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if renewed {
		t.Fatal("cookie renewal callback ran after failed fetch")
	}
}

func TestClientFetchResolvesDefaultWorkspace(t *testing.T) {
	var calls []string
	var dashboardCookie string
	client := NewClient(roundTripDoer(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.Path)
		if req.URL.Path == "/_server" {
			if req.Header.Get("Cookie") != "auth=Fe26.abcdef" {
				t.Fatalf("workspace Cookie header = %q", req.Header.Get("Cookie"))
			}
			if req.Method != http.MethodGet {
				t.Fatalf("workspace method = %s, want GET", req.Method)
			}
			if req.URL.Query().Get("id") != workspacesID {
				t.Fatalf("workspace id query = %q", req.URL.Query().Get("id"))
			}
			if req.Header.Get("X-Server-Id") != workspacesID {
				t.Fatalf("X-Server-Id = %q", req.Header.Get("X-Server-Id"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`1:[{id:"wrk_first",name:"Personal"}]`)),
				Header: http.Header{
					"Set-Cookie": []string{"auth=Fe26.renewed; Path=/; Expires=Sat, 27 Jun 2099 02:00:00 GMT; HttpOnly"},
				},
			}, nil
		}
		dashboardCookie = req.Header.Get("Cookie")
		if !strings.Contains(req.URL.String(), "/workspace/wrk_first/go") {
			t.Fatalf("dashboard URL = %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}` +
					`weeklyUsage:$R[2]={usagePercent:30,resetInSec:120}` +
					`monthlyUsage:$R[3]={usagePercent:40,resetInSec:180}</script>`,
			)),
			Header: make(http.Header),
		}, nil
	}))

	var renewedCookie string
	result, err := client.Fetch(context.Background(), Credential{
		ID:          "id1",
		Name:        "account",
		WorkspaceID: "Default",
		AuthCookie:  "Fe26.abcdef",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}, FetchOptions{
		Force: true,
		OnCookieRenewed: func(authCookie string, _ time.Time) {
			renewedCookie = authCookie
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.WorkspaceID != "wrk_first" {
		t.Fatalf("WorkspaceID = %q, want wrk_first", result.WorkspaceID)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want workspace + dashboard", calls)
	}
	if renewedCookie != "Fe26.renewed" {
		t.Fatalf("renewedCookie = %q, want Fe26.renewed", renewedCookie)
	}
	if dashboardCookie != "auth=Fe26.renewed" {
		t.Fatalf("dashboard Cookie header = %q, want renewed cookie", dashboardCookie)
	}
}

func TestClientFetchWorkspacePostFallback(t *testing.T) {
	var sawPost bool
	client := NewClient(roundTripDoer(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/_server" && req.Method == http.MethodGet {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`null`)),
				Header:     make(http.Header),
			}, nil
		}
		if req.URL.Path == "/_server" && req.Method == http.MethodPost {
			sawPost = true
			body, errRead := io.ReadAll(req.Body)
			if errRead != nil {
				t.Fatalf("read POST body: %v", errRead)
			}
			if string(body) != "[]" {
				t.Fatalf("POST body = %q, want []", body)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"workspaces":[{"id":"wrk_team","name":"Team"}]}`)),
				Header:     make(http.Header),
			}, nil
		}
		if !strings.Contains(req.URL.String(), "/workspace/wrk_team/go") {
			t.Fatalf("dashboard URL = %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`<script>rollingUsage:$R[1]={usagePercent:20,resetInSec:60}` +
					`weeklyUsage:$R[2]={usagePercent:30,resetInSec:120}</script>`,
			)),
			Header: make(http.Header),
		}, nil
	}))

	result, err := client.Fetch(context.Background(), Credential{
		ID:          "id1",
		Name:        "account",
		WorkspaceID: "Team",
		AuthCookie:  "Fe26.abcdef",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !sawPost {
		t.Fatal("expected POST fallback")
	}
	if result.WorkspaceID != "wrk_team" {
		t.Fatalf("WorkspaceID = %q, want wrk_team", result.WorkspaceID)
	}
}
