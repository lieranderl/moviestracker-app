package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestBothPlayersExposeALocalizedPersistentBufferingStatus(t *testing.T) {
	pages := map[string]templ.Component{
		"local": views.TorrServer(testUser, "http://nas:8090", "", testLinks, torrserver.EchoInfo{}, nil, views.GStreamerSetup{}),
		"web":   views.WebTorrServerPage(testUser, nil, nil),
	}
	for name, page := range pages {
		for _, lang := range []struct {
			code    i18n.Lang
			message string
		}{{i18n.English, "Buffering"}, {i18n.Russian, "Буферизация"}} {
			t.Run(name+"/"+string(lang.code), func(t *testing.T) {
				out := html.UnescapeString(renderIn(t, i18n.WithLang(t.Context(), lang.code), page))
				status := regexp.MustCompile(`<[^>]*\bid="player-buffering-status"[^>]*>`).FindString(out)
				for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-atomic="true"`, lang.message} {
					if !strings.Contains(status, want) {
						t.Errorf("buffering status lacks %q: %s", want, status)
					}
				}
				if strings.Contains(status, "data-show=") || strings.Contains(status, " hidden") {
					t.Error("the live region must remain available when its text clears")
				}
			})
		}
	}
}
