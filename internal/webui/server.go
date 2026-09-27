package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brendlij/labbeacon/internal/config"
	"github.com/brendlij/labbeacon/internal/version"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	Store     *config.Store
	State     *State
	Settings  config.WebUI
	Connected func() bool
	Log       *slog.Logger
	key       []byte
	templates *template.Template
	saveMu    sync.Mutex
	sessionMu sync.Mutex
	sessions  map[string]time.Time
}
type page struct {
	Title, Version, CSRF, Message, Error, Revision string
	Authenticated                                  bool
	State                                          Snapshot
	MQTT                                           bool
	Sections                                       []section
	Services                                       []serviceForm
	ServiceDefault                                 serviceForm
	ServicesJSON                                   string
	Containers                                     []choice
	Overrides                                      []string
}

func New(store *config.Store, state *State, settings config.WebUI, connected func() bool, log *slog.Logger) (*Server, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	t, err := template.New("ui").ParseFS(assets, "assets/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{Store: store, State: state, Settings: settings, Connected: connected, Log: log, key: key, templates: t, sessions: make(map[string]time.Time)}, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.overview)
	mux.HandleFunc("GET /config", s.form)
	mux.HandleFunc("POST /config", s.save)
	mux.HandleFunc("GET /config/export", s.export)
	mux.HandleFunc("POST /config/reload", s.reload)
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(files))))
	return s.secure(mux)
}
func (s *Server) cookieName() string { return "homelab_csrf_" + strconv.Itoa(s.Settings.Port) }
func (s *Server) validToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(parts[0]) != 64 {
		return false
	}
	signature, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(parts[0]))
	return hmac.Equal(signature, mac.Sum(nil))
}
func (s *Server) token(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(s.cookieName()); err == nil && s.validToken(cookie.Value) {
		return cookie.Value
	}
	value := rand.Text()
	sum := sha256.Sum256([]byte(value))
	nonce := hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(nonce))
	token := nonce + "." + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	return token
}
func (s *Server) hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	for _, allowed := range s.Settings.AllowedHosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
	}
	bound := net.ParseIP(s.Settings.BindAddress)
	ip := net.ParseIP(host)
	if bound != nil && bound.IsLoopback() {
		return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	}
	return ip != nil // LAN binding is an explicit administrator choice; DNS names require allowlisting.
}
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// no-referrer makes native form POSTs send Origin: null, which our
		// origin guard must reject. Preserve same-origin submissions without
		// disclosing referrers to other origins.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		if !s.hostAllowed(r.Host) {
			http.Error(w, "Hostname not allowed.", http.StatusForbidden)
			return
		}
		public := r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/assets/")
		if s.Settings.Username != "" && !public && !s.authenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "Cross-origin request denied.", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if err != nil || u.Host != r.Host || u.Scheme != scheme {
					http.Error(w, "Cross-origin request denied.", http.StatusForbidden)
					return
				}
			}
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Form is too large or invalid.", http.StatusBadRequest)
				return
			}
			cookie, err := r.Cookie(s.cookieName())
			token := r.PostForm.Get("csrf")
			if err != nil || !s.validToken(token) || !hmac.Equal([]byte(cookie.Value), []byte(token)) {
				http.Error(w, "CSRF check failed. Reload the page.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) base(w http.ResponseWriter, r *http.Request) page {
	connected := false
	if s.Connected != nil {
		connected = s.Connected()
	}
	return page{Authenticated: s.Settings.Username != "" && s.authenticated(r), Version: version.Version, CSRF: s.token(w, r), State: s.State.View(), MQTT: connected, ServiceDefault: serviceForm{Type: "http", ExpectedStatus: 200, Timeout: "5s"}}
}
func (s *Server) render(w http.ResponseWriter, name string, p page) {
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, name, p); err != nil {
		s.Log.Error("web template failed", "error", err)
		http.Error(w, "Page rendering failed.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(b.Bytes()); err != nil {
		s.Log.Debug("web response closed", "error", err)
	}
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	p := s.base(w, r)
	p.Title = "Overview"
	s.render(w, "overview", p)
}
func (s *Server) form(w http.ResponseWriter, r *http.Request) {
	p := s.base(w, r)
	p.Title = "Settings"
	doc, err := s.Store.Read()
	if err != nil {
		http.Error(w, "Could not read configuration. Check the server log.", 500)
		s.Log.Warn("web config read failed", "error", err)
		return
	}
	p.Revision = doc.Revision
	p.Sections, p.Overrides = fields(doc.Config)
	p.Services, p.ServicesJSON = serviceValues(doc.Config.Modules.Services.Checks)
	p.Containers = containerChoices(doc.Config, p.State.Containers)
	switch r.URL.Query().Get("saved") {
	case "1":
		p.Message = "Saved. Live changes will apply after the current collection cycle."
	case "reload":
		p.Message = "Reload requested. Live changes will apply after the current collection cycle."
	}
	s.render(w, "settings", p)
}
func (s *Server) failure(w http.ResponseWriter, err error, status int) {
	http.Error(w, err.Error(), status)
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	doc, err := s.Store.Read()
	if err != nil {
		s.failure(w, fmt.Errorf("Could not read configuration"), 500)
		return
	}
	if r.PostForm.Get("revision") != doc.Revision {
		s.failure(w, config.ErrConflict, http.StatusConflict)
		return
	}
	next, err := parseForm(doc.Config, r.PostForm)
	if err != nil {
		s.failure(w, err, 400)
		return
	}
	if (Dangerous(doc.Config, next) || Dangerous(s.State.View().Active, next)) && r.PostForm.Get("ack_danger") != "yes" {
		s.failure(w, fmt.Errorf("Host control or confirmation requirements changed: explicit acknowledgment required"), 400)
		return
	}
	saved, err := s.Store.Save(doc.Revision, next)
	if err != nil {
		code := 400
		if errors.Is(err, config.ErrConflict) {
			code = 409
		}
		s.failure(w, err, code)
		return
	}
	s.State.Submit(saved.Config)
	s.Log.Info("web configuration saved", "remote", r.RemoteAddr, "user", s.Settings.Username, "restart_required", RestartFields(s.State.View().Active, saved.Config))
	http.Redirect(w, r, "/config?saved=1", http.StatusSeeOther)
}
func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	doc, err := s.Store.Read()
	if err != nil {
		s.failure(w, fmt.Errorf("Reload failed: configuration is unreadable or invalid"), 400)
		return
	}
	if Dangerous(s.State.View().Active, doc.Config) && r.PostForm.Get("ack_danger") != "yes" {
		s.failure(w, fmt.Errorf("Reload would change host control or confirmation requirements: explicit acknowledgment required"), 400)
		return
	}
	s.State.Submit(doc.Config)
	s.Log.Info("web config reload requested", "remote", r.RemoteAddr, "user", s.Settings.Username)
	http.Redirect(w, r, "/config?saved=reload", http.StatusSeeOther)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	doc, err := s.Store.Read()
	if err != nil {
		http.Error(w, "Export failed.", 500)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	if _, err = w.Write(doc.YAML); err != nil {
		s.Log.Debug("export response closed", "error", err)
	}
}
