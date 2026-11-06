package streams_test

import (
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/streams"
)

func FuzzRangeStartIsNeverNegative(f *testing.F) {
	for _, header := range []string{"", "bytes=0-", "bytes=100-199", "bytes=-500", "bytes= 7 -", "bytes=99999999999999999999-", "items=1-2"} {
		f.Add(header)
	}
	f.Fuzz(func(t *testing.T, header string) {
		if got := streams.RangeStart(header); got < 0 {
			t.Errorf("RangeStart(%q) = %d", header, got)
		}
	})
}
