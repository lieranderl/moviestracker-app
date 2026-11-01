package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestAnAccountKeepsItsOwnListsInTheLocalApp(t *testing.T) {
	s := titlesServer(t)
	if page := sendAs(t, s, http.MethodGet, "/movie/438631"); !strings.Contains(page, "@get('/api/lists/title?type=movie&id=438631')") {
		t.Error("the local movie page does not offer Add to list")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/lists?type=movie&id=438631", strings.NewReader(`{"listName":"Weekend"}`))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signedIn(t, s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("making a list with Dune = %d %s", rec.Code, rec.Body)
	}
	kept, err := store.OpenListFile(filepath.Join(s.store.Dir(), "lists.json"))
	if err != nil {
		t.Fatal(err)
	}
	lists, _ := kept.CustomLists(context.Background(), testUsername)
	if len(lists) != 1 || lists[0].Name != "Weekend" || len(lists[0].Titles) != 1 || lists[0].Titles[0].Title != "Dune" {
		t.Fatalf("kept in the data directory: %+v, want Weekend with Dune", lists)
	}
	page := sendAs(t, s, http.MethodGet, "/lists/c/"+lists[0].ID)
	if !strings.Contains(page, `href="/movie/438631"`) {
		t.Error("the local list page does not show Dune")
	}
	if strings.Contains(page, "/share") {
		t.Error("the local app offers to share a list, which only the web app does")
	}
}

func TestTheLocalListsHintDoesNotOfferSharing(t *testing.T) {
	s := titlesServer(t)
	tabs := sendAs(t, s, http.MethodGet, "/api/lists/tabs?active=want")
	if !strings.Contains(tabs, "Make your own lists") || strings.Contains(tabs, "share") {
		t.Errorf("the local hint should explain lists without sharing:\n%s", tabs)
	}
}
