package webui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginSessionLifecycle(t *testing.T) {
	s, _ := fixture(t)
	values, csrf := fullForm(t, s)
	s.Settings.Username, s.Settings.Password = "admin", "password"
	request := func(method, path string, v url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://localhost")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/login", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `autocomplete="current-password"`) {
		t.Fatal("login form missing")
	}
	v := url.Values{"username": {"admin"}, "password": {"wrong"}, "csrf": {csrf.Value}}
	if w := request("POST", "/login", v); w.Code != 403 {
		t.Fatal("login CSRF bypass")
	}
	if w := request("POST", "/login", v, csrf); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("invalid login accepted or browser prompt")
	}
	v.Set("password", "password")
	w := request("POST", "/login", v, csrf)
	if w.Code != 303 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %s", w.Body.String())
	}
	session := w.Result().Cookies()[0]
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.MaxAge != 43200 {
		t.Fatal("unsafe session cookie")
	}
	for _, path := range []string{"/", "/config", "/config/export"} {
		if w := request("GET", path, nil, session); w.Code != 200 {
			t.Fatalf("authenticated access failed: %s", path)
		}
	}
	if w := request("POST", "/config", values, csrf, session); w.Code != 303 || w.Header().Get("Location") != "/config?saved=1" {
		t.Fatalf("authenticated save failed: %s", w.Body.String())
	}
	if w := request("POST", "/logout", v, session); w.Code != 403 {
		t.Fatal("logout CSRF bypass")
	}
	if w := request("POST", "/logout", v, csrf, session); w.Code != 303 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	if w := request("GET", "/config/export", nil, session); w.Code != 303 {
		t.Fatal("logged out session accepted")
	}
	s.sessions[session.Value] = time.Now().Add(-time.Second)
	if w := request("GET", "/config", nil, session); w.Code != 303 {
		t.Fatal("expired session accepted")
	}
	if _, ok := s.sessions[session.Value]; ok {
		t.Fatal("expired session retained")
	}
}
