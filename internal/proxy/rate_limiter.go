package proxy

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func NewIPRateLimiter(r float64, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(r),
		burst:    burst,
	}
}

func (l *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	l.mu.RLock()
	limiter, exists := l.limiters[ip]
	l.mu.RUnlock()

	if exists {
		return limiter
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	limiter = rate.NewLimiter(l.rate, l.burst)
	l.limiters[ip] = limiter

	return limiter
}

func (l *IPRateLimiter) Allow(ip string) bool {
	return l.GetLimiter(ip).Allow()
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := net.ParseIP(xff); ip != nil {
			return xff
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}
