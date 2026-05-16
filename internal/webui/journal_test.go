package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
)

// TestJournalPageNoTemplateError guards against C1: the shared footer in
// base.html references {{.GeneratedAt.IsZero}} / {{.FromCache}}, and if
// journalPageData were missing those fields the page would render with
// "can't evaluate field GeneratedAt" baked into the response body.
func TestJournalPageNoTemplateError(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer store.Close()

	// Nil broker is fine — the page renders from SQLite reads only.
	journalSvc := server.NewJournalService(nil, store)
	srv := NewWithJournal(Config{}, store, &JournalDeps{
		Service: journalSvc,
		Store:   store,
	})

	req := httptest.NewRequest(http.MethodGet, "/journal", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "template error") {
		t.Errorf("response body contains template error:\n%s", bodyStr)
	}
	// The shared footer in base.html is only emitted when GeneratedAt is non-zero,
	// so its presence confirms the field is wired and the footer block executed.
	if !strings.Contains(bodyStr, "</footer>") {
		t.Errorf("response body missing </footer> — base.html footer block did not render")
	}
	// First-run case: store is empty, sync state is zero — should show
	// the "never synced" hint, not "0.0 hours ago".
	if !strings.Contains(bodyStr, "never been synced") {
		t.Errorf("expected first-run hint, body:\n%s", bodyStr)
	}
	if strings.Contains(bodyStr, "0.0 hours ago") {
		t.Errorf("first-run page rendered nonsense '0.0 hours ago' copy")
	}
}
