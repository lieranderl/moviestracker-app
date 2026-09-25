package views_test

import (
	"bytes"
	"context"
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestThemeSwitcherRendering(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.ThemeSwitcher().Render(ctx, &buf)
	if err != nil {
		t.Fatalf("ThemeSwitcher render failed: %v", err)
	}

	out := html.UnescapeString(buf.String())

	// Verify theme options are present
	requiredSnippets := []string{
		`aria-label="Theme switcher"`,
		`class="lucide shrink-0 size-4 text-warning"`,
		`class="lucide shrink-0 size-4 text-primary"`,
		`class="lucide shrink-0 size-4 text-accent"`,
		`$theme = 'githublight'`,
		`$theme = 'githubdark'`,
		`$theme = 'system'`,
		`Light`,
		`Dark`,
		`System`,
		`githublight`,
		`githubdark`,
		`auto`,
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(out, snippet) {
			t.Errorf("ThemeSwitcher missing expected snippet: %q", snippet)
		}
	}
}

func TestLayoutThemeSignals(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Layout("Test Title").Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Layout render failed: %v", err)
	}

	out := html.UnescapeString(buf.String())

	// Layout should not hardcode data-theme="githubdark" on <html>
	if strings.Contains(out, `data-theme="githubdark"`) {
		t.Errorf("Layout should not hardcode data-theme on <html> tag to allow system theme detection")
	}

	// Layout should have theme signal and effect
	if !strings.Contains(out, "theme:") {
		t.Errorf("Layout missing theme in data-signals")
	}
	if !strings.Contains(out, "$theme === 'system'") {
		t.Errorf("Layout missing system theme logic in data-effect")
	}
}

func TestNavbarIncludesThemeSwitcher(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Navbar(nil).Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Navbar render failed: %v", err)
	}

	html := buf.String()

	if !strings.Contains(html, `aria-label="Theme switcher"`) {
		t.Errorf("Navbar should include ThemeSwitcher component")
	}
}

func TestLoginIncludesThemeSwitcher(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := views.Login(false, "").Render(ctx, &buf)
	if err != nil {
		t.Fatalf("Login render failed: %v", err)
	}

	html := buf.String()

	if !strings.Contains(html, `aria-label="Theme switcher"`) {
		t.Errorf("Login should include ThemeSwitcher component")
	}
}
