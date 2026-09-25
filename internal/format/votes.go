package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatVoteCount returns a compact string representation of vote count
// (<1000 as count, thousands as integer k e.g. "2k", "12k", "57k", millions e.g. "1M", "1.2M").
// Returns empty string if count <= 0.
func FormatVoteCount(count int) string {
	if count <= 0 {
		return ""
	}
	if count < 1000 {
		return strconv.Itoa(count)
	}
	if count < 1_000_000 {
		k := (count + 500) / 1000
		if k >= 1000 {
			return "1M"
		}
		return fmt.Sprintf("%dk", k)
	}
	val := float64(count) / 1_000_000.0
	str := fmt.Sprintf("%.1fM", val)
	return strings.Replace(str, ".0M", "M", 1)
}

// FormatVotes normalizes and compacts a raw vote string (e.g. "2000", "2,000", "56844", "120k", "1.2M")
// to match FormatVoteCount output (e.g. "2k", "57k", "120k", "1.2M").
// Returns empty string if votes is empty, non-positive, or N/A.
func FormatVotes(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "n/a") || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "null") {
		return ""
	}

	// Remove outer parentheses e.g. "(56844)" or "(2k)"
	raw = strings.TrimPrefix(raw, "(")
	raw = strings.TrimSuffix(raw, ")")
	raw = strings.TrimSpace(raw)

	// Remove trailing "votes" / "vote"
	raw = strings.TrimSuffix(raw, " votes")
	raw = strings.TrimSuffix(raw, " vote")
	raw = strings.TrimSpace(raw)

	clean := strings.ReplaceAll(raw, ",", "")
	lower := strings.ToLower(clean)

	if strings.HasSuffix(lower, "k") {
		numStr := strings.TrimSpace(strings.TrimSuffix(lower, "k"))
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			if val <= 0 {
				return ""
			}
			return FormatVoteCount(int(math.Round(val * 1000)))
		}
	}

	if strings.HasSuffix(lower, "m") {
		numStr := strings.TrimSpace(strings.TrimSuffix(lower, "m"))
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			if val <= 0 {
				return ""
			}
			return FormatVoteCount(int(math.Round(val * 1_000_000)))
		}
	}

	if count, err := strconv.Atoi(clean); err == nil {
		return FormatVoteCount(count)
	}

	if val, err := strconv.ParseFloat(clean, 64); err == nil {
		return FormatVoteCount(int(math.Round(val)))
	}

	return clean
}

// RatingTitle returns a standard tooltip title for ratings:
// "<Source>: <Rating>/10 (<votes> votes)" when votes are present, or
// "<Source>: <Rating>/10" when votes are absent.
func RatingTitle(source, rating, votes string) string {
	source = strings.TrimSpace(source)
	rating = strings.TrimSpace(rating)
	if rating == "" || rating == "0" {
		rating = "NR"
	}

	formattedVotes := FormatVotes(votes)
	if formattedVotes != "" {
		return fmt.Sprintf("%s: %s/10 (%s votes)", source, rating, formattedVotes)
	}
	return fmt.Sprintf("%s: %s/10", source, rating)
}
