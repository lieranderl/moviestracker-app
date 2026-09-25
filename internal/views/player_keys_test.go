package views_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tailwind misses a class named only in data-class:<name>, which then does
// nothing, as cursor-none did: the pointer stayed over fullscreen video.
func TestEveryClassToggledByDataClassIsCompiled(t *testing.T) {
	css, err := os.ReadFile("../../static/app.css")
	if err != nil {
		t.Skip("static/app.css is not built: ", err)
	}
	templates, _ := filepath.Glob("*.templ")
	for _, f := range templates {
		src, _ := os.ReadFile(f) // #nosec G304 -- this package's own templates
		for _, m := range regexp.MustCompile(`data-class:([a-z0-9-]+)`).FindAllStringSubmatch(string(src), -1) {
			if !regexp.MustCompile(`\.` + regexp.QuoteMeta(m[1]) + `[{:,. \[]`).Match(css) {
				t.Errorf("%s toggles %q, which the compiled CSS lacks", f, m[1])
			}
		}
	}
}

// Space, ← and → work wherever focus is while the player is open: it opens
// with focus on a header button, and a clicked control keeps it.
func TestPlayerKeysWorkWhereverFocusIsWhileItIsOpen(t *testing.T) {
	out := torrPage(t, true)
	start, end := strings.Index(out, "data-ref:_player"), strings.Index(out, `<video`)
	if start < 0 || end < start {
		t.Fatal("no player")
	}
	player := out[start:end]
	keys := regexp.MustCompile(`data-on:keydown__window="([^"]*)"`).FindStringSubmatch(player)
	if keys == nil {
		t.Fatalf("player keys are not listened for on the window:\n%s", player)
	}
	for _, want := range []string{"$playerOpen", "ArrowLeft", "ArrowRight", "' '", "preventDefault"} {
		if !strings.Contains(keys[1], want) {
			t.Errorf("player keys lack %q:\n%s", want, keys[1])
		}
	}
	if strings.Contains(keys[1], "button") {
		t.Error("keys should still work after a player button was clicked")
	}
}
