// Package dashboard provides a read-only overview of the api-gateway: a
// secret-free summary of its configuration, the last-known discovery state,
// and live metrics scraped from the gateway's /metrics endpoint.
package dashboard

// Status is a point-in-time, read-only snapshot rendered by the dashboard.
// Each panel is optional: a failed source is recorded in Errors while the
// remaining panels still render.
type Status struct {
	Config    *ConfigSummary    `json:"config,omitempty"`
	Discovery *DiscoverySummary `json:"discovery,omitempty"`
	Metrics   *MetricsSummary   `json:"metrics,omitempty"`
	Errors    []string          `json:"errors"`
}

// TargetView is a secret-free representation of a routing target.
type TargetView struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	PathPrefix  string `json:"path_prefix,omitempty"`
	Weight      int    `json:"weight"`
	Timeout     string `json:"timeout,omitempty"`
	HealthCheck string `json:"health_check,omitempty"`
}

// RuleView is a secret-free representation of a routing rule. Auth and
// RateLimit indicate field presence only; their values are never exposed.
type RuleView struct {
	Host       string   `json:"host,omitempty"`
	PathPrefix string   `json:"path_prefix"`
	TargetName string   `json:"target_name"`
	Methods    []string `json:"methods,omitempty"`
	StripPath  bool     `json:"strip_path"`
	Auth       bool     `json:"auth"`
	RateLimit  bool     `json:"rate_limit"`
}
