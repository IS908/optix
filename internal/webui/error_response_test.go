package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteErrorPageSetsContentTypeBeforeStatus pins #162.2: the function used
// to call WriteHeader(code) BEFORE setting Content-Type, so the type-header
// mutation downstream (in renderPage) was a no-op and HTML error pages went out
// without an explicit Content-Type. The fix mirrors writeErrorJSON's ordering.
func TestWriteErrorPageSetsContentTypeBeforeStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorPage(w, "test error", http.StatusInternalServerError)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html...", ct)
	}
	if !strings.Contains(ct, "charset=utf-8") {
		t.Errorf("Content-Type = %q, want charset=utf-8", ct)
	}
}

// TestWriteErrorJSONSetsContentTypeBeforeStatus pins the symmetric behavior of
// the sibling JSON path so a future refactor can't introduce the same ordering
// drift writeErrorPage had.
func TestWriteErrorJSONSetsContentTypeBeforeStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorJSON(w, "boom", http.StatusBadRequest)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
