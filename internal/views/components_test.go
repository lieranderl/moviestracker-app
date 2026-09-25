package views

import (
	"context"
	"strings"
	"testing"
)

func TestIMDbRatingFragment(t *testing.T) {
	tests := []struct {
		name      string
		rating    string
		votes     string
		wantBadge string
		wantTitle string
	}{
		{
			name:      "thousands plain e.g. 2000",
			rating:    "7.8",
			votes:     "2000",
			wantBadge: "(2k)",
			wantTitle: `title="IMDb: 7.8/10 (2k votes)"`,
		},
		{
			name:      "raw service 56844",
			rating:    "6.3",
			votes:     "56844",
			wantBadge: "(57k)",
			wantTitle: `title="IMDb: 6.3/10 (57k votes)"`,
		},
		{
			name:      "already formatted 120k",
			rating:    "8.5",
			votes:     "120k",
			wantBadge: "(120k)",
			wantTitle: `title="IMDb: 8.5/10 (120k votes)"`,
		},
		{
			name:      "empty votes",
			rating:    "8.0",
			votes:     "",
			wantBadge: "",
			wantTitle: `title="IMDb: 8.0/10"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			err := IMDbRatingFragment("test-id", tt.rating, tt.votes).Render(context.Background(), &sb)
			if err != nil {
				t.Fatalf("IMDbRatingFragment.Render() failed: %v", err)
			}
			out := sb.String()
			if tt.wantBadge != "" && !strings.Contains(out, tt.wantBadge) {
				t.Errorf("Render() missing expected badge text %q in output: %s", tt.wantBadge, out)
			}
			if tt.wantBadge == "" && strings.Contains(out, "font-mono") {
				t.Errorf("Render() should not contain vote span when empty, got: %s", out)
			}
			if !strings.Contains(out, tt.wantTitle) {
				t.Errorf("Render() missing expected title %q in output: %s", tt.wantTitle, out)
			}
		})
	}
}
