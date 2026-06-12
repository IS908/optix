package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IS908/optix/internal/intel"
)

// /intel/ 必须出 HTML（embed 占位或真产物均可）；/intel 301 → /intel/。
func TestIntelSPAServing(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, nil)

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/intel/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("GET /intel/ = %d, body: %.80s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", cc)
	}

	// ServeMux 子树模式自带 /intel → /intel/ 重定向（Go ≤1.24 发 301，Go 1.25+ 发 307）。
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/intel", nil))
	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("GET /intel = %d, want 301/307 redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/intel/" {
		t.Errorf("GET /intel Location = %q, want /intel/", loc)
	}
}

func TestAttachIntelRegistersAPI(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0"}, nil)
	s.AttachIntel(&intel.Handlers{}) // Pulse=nil：state 可用、pulse 503
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/intel/state = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/intel/pulse", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/api/intel/pulse without provider = %d, want 503", rec.Code)
	}
}
