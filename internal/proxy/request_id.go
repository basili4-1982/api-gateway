package proxy

import (
	"fmt"
	"math/rand/v2"
	"net/http"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// generateRequestID возвращает короткий ID для трассировки/логов.
// Не требует криптографической стойкости, поэтому используется быстрый
// math/rand/v2 (без syscall на каждый вызов) вместо crypto/rand.
func generateRequestID() string {
	return fmt.Sprintf("%016x", rand.Uint64())
}
