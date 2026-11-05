package views_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"testing"
)

// A browser keeps a stylesheet or script it fetched once; a new build must
// name its files differently, or the page renders with the old ones.
func TestPagesLoadThisBuildsStylesheetAndScripts(t *testing.T) {
	asset := regexp.MustCompile(`(?:href|src)="/static/([^"?]+)(\?v=[^"]*)?"`)
	stylesheets := 0
	for name, page := range allPages() {
		for _, m := range asset.FindAllStringSubmatch(render(t, page), -1) {
			if m[1] == "app.css" {
				stylesheets++
			}
			content, err := os.ReadFile("../../static/" + m[1])
			if err != nil {
				t.Fatalf("%s loads /static/%s: %v", name, m[1], err)
			}
			sum := sha256.Sum256(content)
			if want := "?v=" + hex.EncodeToString(sum[:])[:16]; m[2] != want {
				t.Errorf("%s loads /static/%s%s, want the version %s of this build's file", name, m[1], m[2], want)
			}
		}
	}
	if stylesheets == 0 {
		t.Error("no page loads the stylesheet")
	}
}
