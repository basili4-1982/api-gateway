package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

type bodyCapture struct {
	buf         []byte
	limit       int
	contentType string
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
	capture    *bodyCapture
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

// enableCapture включает запись тела ответа (с ограничением limit байт)
// и Content-Type ответа. Используется только когда хотя бы один вебхук
// запросил публикацию тела ответа (include_response_body).
func (rw *responseWriter) enableCapture(limit int) {
	if limit <= 0 {
		limit = defaultMaxResponseBodyBytes
	}
	rw.capture = &bodyCapture{limit: limit}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		if rw.capture != nil {
			rw.capture.contentType = rw.Header().Get("Content-Type")
		}
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	if rw.capture != nil {
		if len(rw.capture.buf) < rw.capture.limit {
			need := rw.capture.limit - len(rw.capture.buf)
			if len(b) < need {
				need = len(b)
			}
			rw.capture.buf = append(rw.capture.buf, b[:need]...)
		}
	}
	return rw.ResponseWriter.Write(b)
}

// CapturedResponseBody возвращает захваченное тело ответа (nil, если сбор не
// включён или тело пустое).
func (rw *responseWriter) CapturedResponseBody() []byte {
	if rw.capture == nil || len(rw.capture.buf) == 0 {
		return nil
	}
	// Точный срез: не возвращаем переиспользуемую память вне контекста запроса.
	return bytes.Clone(rw.capture.buf)
}

// CapturedContentType возвращает Content-Type ответа.
func (rw *responseWriter) CapturedContentType() string {
	if rw.capture == nil {
		return ""
	}
	return rw.capture.contentType
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
