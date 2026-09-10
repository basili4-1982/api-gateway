package discovery

import (
	"strconv"
	"strings"
	"time"

	"github.com/basili4-1982/api-gateway/internal/config"
)

// ParseOptions — параметры парсинга labels.
type ParseOptions struct {
	LabelPrefix       string
	ServiceNameLabels []string
	DefaultTimeout    time.Duration
	Network           string
}

// ParseContainers превращает список контейнеров в Result. Контейнеры без
// gateway.enable=true, без валидного порта или без хотя бы одного роутера
// пропускаются.
func ParseContainers(containers []Container, opts ParseOptions) Result {
	var res Result
	seen := make(map[string]bool, len(containers))

	for _, c := range containers {
		if !isEnabled(c.Labels, opts.LabelPrefix) {
			continue
		}
		if opts.Network != "" {
			if _, ok := c.Networks[opts.Network]; !ok {
				continue
			}
		}

		name := targetName(c, opts)
		if name == "" || seen[name] {
			continue
		}

		port, ok := targetPort(c, opts.LabelPrefix)
		if !ok {
			continue
		}

		routers := parseRouters(c.Labels, opts.LabelPrefix)
		if len(routers) == 0 {
			continue
		}

		scheme := labelValue(c.Labels, opts.LabelPrefix, "scheme")
		if scheme == "" {
			scheme = "http"
		}

		timeout := opts.DefaultTimeout
		if v := labelValue(c.Labels, opts.LabelPrefix, "timeout"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				timeout = d
			}
		}

		baseURL := scheme + "://" + name + ":" + strconv.Itoa(port)
		target := config.TargetConfig{
			Name:    name,
			URL:     baseURL,
			Timeout: timeout,
			Weight:  atoi(labelValue(c.Labels, opts.LabelPrefix, "weight")),
		}
		if health := labelValue(c.Labels, opts.LabelPrefix, "health"); health != "" {
			if strings.HasPrefix(health, "http://") || strings.HasPrefix(health, "https://") {
				target.HealthCheck = health
			} else {
				target.HealthCheck = baseURL + health
			}
		}

		seen[name] = true
		res.Targets = append(res.Targets, target)
		for i := range routers {
			routers[i].TargetName = name
			res.Rules = append(res.Rules, routers[i])
		}
	}

	return res
}

func isEnabled(labels map[string]string, prefix string) bool {
	return parseBool(labelValue(labels, prefix, "enable"))
}

func targetName(c Container, opts ParseOptions) string {
	if v := labelValue(c.Labels, opts.LabelPrefix, "name"); v != "" {
		return v
	}
	for _, l := range opts.ServiceNameLabels {
		if v := c.Labels[l]; v != "" {
			return v
		}
	}
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return ""
}

func targetPort(c Container, prefix string) (int, bool) {
	if v := labelValue(c.Labels, prefix, "port"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	}
	ports := make(map[int]struct{})
	for _, p := range c.Ports {
		if p.PrivatePort > 0 {
			ports[p.PrivatePort] = struct{}{}
		}
	}
	if len(ports) == 1 {
		for p := range ports {
			return p, true
		}
	}
	return 0, false
}

// labelValue возвращает значение label "<prefix>.<field>" или "".
func labelValue(labels map[string]string, prefix, field string) string {
	if labels == nil {
		return ""
	}
	return labels[prefix+"."+field]
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func atoi(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

// routerFields — множество полей роутера, допустимых в labels.
var routerFields = map[string]bool{
	"host":             true,
	"path_prefix":      true,
	"methods":          true,
	"strip_path":       true,
	"auth.required":    true,
	"auth.roles":       true,
	"auth.strip_token": true,
	"rate_limit.rps":   true,
	"rate_limit.burst": true,
}

// parseRouters собирает роутеры из labels (короткая форма = роутер "default").
func parseRouters(labels map[string]string, prefix string) []config.RoutingRule {
	shortPrefix := prefix + "."
	fields := make(map[string]string)
	for k, v := range labels {
		if !strings.HasPrefix(k, shortPrefix) {
			continue
		}
		field := strings.TrimPrefix(k, shortPrefix)
		if routerFields[field] {
			fields[field] = v
		}
	}

	rule := buildRule(fields)
	if rule.Host == "" && rule.PathPrefix == "" {
		return nil
	}
	if rule.Host != "" && rule.PathPrefix == "" {
		rule.PathPrefix = "/"
	}
	return []config.RoutingRule{rule}
}

func buildRule(f map[string]string) config.RoutingRule {
	rule := config.RoutingRule{
		Host:       f["host"],
		PathPrefix: f["path_prefix"],
		StripPath:  parseBool(f["strip_path"]),
	}
	if m := f["methods"]; m != "" {
		rule.Methods = splitCSV(m)
	}

	if v, ok := f["auth.required"]; ok {
		required := parseBool(v)
		rule.Auth = ensureAuth(rule.Auth)
		rule.Auth.Required = required
	}
	if v, ok := f["auth.roles"]; ok {
		rule.Auth = ensureAuth(rule.Auth)
		rule.Auth.Roles = splitCSV(v)
	}
	if v, ok := f["auth.strip_token"]; ok {
		strip := parseBool(v)
		rule.Auth = ensureAuth(rule.Auth)
		rule.Auth.StripToken = &strip
	}

	if v, ok := f["rate_limit.rps"]; ok {
		if rps, err := strconv.ParseFloat(v, 64); err == nil {
			burst := 0
			if b, err := strconv.Atoi(f["rate_limit.burst"]); err == nil {
				burst = b
			}
			rule.RateLimit = &config.RateLimitRule{RequestsPerSecond: rps, Burst: burst}
		}
	}

	return rule
}

func ensureAuth(a *config.AuthRule) *config.AuthRule {
	if a == nil {
		return &config.AuthRule{}
	}
	return a
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
