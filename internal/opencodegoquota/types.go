package opencodegoquota

import "time"

const (
	ProviderType = "opencode_go"

	defaultRefreshIntervalSec = 60
	minRefreshIntervalSec     = 15
	failedRefreshIntervalSec  = 15
)

type ErrorCode string

const (
	ErrorAuthCookieEmpty       ErrorCode = "opencode_go_auth_cookie_empty"
	ErrorAuthFailed            ErrorCode = "opencode_go_auth_failed"
	ErrorWorkspaceLookupFailed ErrorCode = "opencode_go_workspace_lookup_failed"
	ErrorDashboardFetchFailed  ErrorCode = "opencode_go_dashboard_fetch_failed"
	ErrorDashboardParseFailed  ErrorCode = "opencode_go_dashboard_parse_failed"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Err     error     `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newError(code ErrorCode, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

type Credential struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
	Name               string    `json:"name"`
	Enabled            bool      `json:"enabled"`
	WorkspaceID        string    `json:"workspace_id"`
	AuthCookie         string    `json:"-"`
	MaskedAuthCookie   string    `json:"auth_cookie,omitempty"`
	CookieRenewedAt    time.Time `json:"cookie_renewed_at,omitempty,omitzero"`
	ShowRolling        bool      `json:"show_rolling"`
	ShowWeekly         bool      `json:"show_weekly"`
	ShowMonthly        bool      `json:"show_monthly"`
	RefreshIntervalSec int       `json:"refresh_interval_sec"`
	CreatedAt          time.Time `json:"created_at,omitempty,omitzero"`
	UpdatedAt          time.Time `json:"updated_at,omitempty,omitzero"`
}

type QuotaWindow struct {
	Label            string    `json:"label"`
	UsagePercent     float64   `json:"usage_percent"`
	RemainingPercent float64   `json:"remaining_percent"`
	ResetInSec       int64     `json:"reset_in_sec"`
	ResetAt          time.Time `json:"reset_at,omitempty,omitzero"`
}

type QuotaResult struct {
	CredentialID    string       `json:"credential_id,omitempty"`
	Name            string       `json:"name,omitempty"`
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	Plan            string       `json:"plan,omitempty"`
	CookieRenewedAt time.Time    `json:"cookie_renewed_at,omitempty,omitzero"`
	Rolling         *QuotaWindow `json:"rolling,omitempty"`
	Weekly          *QuotaWindow `json:"weekly,omitempty"`
	Monthly         *QuotaWindow `json:"monthly,omitempty"`
	FetchedAt       time.Time    `json:"fetched_at"`
	Cached          bool         `json:"cached,omitempty"`
	Error           *Error       `json:"error,omitempty"`
}

func NormalizeRefreshInterval(seconds int) int {
	if seconds <= 0 {
		return defaultRefreshIntervalSec
	}
	if seconds < minRefreshIntervalSec {
		return minRefreshIntervalSec
	}
	return seconds
}
