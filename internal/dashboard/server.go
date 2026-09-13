package dashboard

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/overview.html
var overviewHTML string

const defaultServerTimeout = 5 * time.Second

// ServerConfig configures the read-only dashboard server.
type ServerConfig struct {
	// ConfigPath is the gateway config file to summarize. Empty disables the panel.
	ConfigPath string
	// DiscoveryState is the gateway discovery state file. Empty disables the panel.
	DiscoveryState string
	// MetricsURL is the gateway /metrics endpoint. Empty disables the panel.
	MetricsURL string
	// Refresh is the HTML auto-refresh interval; <= 0 disables auto-refresh.
	Refresh time.Duration
	// BasicAuth, when set to "user:password", protects every route.
	BasicAuth string
	// Timeout bounds each request's data collection. Defaults to 5s.
	Timeout time.Duration
}

// Server renders the dashboard overview and status API.
type Server struct {
	cfg      ServerConfig
	tmpl     *template.Template
	client   *http.Client
	handler  http.Handler
	user     string
	password string
	hasAuth  bool
}

// NewServer builds a dashboard server. It never fails: a missing or invalid
// data source degrades to an error banner at request time.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultServerTimeout
	}
	s := &Server{
		cfg:    cfg,
		tmpl:   template.Must(template.New("overview").Parse(overviewHTML)),
		client: &http.Client{Timeout: defaultScrapeTimeout},
	}
	if cfg.BasicAuth != "" {
		s.user, s.password, _ = strings.Cut(cfg.BasicAuth, ":")
		s.hasAuth = true
	}
	s.handler = s.routes()
	return s
}

// Handler returns the HTTP handler serving GET /, GET /api/status and GET /healthz.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /", s.handleOverview)
	if s.hasAuth {
		return s.withBasicAuth(mux)
	}
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.collect(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(status)
}

type overviewData struct {
	*Status
	RefreshSeconds int
	GeneratedAt    time.Time
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	status := s.collect(r.Context())
	data := overviewData{
		Status:         status,
		RefreshSeconds: int(s.cfg.Refresh.Seconds()),
		GeneratedAt:    time.Now(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// collect gathers every enabled source. Failures are recorded in Errors; the
// remaining panels still render.
func (s *Server) collect(ctx context.Context) *Status {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	status := &Status{Errors: []string{}}
	if s.cfg.ConfigPath != "" {
		if c, err := LoadConfigSummary(s.cfg.ConfigPath); err != nil {
			status.Errors = append(status.Errors, "config: "+redactCredentials(err.Error()))
		} else {
			status.Config = c
		}
	}
	if s.cfg.DiscoveryState != "" {
		if d, err := LoadDiscoverySummary(s.cfg.DiscoveryState); err != nil {
			status.Errors = append(status.Errors, "discovery: "+redactCredentials(err.Error()))
		} else {
			status.Discovery = d
		}
	}
	if s.cfg.MetricsURL != "" {
		if m, err := scrapeMetrics(ctx, s.client, s.cfg.MetricsURL); err != nil {
			status.Errors = append(status.Errors, "metrics: "+redactCredentials(err.Error()))
		} else {
			status.Metrics = m
		}
	}
	return status
}

func (s *Server) withBasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.password)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="api-gateway-dashboard", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
