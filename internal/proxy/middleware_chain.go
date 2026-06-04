package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// contextKey тип для ключей контекста
type contextKey string

const (
	ctxKeyRequestID     contextKey = "request_id"
	ctxKeyTraceID       contextKey = "trace_id"
	ctxKeyTarget        contextKey = "target"
	ctxKeyRule          contextKey = "rule"
	ctxKeyRemainingPath contextKey = "remaining_path"
	ctxKeyTargetProxy   contextKey = "target_proxy"
)

// ──────── Panic Recovery ────────

func recoveryMiddleware(logger *zap.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("Panic recovered",
						zap.Any("panic", rec),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.String("remote_addr", r.RemoteAddr),
						zap.Stack("stack"),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ──────── Request ID ────────

func requestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = generateRequestID()
			}
			r.Header.Set("X-Request-ID", reqID)
			w.Header().Set("X-Request-ID", reqID)

			traceID := r.Header.Get("X-Trace-ID")
			if traceID == "" {
				traceID = reqID
			}
			r.Header.Set("X-Trace-ID", traceID)

			ctx := r.Context()
			ctx = context.WithValue(ctx, ctxKeyRequestID, reqID)
			ctx = context.WithValue(ctx, ctxKeyTraceID, traceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ──────── OpenTelemetry tracing ────────

func tracingMiddleware(tp *TracerProvider) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tp == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Извлекаем propagation context из заголовков (traceparent)
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := tp.Tracer().Start(ctx, spanName)
			defer span.End()

			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.host", r.Host),
				attribute.String("http.user_agent", r.UserAgent()),
			)

			reqID := ctx.Value(ctxKeyRequestID)
			if reqID != nil {
				span.SetAttributes(attribute.String("request_id", reqID.(string)))
			}

			// Пропагируем trace context в заголовки upstream
			otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(r.Header))

			next.ServeHTTP(w, r.WithContext(ctx))

			// Записываем статус ответа после завершения
			if rw, ok := w.(*responseWriter); ok {
				span.SetAttributes(attribute.Int("http.status_code", rw.statusCode))
			}
		})
	}
}

// ──────── Metrics: active requests ────────

func activeRequestMetricsMiddleware(metrics *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.IncActiveRequests()
			defer metrics.DecActiveRequests()
			next.ServeHTTP(w, r)
		})
	}
}

// ──────── Health probes ────────

var (
	GitSHA     string
	BuildTime  string
	BuildRunID string
)

func (mp *MultiProxy) healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		overallStatus := "ok"

		type targetHealth struct {
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		services := make(map[string]targetHealth)

		mp.mu.RLock()
		for name, target := range mp.targets {
			target.mu.RLock()
			h := target.healthy
			target.mu.RUnlock()
			th := targetHealth{Status: "ok"}
			if !h {
				th.Status = "degraded"
				th.Error = "health check failed"
				overallStatus = "degraded"
			}
			services[name] = th
		}
		mp.mu.RUnlock()

		resp := map[string]any{
			"status":   overallStatus,
			"services": services,
			"build": map[string]any{
				"git_sha":      GitSHA,
				"build_time":   BuildTime,
				"build_run_id": BuildRunID,
			},
		}

		body, _ := json.Marshal(resp)
		httpCode := http.StatusOK
		if overallStatus != "ok" {
			httpCode = http.StatusServiceUnavailable
		}
		w.WriteHeader(httpCode)
		w.Write(body)
	})
}

// ──────── Metrics endpoint ────────

func metricsEndpointMiddleware(metrics *Metrics, allowedIPs []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			if len(allowedIPs) > 0 {
				clientIP := getClientIP(r)
				allowed := false
				for _, ip := range allowedIPs {
					if clientIP == ip {
						allowed = true
						break
					}
				}
				if !allowed {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
			metrics.Handler().ServeHTTP(w, r)
		})
	}
}

// ──────── Global rate limiter ────────

func globalRateLimitMiddleware(limiter *rate.Limiter, metrics *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter != nil && !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				metrics.IncRateLimitDenial()
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ──────── SPA static ────────

func spaStaticMiddleware(mp *MultiProxy) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mp.config.Load().Static != nil {
				if handled := mp.serveStatic(w, r); handled {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ──────── CORS preflight ────────

func corsPreflightMiddleware(mp *MultiProxy, metrics *Metrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				mp.setCORSHeaders(w.Header(), r)
				w.WriteHeader(http.StatusOK)
				metrics.IncRequests(r.Method, r.URL.Path, "200")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ──────── Route matching + health + rate limit + auth + proxy ────────

// proxyHandler финальный handler для проксирования запроса
func (mp *MultiProxy) proxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		reqID := r.Context().Value(ctxKeyRequestID).(string)
		traceID := r.Context().Value(ctxKeyTraceID).(string)

		target, rule := mp.config.Load().FindTargetForPath(r.URL.Path, r.Method)

		rw := newResponseWriter(w)

		// Route
		if target == nil {
			mp.setCORSHeaders(rw.Header(), r)
			http.Error(rw, "No route found", http.StatusNotFound)
			mp.metrics.IncRequests(r.Method, r.URL.Path, "404")
			mp.logAccess(reqID, traceID, r, 404, 0, nil)
			return
		}

		// Health
		mp.mu.RLock()
		targetProxy, exists := mp.targets[target.Name]
		mp.mu.RUnlock()

		if !exists || !targetProxy.isHealthy(mp.config.Load().App.CircuitBreaker) {
			mp.setCORSHeaders(rw.Header(), r)
			http.Error(rw, "Target unavailable", http.StatusServiceUnavailable)
			mp.metrics.IncRequests(r.Method, r.URL.Path, "503")
			mp.logAccess(reqID, traceID, r, 503, 0, target)
			return
		}

		// Per-route rate limit
		rc := mp.findRouteConfig(rule)
		if rc != nil && rc.RateLimit != nil {
			clientIP := getClientIP(r)
			if !rc.RateLimit.Allow(clientIP) {
				mp.setCORSHeaders(rw.Header(), r)
				http.Error(rw, "Too many requests", http.StatusTooManyRequests)
				mp.metrics.IncRateLimitDenial()
				mp.metrics.IncRequests(r.Method, r.URL.Path, "429")
				mp.logAccess(reqID, traceID, r, 429, 0, target)
				return
			}
		}

		// Strip path
		remainingPath := r.URL.Path
		if rule != nil && rule.StripPath {
			remainingPath = strings.TrimPrefix(r.URL.Path, rule.PathPrefix)
			if !strings.HasPrefix(remainingPath, "/") {
				remainingPath = "/" + remainingPath
			}
		}

		// JWT + headers
		if err := mp.modifyRequest(r, target, rule); err != nil {
			mp.setCORSHeaders(rw.Header(), r)
			http.Error(rw, err.Error(), http.StatusUnauthorized)
			mp.metrics.IncRequests(r.Method, r.URL.Path, "401")
			mp.logAccess(reqID, traceID, r, 401, 0, target)
			return
		}

		mp.logger.Debug("Proxying request",
			zap.String("request_id", reqID),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("target", target.Name),
			zap.String("remaining_path", remainingPath),
		)

		mp.proxyRequest(rw, r, targetProxy, remainingPath)

		mp.metrics.IncRequests(r.Method, r.URL.Path, fmt.Sprintf("%d", rw.statusCode))
		mp.metrics.ObserveDuration(r.Method, r.URL.Path, time.Since(startTime))

		mp.logAccess(reqID, traceID, r, rw.statusCode, time.Since(startTime), target)
	})
}
