package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandler_ServesTheEmbeddedPage is the smoke test for the wiring
// (CLAUDE.md §14.3): it proves the //go:embed actually produced a usable page
// and that it is reachable at /. A build succeeds even if the routing is
// wrong, so this is the check that the page is really served.
func TestHandler_ServesTheEmbeddedPage(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html...", ct)
	}

	body := rec.Body.String()
	// Three things the page cannot work without.
	for _, want := range []string{"<title>", "/api/events", "<tbody id=\"rows\">"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	// The demo runs offline, so an external reference would be a hard failure
	// with no network. This test is what stops one being added by accident.
	for _, forbidden := range []string{"http://", "https://"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page references an external URL (%q): it must work offline", forbidden)
		}
	}
}

func TestHandler_UnknownPathIs404(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
