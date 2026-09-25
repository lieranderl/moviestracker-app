package views_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestPlayedTodayListsEveryPlayAndScrollsPastFive(t *testing.T) {
	var rows []views.DashPlayed
	for i := range 8 {
		rows = append(rows, views.DashPlayed{When: fmt.Sprintf("2%d:00", i), Title: fmt.Sprintf("Film %d", i), Viewer: "admin", Device: "::1", Kind: "HLS"})
	}
	out := render(t, views.DashPlayedCard(rows))
	for _, r := range rows {
		if !strings.Contains(out, r.Title) {
			t.Errorf("Played today leaves out %q", r.Title)
		}
	}
	for _, want := range []string{"overflow-y-auto", "max-h-", ">8<"} { // scrolls, capped, counted
		if !strings.Contains(out, want) {
			t.Errorf("Played today lacks %q:\n%s", want, out)
		}
	}
}
