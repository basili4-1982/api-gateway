package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPprofMux_ServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	pprofMux().ServeHTTP(rec, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
