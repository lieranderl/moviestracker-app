package views

import (
	"context"
	"encoding/json"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

type playerControlsKey struct{}

// WithPlayerControls gives existing title controls file-specific response targets.
// Callers validate the key before using it as a signal path or target suffix.
func WithPlayerControls(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, playerControlsKey{}, key)
}

func playerControls(ctx context.Context) string {
	key, _ := ctx.Value(playerControlsKey{}).(string)
	return key
}

func titleControlID(ctx context.Context, id string) string {
	if key := playerControls(ctx); key != "" {
		return id + "-" + key
	}
	return id
}

func titleControlURL(ctx context.Context, url string) string {
	if key := playerControls(ctx); key != "" {
		return url + "&player=" + key
	}
	return url
}

func reviewRatingSignal(ctx context.Context) string {
	if key := playerControls(ctx); key != "" {
		return "$_completionReviews." + key + ".rating"
	}
	return "$reviewRating"
}

// ReviewSignals keeps late player responses out of the title page's review state.
func ReviewSignals(ctx context.Context, review store.Review) map[string]any {
	if key := playerControls(ctx); key != "" {
		return map[string]any{"_completionReviews": map[string]any{key: map[string]any{"rating": review.Rating, "note": review.Note}}}
	}
	return map[string]any{"reviewRating": review.Rating, "reviewNote": review.Note, "_reviewReady": true}
}

func reviewInitialSignals(ctx context.Context, review store.Review) string {
	data, _ := json.Marshal(ReviewSignals(ctx, review))
	return string(data)
}

func watchStateButtonClass(ctx context.Context) string {
	if playerControls(ctx) != "" {
		return "btn btn-sm gap-2"
	}
	return "btn sm:btn-lg join-item gap-2"
}
