package webui

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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

	"homelab-agent/internal/config"
	"homelab-agent/internal/version"
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
}
type page struct {
	Title, Version, CSRF, Message, Error, Revision string
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
	return &Server{Store: store, State: state, Settings: settings, Connected: connected, Log: log, key: key, templates: t}, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
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
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		if !s.hostAllowed(r.Host) {
			http.Error(w, "Nicht erlaubter Hostname.", http.StatusForbidden)
			return
		}
		if s.Settings.Username != "" {
			user, password, ok := r.BasicAuth()
			u := sha256.Sum256([]byte(user))
			p := sha256.Sum256([]byte(password))
			eu := sha256.Sum256([]byte(s.Settings.Username))
			ep := sha256.Sum256([]byte(s.Settings.Password))
			if !ok || subtle.ConstantTimeCompare(u[:], eu[:])&subtle.ConstantTimeCompare(p[:], ep[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="homelab-agent", charset="UTF-8"`)
				http.Error(w, "Anmeldung erforderlich.", http.StatusUnauthorized)
				return
			}
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "Fremder Ursprung.", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if err != nil || u.Host != r.Host || u.Scheme != scheme {
					http.Error(w, "Fremder Ursprung.", http.StatusForbidden)
					return
				}
			}
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Formular zu groß oder ungültig.", http.StatusBadRequest)
				return
			}
			cookie, err := r.Cookie(s.cookieName())
			token := r.PostForm.Get("csrf")
			if err != nil || !s.validToken(token) || !hmac.Equal([]byte(cookie.Value), []byte(token)) {
				http.Error(w, "CSRF-Prüfung fehlgeschlagen. Seite neu laden.", http.StatusForbidden)
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
	return page{Version: version.Version, CSRF: s.token(w, r), State: s.State.View(), MQTT: connected, ServiceDefault: serviceForm{Type: "http", ExpectedStatus: 200, Timeout: "5s"}}
}
func (s *Server) render(w http.ResponseWriter, name string, p page) {
	var b bytes.Buffer
	if err := s.templates.ExecuteTemplate(&b, name, p); err != nil {
		s.Log.Error("web template failed", "error", err)
		http.Error(w, "Darstellung fehlgeschlagen.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(b.Bytes()); err != nil {
		s.Log.Debug("web response closed", "error", err)
	}
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	p := s.base(w, r)
	p.Title = "Übersicht"
	s.render(w, "overview", p)
}
func (s *Server) form(w http.ResponseWriter, r *http.Request) {
	p := s.base(w, r)
	p.Title = "Einstellungen"
	doc, err := s.Store.Read()
	if err != nil {
		http.Error(w, "Konfiguration konnte nicht gelesen werden. Server-Log prüfen.", 500)
		s.Log.Warn("web config read failed", "error", err)
		return
	}
	p.Revision = doc.Revision
	p.Sections, p.Overrides = fields(doc.Config)
	p.Services, p.ServicesJSON = serviceValues(doc.Config.Modules.Services.Checks)
	p.Containers = containerChoices(doc.Config, p.State.Containers)
	switch r.URL.Query().Get("saved") {
	case "1":
		p.Message = "Gespeichert. Live-Änderungen werden nach dem laufenden Messzyklus übernommen."
	case "reload":
		p.Message = "Reload angefordert. Live-Änderungen werden nach dem laufenden Messzyklus übernommen."
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
		s.failure(w, fmt.Errorf("Konfiguration nicht lesbar"), 500)
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
		s.failure(w, fmt.Errorf("Host-Steuerung oder Bestätigungsregeln geändert: zusätzliche Bestätigung erforderlich"), 400)
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
		s.failure(w, fmt.Errorf("Reload fehlgeschlagen: Konfiguration nicht lesbar oder ungültig"), 400)
		return
	}
	if Dangerous(s.State.View().Active, doc.Config) && r.PostForm.Get("ack_danger") != "yes" {
		s.failure(w, fmt.Errorf("Reload würde Host-Steuerung oder Bestätigungsregeln ändern: zusätzliche Bestätigung erforderlich"), 400)
		return
	}
	s.State.Submit(doc.Config)
	s.Log.Info("web config reload requested", "remote", r.RemoteAddr, "user", s.Settings.Username)
	http.Redirect(w, r, "/config?saved=reload", http.StatusSeeOther)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	doc, err := s.Store.Read()
	if err != nil {
		http.Error(w, "Export fehlgeschlagen.", 500)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config.yaml"`)
	if _, err = w.Write(doc.YAML); err != nil {
		s.Log.Debug("export response closed", "error", err)
	}
}
