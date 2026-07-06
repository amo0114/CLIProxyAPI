package opencodegoquota

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClientDefaultsHonorEnvProxy(t *testing.T) {
	proxyHit := make(chan *http.Request, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case proxyHit <- r:
		default:
		}
		http.Error(w, "proxy ok", http.StatusOK)
	}))
	defer proxy.Close()

	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	client := NewClient(nil)
	cred := Credential{
		ID:          "id_proxy_default",
		Name:        "proxy-default",
		WorkspaceID: "wrk_proxy_default",
		AuthCookie:  "Fe26.abcdef1234567890",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}
	_, _ = client.Fetch(context.Background(), cred, FetchOptions{Force: true, TTL: time.Minute})

	var seenTargets []string
loop:
	for {
		select {
		case req := <-proxyHit:
			if req != nil {
				seenTargets = append(seenTargets, req.URL.String())
			}
		case <-time.After(200 * time.Millisecond):
			break loop
		}
	}
	if len(seenTargets) == 0 {
		t.Fatal("default client did not route through HTTPS_PROXY")
	}
	for _, target := range seenTargets {
		if !strings.Contains(target, "opencode.ai") {
			t.Fatalf("proxy forwarded URL = %q, want an opencode.ai target", target)
		}
	}
}

func TestNewClientCustomDoerIsPreserved(t *testing.T) {
	var customCalled bool
	custom := roundTripDoer(func(req *http.Request) (*http.Response, error) {
		customCalled = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`rollingUsage:$R[1]={usagePercent:10,resetInSec:30}`)),
			Header:     make(http.Header),
		}, nil
	})

	client := NewClient(custom)
	cred := Credential{
		ID:          "id_custom_doer",
		Name:        "custom-doer",
		WorkspaceID: "wrk_custom_doer",
		AuthCookie:  "Fe26.abcdef1234567890",
		ShowRolling: true,
	}
	_, err := client.Fetch(context.Background(), cred, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !customCalled {
		t.Fatal("custom HTTPDoer was not used; NewClient should not override it")
	}
}

func TestNewClientNoProxyBypassesProxy(t *testing.T) {
	proxyHit := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case proxyHit <- r:
		default:
		}
		http.Error(w, "proxy ok", http.StatusOK)
	}))
	defer proxy.Close()

	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "opencode.ai")

	upstreamHit := make(chan struct{}, 1)
	doer := roundTripDoer(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "opencode.ai") {
			select {
			case upstreamHit <- struct{}{}:
			default:
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`rollingUsage:$R[1]={usagePercent:5,resetInSec:15}` +
						`weeklyUsage:$R[2]={usagePercent:5,resetInSec:15}` +
						`monthlyUsage:$R[3]={usagePercent:5,resetInSec:15}`,
				)),
				Header: make(http.Header),
			}, nil
		}
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: http.ErrServerClosed}
	})

	client := NewClient(doer)
	cred := Credential{
		ID:          "id_no_proxy",
		Name:        "no-proxy",
		WorkspaceID: "wrk_no_proxy",
		AuthCookie:  "Fe26.abcdef1234567890",
		ShowRolling: true,
		ShowWeekly:  true,
		ShowMonthly: true,
	}
	_, err := client.Fetch(context.Background(), cred, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	select {
	case <-upstreamHit:
	case <-proxyHit:
		t.Fatal("proxy was hit despite NO_PROXY=opencode.ai")
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not hit within timeout")
	}
}
