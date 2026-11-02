package handlers

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

var undoPath = regexp.MustCompile(`/api/titles/undo\?[^"']+`)

// undoFrom is the undo a quick action's answer offers, "" for none.
func undoFrom(body string) string {
	return undoPath.FindString(strings.ReplaceAll(body, `\u0026`, "&"))
}

func keptTitleState(t *testing.T, s *Server) store.TitleState {
	t.Helper()
	kept, err := store.OpenTitleFile(filepath.Join(s.store.Dir(), "titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := kept.TitleState(context.Background(), testUsername, "movie", 438631)
	return got
}

func TestAUserMarksACardWantToWatchAndUndoesIt(t *testing.T) {
	s := titlesServer(t)
	body := sendAs(t, s, http.MethodPost, "/api/titles/quick?type=movie&id=438631&state=want")
	if !strings.Contains(body, "Added to Want to watch") {
		t.Errorf("the quick action answered:\n%s", body)
	}
	if got := keptTitleState(t, s); got.State != store.WantToWatch || got.Title != "Dune" {
		t.Fatalf("after the quick action Dune is %+v, want to watch", got)
	}
	undo := undoFrom(body)
	if undo == "" {
		t.Fatalf("the quick action offers no undo:\n%s", body)
	}
	if body := sendAs(t, s, http.MethodPost, undo); !strings.Contains(body, "Undone") {
		t.Errorf("the undo answered:\n%s", body)
	}
	if got := keptTitleState(t, s); got.State != "" {
		t.Errorf("after the undo Dune is %q, want unmarked", got.State)
	}
}

func TestAnUndoLeavesAChangeMadeElsewhereAlone(t *testing.T) {
	s := titlesServer(t)
	undo := undoFrom(sendAs(t, s, http.MethodPost, "/api/titles/quick?type=movie&id=438631&state=want"))
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=watched")
	if body := sendAs(t, s, http.MethodPost, undo); !strings.Contains(body, "changed elsewhere") {
		t.Errorf("an undo after a change elsewhere answered:\n%s", body)
	}
	if got := keptTitleState(t, s); got.State != store.Watched {
		t.Errorf("the undo overwrote the change made elsewhere: Dune is %q, want watched", got.State)
	}
}

func TestUndoingAWatchedMarkPutsTheTitleBackInWantToWatch(t *testing.T) {
	s := titlesServer(t)
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want")
	marked := keptTitleState(t, s).UpdatedAt
	undo := undoFrom(sendAs(t, s, http.MethodPost, "/api/titles/quick?type=movie&id=438631&state=watched"))
	sendAs(t, s, http.MethodPost, undo)
	if got := keptTitleState(t, s); got.State != store.WantToWatch || !got.UpdatedAt.Equal(marked) || got.Title != "Dune" {
		t.Errorf("after undoing Watched Dune is %+v, want to watch as marked at %v", got, marked)
	}
}

func TestACardActionOnATitleAlreadyInThatStateOffersNoUndo(t *testing.T) {
	s := titlesServer(t)
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=want")
	body := sendAs(t, s, http.MethodPost, "/api/titles/quick?type=movie&id=438631&state=want")
	if !strings.Contains(body, "Already in Want to watch") || undoFrom(body) != "" {
		t.Errorf("a quick action changing nothing answered:\n%s", body)
	}
}
