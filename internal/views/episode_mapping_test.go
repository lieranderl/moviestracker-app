package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Both apps' file menus hold one episode section, asked for only for a
// known series' video and bound to the file and the opening it answers.
func TestBothFileMenusOfferEpisodeConfirmationForKnownSeriesFiles(t *testing.T) {
	for app, page := range map[string]string{"local": torrPage(t, true), "web": html.UnescapeString(render(t, views.WebTorrServerPage(testUser, nil, nil)))} {
		menu := regexp.MustCompile(`(?s)<dialog id="torr-file-menu".*?</dialog>`).FindString(page)
		section := regexp.MustCompile(`(?s)<div\s+id="episode-mapping".*?></div>`).FindString(menu)
		for _, want := range []string{`data-show="$_file.video && $_file.tv > 0"`, `data-attr:data-file-key="$_file.hash + '-' + $_file.index + '-' + $_mappingEpoch"`} {
			if !strings.Contains(section, want) {
				t.Errorf("%s menu's episode section lacks %s: %q", app, want, section)
			}
		}
		if !strings.Contains(page, `$_mappingEpoch++`) || !strings.Contains(page, `'/api/episodes/mapping?'`) {
			t.Errorf("%s menu does not ask for the opened file's episodes", app)
		}
		// Closing must not bump the epoch: its close event can land after a quick
		// reopen and orphan that open's reply. Every open bumps it instead.
		if strings.Contains(menu, `data-on:close="$fileMenuOpen = false; $_mappingEpoch++"`) {
			t.Errorf("%s menu fences replies on close", app)
		}
	}
}

func TestSeriesFileRowsCarryTheirSeriesAndMoviesDoNot(t *testing.T) {
	files := []torrserver.FileStat{{ID: 1, Path: "Show/episode.mkv", Length: 1 << 30}}
	for _, tc := range []struct {
		data, want string
	}{
		{`{"tmdb":{"id":95396,"type":"tv"}}`, `data-tv="95396"`},
		{`{"tmdb":{"id":603,"type":"movie"}}`, `data-tv="0"`},
		{``, `data-tv="0"`},
	} {
		torrents := []torrserver.Torrent{{Hash: "hash1", Title: "Show", Data: tc.data, FileStats: files}}
		out := html.UnescapeString(render(t, views.TorrServer(testUser, "http://nas:8090", "", testLinks, torrserver.EchoInfo{Version: "1.0"}, torrents, views.GStreamerSetup{})))
		more := regexp.MustCompile(`<button[^>]*data-action="more"[^>]*>`).FindString(out)
		if !strings.Contains(more, tc.want) {
			t.Errorf("%s: file menu button lacks %s: %s", tc.data, tc.want, more)
		}
	}
}

func mappingView() views.EpisodeMappingView {
	return views.EpisodeMappingView{
		Context:        views.EpisodeMappingContext{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1, File: "Show/episode.mkv", TMDBID: 95396, Request: 7},
		Series:         &tmdb.TVDetails{MediaItem: tmdb.MediaItem{ID: 95396, Title: "Example series"}, Seasons: []tmdb.SeasonSummary{{Number: 0, Name: "Specials"}, {Number: 2, Name: "Season 2"}}},
		Season:         &tmdb.Season{ShowID: 95396, Number: 2, Episodes: []tmdb.Episode{{Number: 3, SeasonNumber: 2, Name: "Third"}, {Number: 4, SeasonNumber: 2, Name: "Fourth"}}},
		SelectedSeason: 2, First: 3, Last: 4, Editing: true,
	}
}

func TestTheEpisodeFormOffersLabeledSeasonAndRangeControls(t *testing.T) {
	out := html.UnescapeString(render(t, views.EpisodeMappingPanel(mappingView())))
	for _, want := range []string{
		`<form id="episode-mapping-form"`, `data-on:submit="@post('/api/episodes/mapping', {contentType: 'form'})"`,
		`name="hash" value="aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"`, `name="index" value="1"`, `name="tv" value="95396"`, `name="key" value="7"`,
		`name="season"`, `name="first"`, `name="last"`, "Season", "First episode", "Last episode", "Save",
		`<option value="2" selected>Season 2</option>`, `<option value="4" selected>E04 · Fourth</option>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("episode form lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `name="uid"`) || strings.Contains(out, "Clear") {
		t.Errorf("an unsaved form names an account or offers Clear:\n%s", out)
	}
	v := mappingView()
	v.Saved, v.Editing = &store.EpisodeMapping{Hash: v.Context.Hash, Index: 1, TMDBID: 95396, Season: 2, First: 3, Last: 4}, false
	saved := html.UnescapeString(render(t, views.EpisodeMappingPanel(v)))
	for _, want := range []string{"S02E03–E04", "Edit", "Clear", `@delete('/api/episodes/mapping', {contentType: 'form'})`} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved confirmation lacks %q:\n%s", want, saved)
		}
	}
	if strings.Contains(saved, `name="first"`) {
		t.Error("a saved confirmation shows the range form before Edit")
	}
	ru := renderIn(t, i18n.WithLang(context.Background(), i18n.Russian), views.EpisodeMappingPanel(mappingView()))
	if !strings.Contains(ru, "Первая серия") || !strings.Contains(ru, "Сохранить") {
		t.Errorf("Russian form untranslated:\n%s", ru)
	}
}
