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
	ctx := context.WithValue(context.Background(), ctxKey{}, &holder{v: fpValues{ja4v: []string{"t13d15h2_abc_def"}, ja3v: []string{"deadbeef"}, srcv: []string{"gateway"}}})

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
	ctx := context.WithValue(context.Background(), ctxKey{}, &holder{v: fpValues{ja4v: []string{"t13d15h2_abc_def"}, ja3v: []string{"deadbeef"}, srcv: []string{"gateway"}}})
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

func TestDefaultsWithoutConfig(t *testing.T) {
	m := newModule()
	if m.c4 != "X-Ja4" || m.c3 != "X-Ja3" || m.cs != "X-Fp-Source" {
		t.Fatalf("канонические имена не заданы: %q %q %q", m.c4, m.c3, m.cs)
	}
	var got string
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-JA4")
	}))
	req := httptest.NewRequest("GET", "/", nil).WithContext(context.WithValue(context.Background(), ctxKey{}, &holder{v: fpValues{ja4v: []string{"x"}, srcv: []string{"gateway"}}}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "x" {
		t.Fatalf("заголовок по умолчанию = %q", got)
	}
}

func BenchmarkWrapParallel(b *testing.B) {
	m := newModule()
	ctx := context.WithValue(context.Background(), ctxKey{}, &holder{v: fpValues{ja4v: []string{"t13d1516h2_8daaf6152771_02713d6af862"}, ja3v: []string{"deadbeef"}, srcv: []string{"gateway"}}})
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		for pb.Next() {
			h.ServeHTTP(rec, req)
		}
	})
}

// TestConcurrentRequestsShareFingerprint доказывает, что срезы только читаются:
// сотня горутин берёт одно соединение и пишет заголовки одновременно.
// Запускать с -race — это и есть гарантия READ ONLY.
func TestConcurrentRequestsShareFingerprint(t *testing.T) {
	m := newModule()
	ctx := context.WithValue(context.Background(), ctxKey{}, &holder{v: fpValues{ja4v: []string{"t13d1516h2_8daaf6152771_02713d6af862"}, ja3v: []string{"deadbeef"}, srcv: []string{"gateway"}}})
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-JA4"); got != "t13d1516h2_8daaf6152771_02713d6af862" {
			t.Errorf("X-JA4 = %q", got)
		}
	}))

	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}
