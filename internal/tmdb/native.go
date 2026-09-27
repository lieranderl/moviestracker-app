package tmdb

import (
	"slices"
	"unicode"
)

// altTitle is one of a title's alternative titles, which TMDB lists by
// country, not language.
type altTitle struct {
	Country string `json:"iso_3166_1"`
	Title   string `json:"title"`
}

// scripts are the writing systems of languages not written in Latin
// letters, by TMDB's language code.
var scripts = map[string][]*unicode.RangeTable{
	"ru": {unicode.Cyrillic}, "uk": {unicode.Cyrillic}, "be": {unicode.Cyrillic}, "bg": {unicode.Cyrillic},
	"mk": {unicode.Cyrillic}, "kk": {unicode.Cyrillic}, "ky": {unicode.Cyrillic}, "mn": {unicode.Cyrillic}, "tg": {unicode.Cyrillic},
	"ja": {unicode.Hiragana, unicode.Katakana, unicode.Han},
	"zh": {unicode.Han}, "cn": {unicode.Han},
	"ko": {unicode.Hangul},
	"he": {unicode.Hebrew}, "yi": {unicode.Hebrew},
	"ar": {unicode.Arabic}, "fa": {unicode.Arabic}, "ur": {unicode.Arabic},
	"el": {unicode.Greek},
	"th": {unicode.Thai},
	"ka": {unicode.Georgian},
	"hy": {unicode.Armenian},
	"hi": {unicode.Devanagari}, "mr": {unicode.Devanagari}, "ne": {unicode.Devanagari},
	"sa": {unicode.Devanagari},
	"bn": {unicode.Bengali}, "as": {unicode.Bengali}, "ta": {unicode.Tamil}, "te": {unicode.Telugu},
	"ml": {unicode.Malayalam}, "kn": {unicode.Kannada}, "gu": {unicode.Gujarati}, "pa": {unicode.Gurmukhi},
	"or": {unicode.Oriya}, "si": {unicode.Sinhala},
	"km": {unicode.Khmer}, "my": {unicode.Myanmar}, "lo": {unicode.Lao}, "bo": {unicode.Tibetan}, "dz": {unicode.Tibetan},
	"am": {unicode.Ethiopic}, "ti": {unicode.Ethiopic},
	"dv": {unicode.Thaana},
	"ps": {unicode.Arabic}, "sd": {unicode.Arabic}, "ug": {unicode.Arabic},
}

// nativeTitle is original, or, when TMDB wrote it in letters of another
// script than its language's (a Russian series called "The Boy's Word
// :Blood on the Asphalt"), the first alternative title of its countries
// that is in its language's script. Torrent trackers name releases by it.
func nativeTitle(original, language string, countries []string, alts []altTitle) string {
	script, ok := scripts[language]
	if !ok || inScript(original, script) {
		return original
	}
	for _, alt := range alts {
		if slices.Contains(countries, alt.Country) && inScript(alt.Title, script) {
			return alt.Title
		}
	}
	return original
}

// inScript reports whether s has a letter of script.
func inScript(s string, script []*unicode.RangeTable) bool {
	for _, r := range s {
		if unicode.IsOneOf(script, r) {
			return true
		}
	}
	return false
}
