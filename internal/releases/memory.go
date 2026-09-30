package releases

import (
	"context"
	"slices"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Memory is a Source of fixed feeds, for tests and local runs without
// Firestore.
type Memory struct {
	feeds map[Feed][]Release
}

// NewMemory holds feeds; a missing feed is empty.
func NewMemory(feeds map[Feed][]Release) *Memory {
	sorted := make(map[Feed][]Release, len(feeds))
	for feed, rs := range feeds {
		rs = slices.Clone(rs)
		slices.SortStableFunc(rs, func(a, b Release) int { return b.FoundAt.Compare(a.FoundAt) })
		sorted[feed] = rs
	}
	return &Memory{feeds: sorted}
}

// Page implements Source.
func (m *Memory) Page(ctx context.Context, feed Feed, page int) (tmdb.Page, error) {
	if err := checkPage(feed, page); err != nil {
		return tmdb.Page{}, err
	}
	rs := m.feeds[feed]
	out := tmdb.Page{Page: page, TotalPages: totalPages(len(rs))}
	for _, r := range rs[min((page-1)*PageSize, len(rs)):min(page*PageSize, len(rs))] {
		out.Items = append(out.Items, item(ctx, r))
	}
	return out, nil
}
