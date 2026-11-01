package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheLocalTitlePageStartsWithTheAccountsLastReleaseFilters(t *testing.T) {
	s := titlesServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/preferences/release-filters", strings.NewReader(`{"sort":"size","qual":["","1080","",""],"hdr":false,"hideRecordings":true}`))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signedIn(t, s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("saving the filters = %d %s, want 204", rec.Code, rec.Body)
	}
	page := sendAs(t, s, http.MethodGet, "/movie/438631")
	for _, want := range []string{`sort: 'size', qual: ['','1080','',''], hdr: false`, `hideRecordings: true`} {
		if !strings.Contains(page, want) {
			t.Errorf("Dune's local page lacks %q", want)
		}
	}
	signedOut := httptest.NewRequest(http.MethodPost, "/api/preferences/release-filters", strings.NewReader(`{"sort":"size"}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, signedOut)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", rec.Code)
	}
}
