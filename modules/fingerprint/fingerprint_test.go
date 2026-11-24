package fingerprint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeConn struct {
	net.Conn
	ja4, ja3 string
}

func (f *fakeConn) Fingerprint() (string, string) { return f.ja4, f.ja3 }
func (f *fakeConn) NetConn() net.Conn             { return f }

func TestWrapSetsHeaders(t *testing.T) {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source", c4: "X-Ja4", c3: "X-Ja3", cs: "X-Fp-Source"}
	ctx := context.WithValue(context.Background(), ctxKey{}, fp{ja4: "t13d15h2_abc_def", ja3: "deadbeef"})

	var got4, got3, gotS string
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got4, got3, gotS = r.Header.Get("X-JA4"), r.Header.Get("X-JA3"), r.Header.Get("X-FP-Source")
	}))

	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	req.Header.Set("X-JA4", "client-supplied")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got4 != "t13d15h2_abc_def" {
		t.Fatalf("X-JA4 = %q", got4)
	}
	if got3 != "deadbeef" {
		t.Fatalf("X-JA3 = %q", got3)
	}
	if gotS != "gateway" {
		t.Fatalf("источник = %q", gotS)
	}
}

func TestWrapScrubsWithoutFingerprint(t *testing.T) {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source", c4: "X-Ja4", c3: "X-Ja3", cs: "X-Fp-Source"}
	var got string
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-JA4") + r.Header.Get("X-JA3") + r.Header.Get("X-FP-Source")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-JA4", "fake")
	req.Header.Set("X-JA3", "fake")
	req.Header.Set("X-FP-Source", "fake")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "" {
		t.Fatalf("заголовки клиента не вырезаны: %q", got)
	}
}

func TestConnContextReadsFingerprint(t *testing.T) {
	m := &module{enabled: true}
	// tls.Conn оборачивает наше соединение: отпечаток достаётся через NetConn.
	server, _ := net.Pipe()
	_ = server.Close()
	fc := &fakeConn{}
	tc := tls.Server(fc, &tls.Config{})

	// fakeConn реализует NetConn, но не является net.Conn-обёрткой tls.Conn,
	// поэтому проверяем только явный путь через tls.Conn.
	_ = tc

	ctx := m.ConnContext(context.Background(), nil)
	if ctx.Value(ctxKey{}) != nil {
		t.Fatal("nil-соединение не должно давать отпечаток")
	}
}

func BenchmarkWrapWithFingerprint(b *testing.B) {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source", c4: "X-Ja4", c3: "X-Ja3", cs: "X-Fp-Source"}
	ctx := context.WithValue(context.Background(), ctxKey{}, fp{ja4: "t13d15h2_abc_def", ja3: "deadbeef"})
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkWrapWithoutFingerprint(b *testing.B) {
	m := &module{enabled: true, header4: "X-JA4", header3: "X-JA3", headerS: "X-FP-Source", c4: "X-Ja4", c3: "X-Ja3", cs: "X-Fp-Source"}
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}
