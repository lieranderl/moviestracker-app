package views_test

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

// A page that could not load its data offers to try again, not only says
// so.
func TestAPageThatCouldNotLoadOffersToTryAgain(t *testing.T) {
	pages := map[string]string{
		"lists":    render(t, views.ListsPage(nil, views.ListWant, nil, "", true)),
		"alerts":   render(t, views.AlertsPage(nil, views.AlertsView{Failed: true})),
		"calendar": render(t, views.CalendarPage(nil, nil, true)),
	}
	for name, page := range pages {
		if !strings.Contains(page, "Try again") || !strings.Contains(page, `href=""`) {
			t.Errorf("the %s page that could not load offers no way to try again", name)
		}
	}
}
