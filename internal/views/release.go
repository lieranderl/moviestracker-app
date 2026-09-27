package views

import "context"

// Release is a newer Moviestracker, as pages show it.
type Release struct {
	Version string // "v1.2.3"
}

type releaseKey struct{}

// WithRelease returns ctx carrying rel, so the navbar of a page rendered
// with it tells administrators about rel.
func WithRelease(ctx context.Context, rel Release) context.Context {
	return context.WithValue(ctx, releaseKey{}, rel)
}

// newRelease is the release ctx carries, if any.
func newRelease(ctx context.Context) (Release, bool) {
	rel, ok := ctx.Value(releaseKey{}).(Release)
	return rel, ok
}
