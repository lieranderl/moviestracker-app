package views

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// translated are the calls whose first string argument is looked up in the
// catalog: texts, formats and the one form of counts, and the handlers'
// messages that pages translate.
var translated = regexp.MustCompile(`\b(?:tr|trf|trJS|i18n\.T|i18n\.Tf|trn|i18n\.N|failed|succeeded|patchSetupError)\([^"()]*(?:\([^()]*\)[^"()]*)*"((?:[^"\\]|\\.)*)"`)

// Every text the interface translates has a Russian translation, so a
// Russian page never shows English by accident.
func TestEveryInterfaceTextHasARussianTranslation(t *testing.T) {
	var files []string
	for _, pattern := range []string{"*.templ", "*.go", "../handlers/*.go"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	seen := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_templ.go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file) // #nosec G304 -- this repository's own sources
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range translated.FindAllStringSubmatch(string(src), -1) {
			msg, err := strconv.Unquote(`"` + m[1] + `"`)
			if err != nil {
				t.Errorf("%s: cannot read %q: %v", file, m[1], err)
				continue
			}
			seen++
			if !i18n.Has(i18n.Russian, msg) {
				t.Errorf("%s: no Russian for %q", file, msg)
			}
		}
	}
	if seen < 100 {
		t.Errorf("found %d translated texts; the pattern no longer matches the calls", seen)
	}
	for _, msg := range tableTexts() {
		if msg != "" && !i18n.Has(i18n.Russian, msg) {
			t.Errorf("no Russian for %q", msg)
		}
	}
}

// tableTexts are the texts pages translate from tables rather than from
// literals.
func tableTexts() []string {
	var out []string
	for _, p := range disclaimerPoints {
		out = append(out, p.Title, p.Body, p.Note)
	}
	for _, o := range themeOptions {
		out = append(out, o.Label)
	}
	for _, l := range appLinks {
		out = append(out, l.Label)
	}
	for _, l := range BrowseLists {
		out = append(out, l.Title, l.Subtitle)
	}
	for _, o := range sortOptions {
		out = append(out, o.Label)
	}
	for _, b := range gstreamerBenefits {
		out = append(out, b.Title, b.Body)
	}
	for _, row := range newMediaInfo(&torrserver.ProbeResult{}, 0, nil).Rows {
		out = append(out, row.Label)
	}
	out = append(out, "Open the TV page", "Open the movie page", "Waiting for stream…", "Moviestracker development build", "Official website")
	for _, item := range settingsNav {
		out = append(out, item.Title)
	}
	out = append(out, upstreamTexts...)
	return out
}

// Values TMDB and TorrServer send in English whatever the language, which
// pages translate.
var upstreamTexts = []string{
	"Directed by", "Written by", "Genres", "Release date", "Released", "Runtime", "Status", "Original title", "Created by", "Networks",
	"First aired", "Last aired", "Episodes", "Episode runtime",
	"Rumored", "Planned", "In Production", "Post Production", "Canceled", "Returning Series", "Ended", "Pilot",
	"Trailer", "Teaser", "Clip", "Featurette", "Behind the Scenes", "Bloopers", "Opening Credits",
	"Acting", "Directing", "Writing", "Production", "Sound", "Camera", "Editing", "Art", "Crew",
	"Costume & Make-Up", "Visual Effects", "Lighting", "Creator",
	"Added", "Getting Info", "Preload", "Working", "Closed", "Dropped (in db)", "Dropped",
	"Download", "Install", "Start TorrServer", "Ready",
	"Torrent added", "Torrent getting info", "Torrent preload", "Torrent working", "Torrent closed", "Torrent in db",
}
