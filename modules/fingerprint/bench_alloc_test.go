package fingerprint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkWrapReuseSlices — тот же путь, но значения заголовков лежат
// готовыми срезами в структуре соединения: ни одной аллокации на запрос.
func BenchmarkWrapReuseSlices(b *testing.B) {
	ja4v := []string{"t13d1516h2_8daaf6152771_02713d6af862"}
	ja3v := []string{"deadbeef"}
	srcv := []string{"gateway"}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	_ = context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		delete(req.Header, "X-Ja4")
		delete(req.Header, "X-Ja3")
		delete(req.Header, "X-Fp-Source")
		req.Header["X-Ja4"] = ja4v
		req.Header["X-Ja3"] = ja3v
		req.Header["X-Fp-Source"] = srcv
		h.ServeHTTP(rec, req)
	}
}

// BenchmarkMapWritesOnly — цена только двух записей в map без аллокации.
func BenchmarkMapWritesOnly(b *testing.B) {
	req := httptest.NewRequest("GET", "/", nil)
	ja4v := []string{"x"}
	ja3v := []string{"y"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Header["X-Ja4"] = ja4v
		req.Header["X-Ja3"] = ja3v
	}
}
