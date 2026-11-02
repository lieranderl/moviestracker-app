package views

import "context"

type signedInKey struct{}

// WithSignedIn marks ctx's pages as a signed-in visitor's: cards offer their
// quick actions (cardActions).
func WithSignedIn(ctx context.Context) context.Context {
	return context.WithValue(ctx, signedInKey{}, true)
}

func signedIn(ctx context.Context) bool {
	in, _ := ctx.Value(signedInKey{}).(bool)
	return in
}
