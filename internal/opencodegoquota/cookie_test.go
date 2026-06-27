package opencodegoquota

import (
	"testing"
	"time"
)

func TestBuildOpenCodeGoCookieHeader(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "bare token", raw: "Fe26.abcdef", want: "auth=Fe26.abcdef"},
		{name: "cookie prefix", raw: "Cookie: auth=Fe26.abcdef", want: "auth=Fe26.abcdef"},
		{name: "multiple cookies", raw: "foo=bar; auth=Fe26.abcdef ; theme=dark", want: "auth=Fe26.abcdef"},
		{name: "missing auth", raw: "foo=bar", wantErr: true},
		{name: "empty", raw: " ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildOpenCodeGoCookieHeader(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildOpenCodeGoCookieHeader() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("BuildOpenCodeGoCookieHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaskOpenCodeGoCookie(t *testing.T) {
	got := MaskOpenCodeGoCookie("auth=Fe26.abcdef1234567890")
	if got != "Fe26.abcd...7890" {
		t.Fatalf("MaskOpenCodeGoCookie() = %q", got)
	}
	if got := MaskOpenCodeGoCookie("auth=short"); got != "configured" {
		t.Fatalf("short mask = %q, want configured", got)
	}
}

func TestExtractRenewedOpenCodeGoAuthCookie(t *testing.T) {
	now := time.Date(2026, 6, 27, 1, 0, 0, 0, time.UTC)
	renewed, ok := ExtractRenewedOpenCodeGoAuthCookie(
		[]string{"theme=dark; Path=/", "auth=Fe26.new-value; Path=/; Expires=Sat, 27 Jun 2026 02:00:00 GMT; HttpOnly"},
		"auth=Fe26.old-value",
		now,
	)
	if !ok || renewed != "Fe26.new-value" {
		t.Fatalf("renewed = %q ok=%v, want Fe26.new-value true", renewed, ok)
	}

	if renewed, ok := ExtractRenewedOpenCodeGoAuthCookie(
		[]string{"auth=Fe26.old-value; Path=/; Expires=Sat, 27 Jun 2026 02:00:00 GMT"},
		"auth=Fe26.old-value",
		now,
	); ok || renewed != "" {
		t.Fatalf("same cookie renewed = %q ok=%v, want empty false", renewed, ok)
	}

	if renewed, ok := ExtractRenewedOpenCodeGoAuthCookie(
		[]string{"auth=Fe26.expired; Path=/; Expires=Sat, 27 Jun 2026 00:00:00 GMT"},
		"auth=Fe26.old-value",
		now,
	); ok || renewed != "" {
		t.Fatalf("expired cookie renewed = %q ok=%v, want empty false", renewed, ok)
	}
}
