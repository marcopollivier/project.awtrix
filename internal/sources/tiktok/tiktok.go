// Package tiktok reads followers through TikTok's Display API
// (open.tiktokapis.com, scope user.info.stats).
//
// Access tokens last 24h and refresh tokens a year, but a refresh may hand back
// a new refresh token that replaces the old one. So the hub and the clock each
// log in on their own (`awtrix-hub tiktok login [--hub]`) and never share a
// token: whichever refreshed first would cut the other off. The hub keeps its
// pair in tokenPath.
package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
)

const (
	AuthorizeURL = "https://www.tiktok.com/v2/auth/authorize/"
	Scopes       = "user.info.basic,user.info.stats"
	// refreshBefore renews the access token this long before it expires.
	refreshBefore = time.Hour
)

// ErrInvalid means the refresh token was rejected: only a new login fixes it.
var ErrInvalid = errors.New("tiktok: refresh token invalid or expired")

type Token struct {
	Access    string    `json:"access_token"`
	Refresh   string    `json:"refresh_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Source struct {
	cfg       config.TikTok
	icons     func(string) string
	tokenPath string
	BaseURL   string // https://open.tiktokapis.com
	alertedAt time.Time
}

func New(cfg config.TikTok, icons func(string) string, tokenPath string) *Source {
	return &Source{cfg: cfg, icons: icons, tokenPath: tokenPath, BaseURL: "https://open.tiktokapis.com"}
}

func (s *Source) Name() string                 { return "tiktok" }
func (s *Source) Next(time.Time) time.Duration { return s.cfg.Interval }

func (s *Source) Fetch(ctx context.Context, now time.Time) (sources.Output, error) {
	tok, err := ReadToken(s.tokenPath)
	if err != nil {
		return sources.Output{}, fmt.Errorf("no TikTok token (awtrix-hub tiktok login --hub): %w", err)
	}
	n, err := s.followers(ctx, &tok, now)
	if errors.Is(err, ErrInvalid) {
		// Warn on the clock at most once a day.
		if now.Sub(s.alertedAt) > 24*time.Hour {
			s.alertedAt = now
			return sources.Output{Notifications: []awtrix.Notification{{
				App: awtrix.App{Text: awtrix.Plain("TOKEN TIKTOK INVALIDO"), Icon: s.icons("tiktok"), TextColor: "#FF3333"},
			}}}, nil
		}
	}
	if err != nil {
		return sources.Output{}, err
	}
	return sources.Output{Metrics: []sources.Metric{
		{Key: "tiktok_followers", Label: "TIKTOK", Icon: s.icons("tiktok"), Value: n, Milestones: s.cfg.Milestones},
	}}, nil
}

// followers refreshes the access token when it is about to expire, and once
// more if TikTok rejects the cached one.
func (s *Source) followers(ctx context.Context, tok *Token, now time.Time) (int64, error) {
	fresh := false
	if tok.Access == "" || tok.ExpiresAt.Sub(now) < refreshBefore {
		if err := s.refresh(ctx, tok, now); err != nil {
			return 0, err
		}
		fresh = true
	}
	var resp struct {
		Data struct {
			User struct {
				Followers *int64 `json:"follower_count"`
			} `json:"user"`
		} `json:"data"`
	}
	err := sources.GetJSON(ctx, s.BaseURL+"/v2/user/info/?fields=follower_count", &resp, "Authorization", "Bearer "+tok.Access)
	if he, ok := errors.AsType[*sources.HTTPError](err); ok && he.Status == 401 && !fresh {
		if err := s.refresh(ctx, tok, now); err != nil {
			return 0, err
		}
		err = sources.GetJSON(ctx, s.BaseURL+"/v2/user/info/?fields=follower_count", &resp, "Authorization", "Bearer "+tok.Access)
	}
	if err != nil {
		return 0, err
	}
	if resp.Data.User.Followers == nil {
		return 0, errors.New("follower_count missing (scope user.info.stats granted?)")
	}
	return *resp.Data.User.Followers, nil
}

func (s *Source) refresh(ctx context.Context, tok *Token, now time.Time) error {
	t, err := tokenRequest(ctx, s.BaseURL, url.Values{
		"client_key": {s.cfg.ClientKey}, "client_secret": {s.cfg.ClientSecret},
		"grant_type": {"refresh_token"}, "refresh_token": {tok.Refresh},
	}, now)
	if err != nil {
		return err
	}
	if t.Refresh == "" {
		t.Refresh = tok.Refresh
	}
	*tok = t
	// Best effort: if it is not written, the next fetch refreshes again from
	// the old refresh token, which works until TikTok rotates it.
	_ = WriteToken(s.tokenPath, t)
	return nil
}

// AuthURL is the page where the user authorizes the app; TikTok then sends the
// browser to redirectURI with ?code=…&state=….
func AuthURL(clientKey, redirectURI, state string) string {
	q := url.Values{
		"client_key": {clientKey}, "response_type": {"code"}, "scope": {Scopes},
		"redirect_uri": {redirectURI}, "state": {state},
	}
	return AuthorizeURL + "?" + q.Encode()
}

// CodeFrom reads the authorization code from the URL TikTok redirected to,
// checking state against the one sent in AuthURL.
func CodeFrom(redirected, state string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(redirected))
	if err != nil {
		return "", err
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		// error_type says what is wrong (e.g. redirect_uri, non_sandbox_target).
		return "", fmt.Errorf("tiktok: %s (%s) %s", e, q.Get("error_type"), q.Get("error_description"))
	}
	if q.Get("state") != state {
		return "", errors.New("tiktok: state does not match; start the login again")
	}
	if q.Get("code") == "" {
		return "", errors.New("tiktok: no code in the URL")
	}
	return q.Get("code"), nil
}

// Exchange trades an authorization code for the first token pair.
func Exchange(ctx context.Context, baseURL string, cfg config.TikTok, code string, now time.Time) (Token, error) {
	return tokenRequest(ctx, baseURL, url.Values{
		"client_key": {cfg.ClientKey}, "client_secret": {cfg.ClientSecret},
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cfg.RedirectURI},
	}, now)
}

// tokenRequest posts to /v2/oauth/token/. TikTok may report errors with a 200
// and an "error" field, so both the status and the body are checked.
func tokenRequest(ctx context.Context, baseURL string, form url.Values, now time.Time) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := sources.HTTPClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	_ = json.Unmarshal(b, &r)
	switch {
	case r.Error == "invalid_grant" || resp.StatusCode == 400 || resp.StatusCode == 401:
		return Token{}, ErrInvalid
	case resp.StatusCode != http.StatusOK:
		return Token{}, &sources.HTTPError{Status: resp.StatusCode, Body: r.Error}
	case r.Error != "" || r.AccessToken == "":
		// The body may echo request details; report only the error code.
		return Token{}, fmt.Errorf("tiktok token: %q", r.Error)
	}
	if r.ExpiresIn <= 0 {
		r.ExpiresIn = 86400
	}
	return Token{Access: r.AccessToken, Refresh: r.RefreshToken, ExpiresAt: now.Add(time.Duration(r.ExpiresIn) * time.Second)}, nil
}

func ReadToken(path string) (Token, error) {
	var t Token
	b, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, err
	}
	if t.Refresh == "" {
		return t, errors.New("empty refresh token")
	}
	return t, nil
}

func WriteToken(path string, t Token) error {
	b, _ := json.MarshalIndent(t, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
