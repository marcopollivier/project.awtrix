package tiktok

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

// fakeTikTok rotates the refresh token on every refresh, the worst case the
// docs allow: the old one stops working.
type fakeTikTok struct {
	mu        sync.Mutex
	refresh   string // refresh token currently accepted
	access    string // access token currently accepted
	refreshes int
	errIn200  bool // answer refresh failures with 200 + "error", as TikTok may
}

func (f *fakeTikTok) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/oauth/token/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("content-type %q", r.Header.Get("Content-Type"))
		}
		r.ParseForm()
		if r.PostForm.Get("client_key") != "ck" || r.PostForm.Get("client_secret") != "cs" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "the code" || r.PostForm.Get("redirect_uri") != "https://example.com/cb" {
				http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
				return
			}
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != f.refresh {
				if f.errIn200 {
					w.Write([]byte(`{"error":"invalid_grant","error_description":"expired"}`))
					return
				}
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		}
		f.refreshes++
		f.access = fmt.Sprintf("act.%d", f.refreshes)
		f.refresh = fmt.Sprintf("rft.%d", f.refreshes)
		fmt.Fprintf(w, `{"access_token":%q,"expires_in":86400,"open_id":"x","refresh_expires_in":31536000,"refresh_token":%q,"scope":"user.info.basic,user.info.stats","token_type":"Bearer"}`, f.access, f.refresh)
	})
	mux.HandleFunc("GET /v2/user/info/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+f.access || r.URL.Query().Get("fields") != "follower_count" {
			http.Error(w, `{"error":{"code":"access_token_invalid"}}`, http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":{"user":{"follower_count":1234}},"error":{"code":"ok","message":""}}`))
	})
	return mux
}

func newSource(url, path string) *Source {
	s := New(config.TikTok{ClientKey: "ck", ClientSecret: "cs", RedirectURI: "https://example.com/cb"}, func(k string) string { return k }, path)
	s.BaseURL = url
	return s
}

func TestFetchRefreshesAndKeepsRotatedToken(t *testing.T) {
	f := &fakeTikTok{refresh: "rft.seed"}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "tiktok_token.json")
	WriteToken(path, Token{Refresh: "rft.seed"})
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	out, err := newSource(srv.URL, path).Fetch(context.Background(), now)
	if err != nil || out.Metrics[0].Value != 1234 || out.Metrics[0].Key != "tiktok_followers" {
		t.Fatalf("fetch: %+v %v", out, err)
	}
	if f.refreshes != 1 {
		t.Fatalf("no access token: should refresh once, did %d", f.refreshes)
	}
	// Within the day, after a restart: the stored access token is reused.
	if _, err := newSource(srv.URL, path).Fetch(context.Background(), now.Add(12*time.Hour)); err != nil || f.refreshes != 1 {
		t.Fatalf("refreshes = %d, %v", f.refreshes, err)
	}
	// Near expiry: refreshes with the rotated token, not the seed.
	if _, err := newSource(srv.URL, path).Fetch(context.Background(), now.Add(23*time.Hour+30*time.Minute)); err != nil || f.refreshes != 2 {
		t.Fatalf("refreshes = %d, %v", f.refreshes, err)
	}
	if tok, _ := ReadToken(path); tok.Refresh != "rft.2" {
		t.Fatalf("rotated refresh token not saved: %q", tok.Refresh)
	}
}

func TestRevokedAccessTokenRefreshesOnce(t *testing.T) {
	f := &fakeTikTok{refresh: "rft.seed", access: "other"}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "t.json")
	now := time.Now()
	WriteToken(path, Token{Access: "act.old", Refresh: "rft.seed", ExpiresAt: now.Add(20 * time.Hour)})
	if _, err := newSource(srv.URL, path).Fetch(context.Background(), now); err != nil || f.refreshes != 1 {
		t.Fatalf("refreshes = %d, %v", f.refreshes, err)
	}
}

func TestInvalidRefreshTokenAlertsOncePerDay(t *testing.T) {
	for _, in200 := range []bool{false, true} {
		f := &fakeTikTok{refresh: "rft.other", errIn200: in200}
		srv := httptest.NewServer(f.handler(t))
		path := filepath.Join(t.TempDir(), "t.json")
		WriteToken(path, Token{Refresh: "rft.seed"})
		s := newSource(srv.URL, path)
		now := time.Now()
		out, err := s.Fetch(context.Background(), now)
		if err != nil || len(out.Notifications) != 1 {
			t.Fatalf("200=%v: first failure should alert: %+v %v", in200, out, err)
		}
		if _, err := s.Fetch(context.Background(), now.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("200=%v: second failure within a day should be a plain error, got %v", in200, err)
		}
		srv.Close()
	}
}

func TestNoTokenFile(t *testing.T) {
	if _, err := newSource("http://unused", filepath.Join(t.TempDir(), "none.json")).Fetch(context.Background(), time.Now()); err == nil {
		t.Fatal("missing token file should fail")
	}
}

func TestLogin(t *testing.T) {
	f := &fakeTikTok{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	cfg := config.TikTok{ClientKey: "ck", ClientSecret: "cs", RedirectURI: "https://example.com/cb"}

	u, _ := url.Parse(AuthURL(cfg.ClientKey, cfg.RedirectURI, "st8"))
	q := u.Query()
	if q.Get("scope") != "user.info.basic,user.info.stats" || q.Get("redirect_uri") != cfg.RedirectURI || q.Get("response_type") != "code" {
		t.Fatalf("auth url: %s", u)
	}
	if _, err := CodeFrom("https://example.com/cb?code=x&state=forged", "st8"); err == nil {
		t.Fatal("a different state must be rejected")
	}
	if _, err := CodeFrom("https://example.com/cb?error=access_denied&state=st8", "st8"); err == nil {
		t.Fatal("a denied login must fail")
	}
	code, err := CodeFrom("https://example.com/cb?code=the+code&scopes=user.info.basic&state=st8", "st8")
	if err != nil || code != "the code" {
		t.Fatalf("code %q %v", code, err)
	}
	now := time.Now()
	tok, err := Exchange(context.Background(), srv.URL, cfg, code, now)
	if err != nil || tok.Refresh != "rft.1" || tok.Access != "act.1" || !tok.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("exchange: %+v %v", tok, err)
	}
}
