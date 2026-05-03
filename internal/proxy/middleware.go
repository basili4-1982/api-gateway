package proxy

import (
	"net/http"
)

// Middleware функция, оборачивающая http.Handler
type Middleware func(next http.Handler) http.Handler

// MiddlewareHandler объединяет handler и middleware
type MiddlewareHandler struct {
	handler    http.Handler
	middleware []Middleware
}

// NewMiddlewareHandler создаёт цепочку middleware
func NewMiddlewareHandler(handler http.Handler, mw ...Middleware) *MiddlewareHandler {
	return &MiddlewareHandler{
		handler:    handler,
		middleware: mw,
	}
}

func (mh *MiddlewareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := mh.handler
	for i := len(mh.middleware) - 1; i >= 0; i-- {
		handler = mh.middleware[i](handler)
	}
	handler.ServeHTTP(w, r)
}
