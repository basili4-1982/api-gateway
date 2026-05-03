package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/basili4-1982/api-gateway/internal/config"
	"github.com/basili4-1982/api-gateway/internal/jwtutil"
	"github.com/golang-jwt/jwt/v5"
)

type RouteConfig struct {
	Rule      *config.RoutingRule
	Target    *config.TargetConfig
	RateLimit *IPRateLimiter
}

var corsHeaders = map[string]string{
	"Access-Control-Allow-Origin":      "", // Будет заполняться динамически
	"Access-Control-Allow-Methods":     "GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD",
	"Access-Control-Allow-Headers":     "Origin, Content-Type, Accept, Authorization, X-Request-ID, X-User-ID, X-User-Email, X-User-Roles",
	"Access-Control-Expose-Headers":    "X-User-ID, X-User-Email, X-User-Roles, X-Proxy, X-Request-ID, Authorization",
	"Access-Control-Allow-Credentials": "true",
	"Access-Control-Max-Age":           "86400",
}

type circuitState int

const (
	stateClosed   circuitState = iota //正常工作, запросы проходят
	stateOpen                         // цепь разомкнута, запросы падают с 503
	stateHalfOpen                     // пробный запрос
)

// TargetProxy представляет прокси для конкретного таргета
type TargetProxy struct {
	config      *config.TargetConfig
	targetURL   *url.URL
	healthCheck *HealthChecker
	mu          sync.RWMutex
	healthy     bool
	client      *http.Client

	cbState          circuitState
	failureCount     int
	failureThreshold int
	cbLastFailure    time.Time
	cbTimeout        time.Duration // через сколько перейти в half-open
}

const (
	defaultFailureThreshold = 3
	defaultCBTimeout        = 30 * time.Second
)

// MultiProxy основной прокси сервер с поддержкой множественных таргетов
type MultiProxy struct {
	config       *config.Config
	targets      map[string]*TargetProxy
	routeConfigs []RouteConfig
	routeByRule  map[*config.RoutingRule]*RouteConfig
	jwtValidator *jwtutil.JWTValidator
	logger       *zap.Logger
	metrics      *Metrics
	mu           sync.RWMutex
	devMode      bool
	httpServer   *http.Server
	httpsServer  *http.Server
}

// HealthChecker проверяет здоровье таргета
type HealthChecker struct {
	url     string
	period  time.Duration
	timeout time.Duration
	stopCh  chan struct{}
}

// NewMultiProxy создает новый мульти-прокси сервер
func NewMultiProxy(cfg *config.Config, logger *zap.Logger) (*MultiProxy, error) {
	jwtValidator, err := jwtutil.NewJWTValidator(
		cfg.JWT.SecretKey,
		cfg.JWT.Algorithm,
		cfg.JWT.ValidateExp,
		cfg.JWT.ValidateIss,
		cfg.JWT.ExpectedIss,
		cfg.JWT.ValidateAud,
		cfg.JWT.ExpectedAud,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT validator: %w", err)
	}

	mp := &MultiProxy{
		config:       cfg,
		targets:      make(map[string]*TargetProxy),
		routeByRule:  make(map[*config.RoutingRule]*RouteConfig),
		jwtValidator: jwtValidator,
		logger:       logger,
		metrics:      NewMetrics(),
		devMode:      cfg.Env == "dev",
	}

	for _, targetCfg := range cfg.Targets {
		targetProxy, err := mp.createTargetProxy(&targetCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create proxy for target %s: %w", targetCfg.Name, err)
		}
		mp.targets[targetCfg.Name] = targetProxy
	}

	for i := range cfg.Routing.Rules {
		rule := &cfg.Routing.Rules[i]
		rc := RouteConfig{Rule: rule, Target: cfg.GetTargetByName(rule.TargetName)}
		if rule.RateLimit != nil {
			rc.RateLimit = NewIPRateLimiter(rule.RateLimit.RequestsPerSecond, rule.RateLimit.Burst)
		}
		mp.routeConfigs = append(mp.routeConfigs, rc)
		mp.routeByRule[rule] = &mp.routeConfigs[len(mp.routeConfigs)-1]
	}

	return mp, nil
}

// setCORSHeaders устанавливает CORS заголовки
func (mp *MultiProxy) setCORSHeaders(header http.Header, r *http.Request) {
	if !mp.devMode {
		return
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}

	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Methods", corsHeaders["Access-Control-Allow-Methods"])
	header.Set("Access-Control-Allow-Headers", corsHeaders["Access-Control-Allow-Headers"])
	header.Set("Access-Control-Expose-Headers", corsHeaders["Access-Control-Expose-Headers"])
	header.Set("Access-Control-Allow-Credentials", corsHeaders["Access-Control-Allow-Credentials"])
	header.Set("Access-Control-Max-Age", corsHeaders["Access-Control-Max-Age"])
	header.Add("Vary", "Origin")
}

// findRouteConfig возвращает RouteConfig для данного RoutingRule
func (mp *MultiProxy) findRouteConfig(rule *config.RoutingRule) *RouteConfig {
	if rule == nil {
		return nil
	}
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.routeByRule[rule]
}

// createTargetProxy создает прокси для одного таргета
func (mp *MultiProxy) createTargetProxy(targetCfg *config.TargetConfig) (*TargetProxy, error) {
	targetURL, err := url.Parse(targetCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	tp := &TargetProxy{
		config:           targetCfg,
		targetURL:        targetURL,
		healthy:          true,
		cbState:          stateClosed,
		failureThreshold: defaultFailureThreshold,
		cbTimeout:        defaultCBTimeout,
		client: &http.Client{
			Timeout: targetCfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false,
			},
		},
	}

	if targetCfg.HealthCheck != "" && mp.config.HealthCheck {
		tp.healthCheck = &HealthChecker{
			url:     targetCfg.HealthCheck,
			period:  30 * time.Second,
			timeout: 5 * time.Second,
			stopCh:  make(chan struct{}),
		}
		go tp.startHealthCheck(mp.logger, mp)
	}

	return tp, nil
}

// modifyRequest модифицирует запрос перед отправкой
func (mp *MultiProxy) modifyRequest(r *http.Request, targetCfg *config.TargetConfig, rule *config.RoutingRule) error {
	authHeader := r.Header.Get("Authorization")

	authRequired := mp.config.JWT.Required
	stripToken := mp.config.Headers.StripAuthorization
	if rule != nil && rule.Auth != nil {
		authRequired = rule.Auth.Required
		if rule.Auth.StripToken != nil {
			stripToken = *rule.Auth.StripToken
		}
	}

	if authHeader == "" && authRequired {
		return fmt.Errorf("missing authorization token")
	}

	if authHeader != "" {
		claims, err := mp.jwtValidator.ParseAndValidate(authHeader)
		if err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}

		if err := mp.jwtValidator.ValidateClaims(claims); err != nil {
			return fmt.Errorf("invalid token claims: %w", err)
		}

		if rule != nil && rule.Auth != nil && len(rule.Auth.Roles) > 0 {
			if err := mp.checkRoles(claims, rule.Auth.Roles); err != nil {
				return err
			}
		}

		extracted := jwtutil.ExtractClaims(claims, mp.config.JWT.ClaimMappings)
		for claimName, headerName := range mp.config.Headers.ClaimToHeader {
			if val, ok := extracted[claimName]; ok {
				r.Header.Set(headerName, fmt.Sprintf("%v", val))
			}
		}
	}

	for header, value := range mp.config.Headers.AddHeaders {
		r.Header.Set(header, value)
	}

	if stripToken {
		r.Header.Del("Authorization")
	}

	if clientIP := r.Header.Get("X-Forwarded-For"); clientIP == "" {
		r.Header.Set("X-Forwarded-For", r.RemoteAddr)
	}

	return nil
}

func (mp *MultiProxy) checkRoles(claims jwt.MapClaims, requiredRoles []string) error {
	rolesRaw, ok := claims["roles"]
	if !ok {
		return fmt.Errorf("missing roles claim")
	}

	roles, ok := rolesRaw.([]interface{})
	if !ok {
		if roleStr, ok := rolesRaw.(string); ok {
			roles = []interface{}{roleStr}
		} else {
			return fmt.Errorf("invalid roles format")
		}
	}

	roleSet := make(map[string]bool, len(roles))
	for _, r := range roles {
		roleSet[fmt.Sprintf("%v", r)] = true
	}

	for _, required := range requiredRoles {
		if !roleSet[required] {
			return fmt.Errorf("missing required role: %s", required)
		}
	}

	return nil
}

// proxyRequest выполняет проксирование запроса
func (mp *MultiProxy) proxyRequest(w http.ResponseWriter, r *http.Request, target *TargetProxy, remainingPath string) {
	// Создаем URL для целевого сервера
	targetURL := *target.targetURL
	targetURL.Path = remainingPath
	if targetURL.Path == "" {
		targetURL.Path = "/"
	}
	targetURL.RawQuery = r.URL.RawQuery

	mp.logger.Info("Proxying to target",
		zap.String("target_url", targetURL.String()),
		zap.String("method", r.Method),
	)

	// Логируем тело запроса если есть
	var requestBody []byte
	if r.Body != nil {
		requestBody, _ = io.ReadAll(r.Body)
		err := r.Body.Close()
		if err != nil {
			mp.logger.Error("failed to close body", zap.Error(err))
			return
		}
		if len(requestBody) > 0 {
			mp.logger.Debug("Request body",
				zap.String("target", target.config.Name),
				zap.ByteString("body", requestBody),
			)
		}
		// Восстанавливаем тело для дальнейшего использования
		r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	}

	// Создаем новый запрос
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		mp.logger.Error("Failed to create proxy request",
			zap.Error(err),
			zap.String("target", target.config.Name),
		)
		mp.setCORSHeaders(w.Header(), r)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Копируем заголовки
	proxyReq.Header = r.Header.Clone()

	// Выполняем запрос
	mp.logger.Info("Sending request to target",
		zap.String("target", target.config.Name),
		zap.String("url", targetURL.String()),
		zap.String("host", targetURL.Host),
		zap.String("port", targetURL.Port()),
		zap.Any("headers", r.Header),
	)

	resp, err := target.client.Do(proxyReq)
	if err != nil {
		target.recordCall(err)
		mp.setCORSHeaders(w.Header(), r)
		mp.logger.Error("Proxy request failed",
			zap.Error(err),
			zap.String("target", target.config.Name),
		)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			mp.logger.Error("failed to close body", zap.Error(err))
			return
		}
	}(resp.Body)

	mp.logger.Info("Received response from target",
		zap.String("target", target.config.Name),
		zap.Int("status", resp.StatusCode),
	)

	// Circuit breaker: 5xx считается ошибкой
	if resp.StatusCode >= 500 {
		target.recordCall(fmt.Errorf("upstream %d", resp.StatusCode))
	} else {
		target.recordCall(nil)
	}

	// Читаем тело ответа для логирования
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		mp.logger.Error("Failed to read response body",
			zap.Error(err),
			zap.String("target", target.config.Name),
		)
		mp.setCORSHeaders(w.Header(), r)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Логируем ответ
	mp.logger.Info("Target response",
		zap.String("target", target.config.Name),
		zap.Int("status", resp.StatusCode),
		zap.String("status_text", resp.Status),
		zap.Int64("body_size", int64(len(respBody))),
	)

	// Логируем тело ответа (для debug режима)
	if mp.logger.Core().Enabled(zap.DebugLevel) {
		maxLogSize := 1024 // 1KB
		logBody := respBody
		if len(logBody) > maxLogSize {
			logBody = logBody[:maxLogSize] //nolint:staticcheck
		}
	}

	// Копируем заголовки ответа от бэкенда
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Добавляем CORS заголовки
	mp.setCORSHeaders(w.Header(), r)

	// Устанавливаем статус код
	w.WriteHeader(resp.StatusCode)

	// Отправляем тело ответа
	bytesWritten, err := w.Write(respBody)
	if err != nil {
		mp.logger.Error("Failed to write response body",
			zap.Error(err),
			zap.String("target", target.config.Name),
		)
		return
	}

	// Логируем успешную отправку
	mp.logger.Info("Response sent to client",
		zap.String("target", target.config.Name),
		zap.Int("status", resp.StatusCode),
		zap.Int("bytes_written", bytesWritten),
		zap.Any("headers", w.Header()),
	)

	// Форсированно сбрасываем буфер
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		mp.logger.Debug("Response flushed")
	}
}

// Обработчик OPTIONS запросов
func (mp *MultiProxy) handlePreflight(w http.ResponseWriter, r *http.Request) {
	mp.setCORSHeaders(w.Header(), r)
	w.WriteHeader(http.StatusOK)

	mp.logger.Debug("Handled preflight request",
		zap.String("origin", r.Header.Get("Origin")),
		zap.String("method", r.Header.Get("Access-Control-Request-Method")),
	)
}

// ServeHTTP реализует http.Handler
func (mp *MultiProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Request ID
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = generateRequestID()
	}
	r.Header.Set("X-Request-ID", reqID)
	w.Header().Set("X-Request-ID", reqID)

	// Trace ID
	traceID := r.Header.Get("X-Trace-ID")
	if traceID == "" {
		traceID = reqID
	}

	rw := newResponseWriter(w)

	// /metrics endpoint
	if r.URL.Path == "/metrics" {
		mp.metrics.Handler().ServeHTTP(rw, r)
		return
	}

	mp.metrics.IncActiveRequests()
	defer mp.metrics.DecActiveRequests()

	// SPA статика
	if mp.config.Static != nil {
		if handled := mp.serveStatic(rw, r); handled {
			return
		}
	}

	// Логируем входящий запрос
	mp.logger.Info("Incoming request",
		zap.String("request_id", reqID),
		zap.String("trace_id", traceID),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.UserAgent()),
		zap.String("origin", r.Header.Get("Origin")),
	)

	// Обрабатываем OPTIONS запросы
	if r.Method == http.MethodOptions {
		mp.handlePreflight(rw, r)
		mp.metrics.IncRequests(r.Method, r.URL.Path, "200")
		return
	}

	// Находим целевой таргет
	target, rule := mp.config.FindTargetForPath(r.URL.Path, r.Method)
	if target == nil {
		mp.setCORSHeaders(rw.Header(), r)
		mp.metrics.IncRequests(r.Method, r.URL.Path, "404")
		mp.logger.Warn("No target found",
			zap.String("request_id", reqID),
			zap.String("path", r.URL.Path),
			zap.String("method", r.Method),
		)
		http.Error(rw, "No route found", http.StatusNotFound)
		return
	}

	// Получаем прокси для таргета
	mp.mu.RLock()
	targetProxy, exists := mp.targets[target.Name]
	mp.mu.RUnlock()

	if !exists || !targetProxy.isHealthy() {
		mp.setCORSHeaders(rw.Header(), r)
		mp.metrics.IncRequests(r.Method, r.URL.Path, "503")
		mp.logger.Error("Target unavailable",
			zap.String("request_id", reqID),
			zap.String("target", target.Name),
			zap.String("path", r.URL.Path),
		)
		http.Error(rw, "Target unavailable", http.StatusServiceUnavailable)
		return
	}

	// Rate limiting
	rc := mp.findRouteConfig(rule)
	if rc != nil && rc.RateLimit != nil {
		clientIP := getClientIP(r)
		if !rc.RateLimit.Allow(clientIP) {
			mp.setCORSHeaders(rw.Header(), r)
			mp.metrics.IncRateLimitDenial()
			mp.metrics.IncRequests(r.Method, r.URL.Path, "429")
			mp.logger.Warn("Rate limit exceeded",
				zap.String("request_id", reqID),
				zap.String("ip", clientIP),
				zap.String("path", r.URL.Path),
			)
			http.Error(rw, "Too many requests", http.StatusTooManyRequests)
			return
		}
	}

	// Модифицируем путь
	remainingPath := r.URL.Path
	if rule != nil && rule.StripPath {
		remainingPath = strings.TrimPrefix(r.URL.Path, rule.PathPrefix)
		if !strings.HasPrefix(remainingPath, "/") {
			remainingPath = "/" + remainingPath
		}
	}

	// Модифицируем запрос (JWT валидация и заголовки)
	if err := mp.modifyRequest(r, target, rule); err != nil {
		mp.setCORSHeaders(rw.Header(), r)
		mp.metrics.IncRequests(r.Method, r.URL.Path, "401")
		mp.logger.Warn("Request modification failed",
			zap.String("request_id", reqID),
			zap.Error(err),
			zap.String("target", target.Name),
		)
		http.Error(rw, err.Error(), http.StatusUnauthorized)
		return
	}

	// Логируем проксируемый запрос
	mp.logger.Info("Proxying request",
		zap.String("request_id", reqID),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("target", target.Name),
		zap.String("target_url", target.URL),
		zap.String("remaining_path", remainingPath),
	)

	// Выполняем проксирование
	mp.proxyRequest(rw, r, targetProxy, remainingPath)

	mp.metrics.IncRequests(r.Method, r.URL.Path, fmt.Sprintf("%d", rw.statusCode))
	mp.metrics.ObserveDuration(r.Method, r.URL.Path, time.Since(startTime))

	mp.logger.Debug("Request completed",
		zap.String("request_id", reqID),
		zap.String("path", r.URL.Path),
		zap.Int("status", rw.statusCode),
		zap.Duration("duration", time.Since(startTime)),
	)
}

// isHealthy возвращает статус здоровья таргета (учитывая circuit breaker)
func (tp *TargetProxy) isHealthy() bool {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if !tp.healthy {
		return false
	}

	switch tp.cbState {
	case stateClosed:
		return true
	case stateOpen:
		if time.Since(tp.cbLastFailure) > tp.cbTimeout {
			return true // пора пробовать half-open
		}
		return false
	case stateHalfOpen:
		return true // пропускаем ровно один запрос
	}
	return true
}

// setHealthy устанавливает статус здоровья
func (tp *TargetProxy) setHealthy(healthy bool) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.healthy = healthy
}

// recordCall регистрирует результат запроса для circuit breaker
func (tp *TargetProxy) recordCall(err error) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	switch tp.cbState {
	case stateClosed:
		if err != nil {
			tp.failureCount++
			if tp.failureCount >= tp.failureThreshold {
				tp.cbState = stateOpen
				tp.cbLastFailure = time.Now()
			}
		} else {
			tp.failureCount = 0
		}

	case stateHalfOpen:
		if err != nil {
			tp.cbState = stateOpen
			tp.cbLastFailure = time.Now()
		} else {
			tp.cbState = stateClosed
			tp.failureCount = 0
		}

	case stateOpen:
		if err == nil {
			tp.cbState = stateClosed
			tp.failureCount = 0
		}
	}
}

// startHealthCheck запускает проверку здоровья
func (tp *TargetProxy) startHealthCheck(logger *zap.Logger, mp *MultiProxy) {
	ticker := time.NewTicker(tp.healthCheck.period)
	defer ticker.Stop()

	tp.checkHealth(logger, mp)

	for {
		select {
		case <-ticker.C:
			tp.checkHealth(logger, mp)
		case <-tp.healthCheck.stopCh:
			return
		}
	}
}

// checkHealth выполняет проверку здоровья
func (tp *TargetProxy) checkHealth(logger *zap.Logger, mp *MultiProxy) {
	client := &http.Client{
		Timeout: tp.healthCheck.timeout,
	}

	resp, err := client.Get(tp.healthCheck.url)
	if err != nil {
		logger.Warn("Health check failed",
			zap.String("target", tp.config.Name),
			zap.Error(err),
		)
		tp.setHealthy(false)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			zap.L().Error("Error closing body", zap.Error(err))
		}
	}(resp.Body)

	// Читаем тело ответа health check
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Warn("Health check failed")
		return
	}

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	tp.setHealthy(healthy)

	if healthy {
		logger.Debug("Health check successful",
			zap.String("target", tp.config.Name),
			zap.Int("status", resp.StatusCode),
			zap.ByteString("response", body),
		)
	} else {
		logger.Warn("Health check failed",
			zap.String("target", tp.config.Name),
			zap.Int("status", resp.StatusCode),
			zap.ByteString("response", body),
		)
	}
	mp.metrics.SetTargetUp(tp.config.Name, healthy)
}

// Reload перезагружает конфигурацию и обновляет targets/routing
func (mp *MultiProxy) Reload(cfg *config.Config) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	oldTargets := mp.targets
	mp.config = cfg
	mp.devMode = cfg.Env == "dev"

	newTargets := make(map[string]*TargetProxy, len(cfg.Targets))
	for _, targetCfg := range cfg.Targets {
		if old, ok := oldTargets[targetCfg.Name]; ok {
			old.config = &targetCfg
			newTargets[targetCfg.Name] = old
			continue
		}
		tp, err := mp.createTargetProxy(&targetCfg)
		if err != nil {
			return fmt.Errorf("failed to create proxy for target %s: %w", targetCfg.Name, err)
		}
		newTargets[targetCfg.Name] = tp
	}

	for name, old := range oldTargets {
		if _, ok := newTargets[name]; !ok {
			if old.healthCheck != nil {
				close(old.healthCheck.stopCh)
			}
		}
	}

	mp.targets = newTargets
	mp.rebuildRouteConfigs(cfg)

	mp.logger.Info("Configuration reloaded",
		zap.Int("targets", len(newTargets)),
	)
	return nil
}

func (mp *MultiProxy) rebuildRouteConfigs(cfg *config.Config) {
	mp.routeConfigs = nil
	mp.routeByRule = make(map[*config.RoutingRule]*RouteConfig)

	for i := range cfg.Routing.Rules {
		rule := &cfg.Routing.Rules[i]
		rc := RouteConfig{Rule: rule, Target: cfg.GetTargetByName(rule.TargetName)}
		if rule.RateLimit != nil {
			rc.RateLimit = NewIPRateLimiter(rule.RateLimit.RequestsPerSecond, rule.RateLimit.Burst)
		}
		mp.routeConfigs = append(mp.routeConfigs, rc)
		mp.routeByRule[rule] = &mp.routeConfigs[len(mp.routeConfigs)-1]
	}
}

func (mp *MultiProxy) Stop(ctx context.Context) error {
	mp.logger.Info("Stopping multi-proxy server")

	for name, target := range mp.targets {
		if target.healthCheck != nil {
			close(target.healthCheck.stopCh)
		}
		mp.logger.Debug("Stopped target", zap.String("target", name))
	}

	if mp.httpServer != nil {
		if err := mp.httpServer.Shutdown(ctx); err != nil {
			mp.logger.Error("HTTP server shutdown error", zap.Error(err))
		}
	}

	if mp.httpsServer != nil {
		if err := mp.httpsServer.Shutdown(ctx); err != nil {
			mp.logger.Error("HTTPS server shutdown error", zap.Error(err))
		}
	}

	return nil
}

// Start запускает HTTP или HTTPS сервер (с автосертификатами Let's Encrypt)
func (mp *MultiProxy) Start() error {
	if mp.config.TLS != nil && mp.config.TLS.Enabled {
		return mp.startTLS()
	}

	addr := fmt.Sprintf(":%d", mp.config.Server.Port)

	server := &http.Server{
		Addr:         addr,
		Handler:      mp,
		ReadTimeout:  mp.config.Server.ReadTimeout,
		WriteTimeout: mp.config.Server.WriteTimeout,
		IdleTimeout:  mp.config.Server.IdleTimeout,
	}
	mp.httpServer = server

	mp.logger.Info("Starting HTTP proxy server",
		zap.Int("port", mp.config.Server.Port),
		zap.Int("targets", len(mp.targets)),
		zap.Bool("dev_mode", mp.devMode),
	)

	mp.logTargets()

	return server.ListenAndServe()
}

func (mp *MultiProxy) logTargets() {
	for name, target := range mp.targets {
		mp.logger.Info("Registered target",
			zap.String("name", name),
			zap.String("url", target.config.URL),
			zap.Bool("healthy", target.healthy),
		)
	}
}

func (mp *MultiProxy) httpRedirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if mp.config.TLS == nil || !mp.config.TLS.RedirectHTTP {
			mp.ServeHTTP(w, r)
			return
		}
		target := fmt.Sprintf("https://%s%s", r.Host, r.URL.RequestURI())
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// serveStatic пытается отдать SPA статику, возвращает true если запрос обработан
func (mp *MultiProxy) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if mp.config.Static == nil {
		return false
	}

	for _, app := range mp.config.Static.Apps {
		if strings.HasPrefix(r.URL.Path, app.PathPrefix) {
			mp.serveSPA(w, r, &app)
			return true
		}
	}
	return false
}

func (mp *MultiProxy) serveSPA(w http.ResponseWriter, r *http.Request, app *config.StaticApp) {
	fs := http.Dir(app.RootDir)
	path := strings.TrimPrefix(r.URL.Path, app.PathPrefix)
	if path == "" {
		path = "/"
	}

	fullPath := app.RootDir + path
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		path = "/" + app.IndexFile
	}

	mp.logger.Debug("Serving static file",
		zap.String("path", path),
		zap.String("root", app.RootDir),
	)

	maxAge := app.MaxAge
	if maxAge == 0 {
		maxAge = 3600
	}

	handler := http.StripPrefix(app.PathPrefix, http.FileServer(fs))
	handler = cacheControlMiddleware(handler, maxAge)
	handler.ServeHTTP(w, r)
}

func cacheControlMiddleware(next http.Handler, maxAge int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
		next.ServeHTTP(w, r)
	})
}
