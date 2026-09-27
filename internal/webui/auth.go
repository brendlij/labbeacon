package webui

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"
)

const sessionLifetime = 12 * time.Hour

func (s *Server) sessionCookieName() string {
	return "labbeacon_session_" + strconv.Itoa(s.Settings.Port)
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(s.sessionCookieName())
	if err != nil {
		return false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	expires, ok := s.sessions[c.Value]
	if !ok || !time.Now().Before(expires) {
		delete(s.sessions, c.Value)
		return false
	}
	return true
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.Settings.Username == "" || s.authenticated(r) {
		http.Redirect(w, r, "/config", http.StatusSeeOther)
		return
	}
	p := s.base(w, r)
	p.Title = "Sign in"
	s.render(w, "login", p)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.Settings.Username == "" {
		http.Redirect(w, r, "/config", http.StatusSeeOther)
		return
	}
	u, p := sha256.Sum256([]byte(r.PostForm.Get("username"))), sha256.Sum256([]byte(r.PostForm.Get("password")))
	eu, ep := sha256.Sum256([]byte(s.Settings.Username)), sha256.Sum256([]byte(s.Settings.Password))
	if subtle.ConstantTimeCompare(u[:], eu[:])&subtle.ConstantTimeCompare(p[:], ep[:]) != 1 {
		page := s.base(w, r)
		page.Title, page.Error = "Sign in", "Invalid username or password."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, "login", page)
		return
	}
	now := time.Now()
	token := rand.Text()
	s.sessionMu.Lock()
	for key, expires := range s.sessions {
		if !now.Before(expires) {
			delete(s.sessions, key)
		}
	}
	if c, err := r.Cookie(s.sessionCookieName()); err == nil {
		delete(s.sessions, c.Value)
	}
	// Bound memory even if valid credentials are used to create many sessions.
	if len(s.sessions) >= 1024 {
		s.sessionMu.Unlock()
		http.Error(w, "Too many active sessions. Sign out another session and retry.", http.StatusServiceUnavailable)
		return
	}
	s.sessions[token] = now.Add(sessionLifetime)
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: int(sessionLifetime.Seconds())})
	http.Redirect(w, r, "/config", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.sessionCookieName()); err == nil {
		s.sessionMu.Lock()
		delete(s.sessions, c.Value)
		s.sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
