package format

import (
	"testing"
)

func TestFormatVoteCount(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  string
	}{
		{name: "zero", count: 0, want: ""},
		{name: "negative", count: -10, want: ""},
		{name: "single digit", count: 7, want: "7"},
		{name: "hundreds", count: 850, want: "850"},
		{name: "exact 999", count: 999, want: "999"},
		{name: "exact 1000", count: 1000, want: "1k"},
		{name: "1200 rounded down", count: 1200, want: "1k"},
		{name: "1499 rounded down", count: 1499, want: "1k"},
		{name: "1500 rounded up", count: 1500, want: "2k"},
		{name: "exact 2000", count: 2000, want: "2k"},
		{name: "2400", count: 2400, want: "2k"},
		{name: "2500", count: 2500, want: "3k"},
		{name: "9900", count: 9900, want: "10k"},
		{name: "10000", count: 10000, want: "10k"},
		{name: "12450", count: 12450, want: "12k"},
		{name: "56844", count: 56844, want: "57k"},
		{name: "120000", count: 120000, want: "120k"},
		{name: "999499", count: 999499, want: "999k"},
		{name: "999500 reaches 1M", count: 999500, want: "1M"},
		{name: "1000000", count: 1000000, want: "1M"},
		{name: "1200000", count: 1200000, want: "1.2M"},
		{name: "1500000", count: 1500000, want: "1.5M"},
		{name: "2000000", count: 2000000, want: "2M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatVoteCount(tt.count); got != tt.want {
				t.Errorf("FormatVoteCount(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestFormatVotes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "whitespace", raw: "   ", want: ""},
		{name: "na uppercase", raw: "N/A", want: ""},
		{name: "none", raw: "none", want: ""},
		{name: "null", raw: "null", want: ""},
		{name: "zero string", raw: "0", want: ""},
		{name: "negative string", raw: "-5", want: ""},
		{name: "plain small", raw: "850", want: "850"},
		{name: "thousands plain e.g. 2000", raw: "2000", want: "2k"},
		{name: "thousands comma formatted e.g. 2,000", raw: "2,000", want: "2k"},
		{name: "raw imdb service format 56844", raw: "56844", want: "57k"},
		{name: "raw imdb with comma 56,844", raw: "56,844", want: "57k"},
		{name: "already k e.g. 120k", raw: "120k", want: "120k"},
		{name: "already K uppercase e.g. 120K", raw: "120K", want: "120k"},
		{name: "decimal k e.g. 2.4k", raw: "2.4k", want: "2k"},
		{name: "parentheses wrapped e.g. (56844)", raw: "(56844)", want: "57k"},
		{name: "trailing votes suffix e.g. 2000 votes", raw: "2000 votes", want: "2k"},
		{name: "millions e.g. 1,200,000", raw: "1,200,000", want: "1.2M"},
		{name: "already M e.g. 1.5M", raw: "1.5M", want: "1.5M"},
		{name: "already 1M", raw: "1M", want: "1M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatVotes(tt.raw); got != tt.want {
				t.Errorf("FormatVotes(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRatingTitle(t *testing.T) {
	tests := []struct {
		name   string
		source string
		rating string
		votes  string
		want   string
	}{
		{
			name:   "IMDb with raw votes",
			source: "IMDb",
			rating: "6.3",
			votes:  "56844",
			want:   "IMDb: 6.3/10 (57k votes)",
		},
		{
			name:   "IMDb with thousands votes e.g. 2000",
			source: "IMDb",
			rating: "7.8",
			votes:  "2000",
			want:   "IMDb: 7.8/10 (2k votes)",
		},
		{
			name:   "TMDB with votes",
			source: "TMDB",
			rating: "8.7",
			votes:  "12450",
			want:   "TMDB: 8.7/10 (12k votes)",
		},
		{
			name:   "Without votes",
			source: "IMDb",
			rating: "8.0",
			votes:  "",
			want:   "IMDb: 8.0/10",
		},
		{
			name:   "No rating",
			source: "TMDB",
			rating: "",
			votes:  "",
			want:   "TMDB: NR/10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RatingTitle(tt.source, tt.rating, tt.votes); got != tt.want {
				t.Errorf("RatingTitle(%q, %q, %q) = %q, want %q", tt.source, tt.rating, tt.votes, got, tt.want)
			}
		})
	}
}
