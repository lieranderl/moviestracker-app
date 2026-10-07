package jacred

import "regexp"

// Format is what a release's name says it is: where its video comes from,
// how it is encoded, and its best audio. Empty fields are not named.
type Format struct {
	Source string // "Remux", "BluRay", "WEB-DL", "WEBRip", "CAM", …
	Codec  string // "HEVC", "AVC" or "AV1"
	TenBit bool
	Audio  string // "Atmos", "7.1" or "5.1"
	// Recording is a copy filmed or captured in a cinema (CAM, TS, TC).
	Recording bool
}

// token matches pattern as a whole word of a release name, whose words are
// split by anything but letters and digits ("WEB-DL.DDP5.1.H.264").
func token(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + pattern + `)(?:[^\p{L}\p{N}]|$)`)
}

type namedPattern struct {
	name string
	re   *regexp.Regexp
}

// The patterns, best first: the first that matches names the format.
var (
	sources = []namedPattern{
		{"Remux", token(`(?:bd|uhd)?remux`)},
		{"BluRay", token(`blu-?ray|bd-?rip|br-?rip|bd(?:25|50|66|100)`)},
		{"WEB-DLRip", token(`web-?dl-?rip`)},
		{"WEB-DL", token(`web-?dl`)},
		{"WEBRip", token(`web-?rip`)},
		{"HDTV", token(`hdtv(?:-?rip)?`)},
		{"DVD", token(`dvd(?:-?rip|5|9)?`)},
		{"CAM", token(`cam(?:-?rip)?|экранка`)},
		{"TS", token(`hd-?ts|ts|telesync`)},
		{"TC", token(`hd-?tc|tc|telecine`)},
	}
	codecs = []namedPattern{
		{"HEVC", token(`hevc|[hx]\.?265`)},
		{"AVC", token(`avc|[hx]\.?264`)},
		{"AV1", token(`av1`)},
	}
	audios = []namedPattern{
		{"Atmos", token(`atmos`)},
		{"7.1", token(`(?:dd\+?|ddp|e?ac3|aac|dts|truehd|flac)?7[.,]1`)},
		{"5.1", token(`(?:dd\+?|ddp|e?ac3|aac|dts|truehd|flac)?5[.,]1`)},
	}
	tenBit     = token(`10[- ]?bits?`)
	recordings = map[string]bool{"CAM": true, "TS": true, "TC": true}
)

func firstMatch(patterns []namedPattern, title string) string {
	for _, p := range patterns {
		if p.re.MatchString(title) {
			return p.name
		}
	}
	return ""
}

// Format reads the release's format from its name.
func (r Result) Format() Format {
	f := Format{
		Source: firstMatch(sources, r.Title),
		Codec:  firstMatch(codecs, r.Title),
		TenBit: tenBit.MatchString(r.Title),
		Audio:  firstMatch(audios, r.Title),
	}
	f.Recording = recordings[f.Source]
	return f
}
