package web_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

const bitrateIn4K = `{"sort":"bitrate","qual":["2160","1080","",""],"hdr":true,"hideRecordings":true}`

func TestATitlePageStartsWithTheUsersLastReleaseFilters(t *testing.T) {
	h, session, _ := favorites(t)
	if res := sendSignals(t, h, http.MethodPost, "/api/preferences/release-filters", bitrateIn4K, session); res.Code != http.StatusNoContent {
		t.Fatalf("saving the filters = %d %s, want 204", res.Code, res.Body)
	}
	page := html.UnescapeString(getWith(t, h, "/movie/438631", session).Body.String())
	for _, want := range []string{`sort: 'bitrate', qual: ['2160','1080','',''], hdr: true`, `hideRecordings: true`, `data-on:change__debounce.1s="@post('/api/preferences/release-filters'`} {
		if !strings.Contains(page, want) {
			t.Errorf("Dune's page lacks %q", want)
		}
	}
}

func TestReleaseFiltersAreSavedOnlyWhenValid(t *testing.T) {
	h, session, _ := favorites(t)
	if res := sendSignals(t, h, http.MethodPost, "/api/preferences/release-filters", `{"sort":"random","qual":[]}`, session); res.Code != http.StatusBadRequest {
		t.Errorf("an unknown sort = %d, want 400", res.Code)
	}
	if res := sendSignals(t, h, http.MethodPost, "/api/preferences/release-filters", `{"sort":"seeders","qual":["360"]}`, session); res.Code != http.StatusBadRequest {
		t.Errorf("an unknown quality = %d, want 400", res.Code)
	}
}
