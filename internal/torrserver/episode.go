package torrserver

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// word wraps pattern so it matches only between non-alphanumerics.
func word(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + pattern + `)(?:[^\p{L}\p{N}]|$)`)
}

var (
	seasonEpisode = word(`s(\d{1,2})[ ._-]?e(\d{1,3})(?:-?e(\d{1,3}))?`)
	crossEpisode  = word(`(\d{1,2})x(\d{2,3})`)
	episodeOnly   = word(`(?:e|ep|episode|серия)[ ._]?(\d{1,3})|(\d{1,3})[ ._]?серия`)
	leadingNumber = regexp.MustCompile(`^(\d{1,3})(?:[ ._-]|$)`)
	seasonFolder  = word(`(?:s|season|сезон)[ ._]?(\d{1,2})`)
)

// EpisodeCode names the episode a torrent file holds ("S01E02", "S02E05–E06",
// or "E07" when no season is known), read from its name and, for the season,
// its folders; "" when the path does not look like an episode.
func EpisodeCode(filePath string) string {
	dir, file := path.Split(filePath)
	name := strings.TrimSuffix(file, path.Ext(file))
	if m := seasonEpisode.FindStringSubmatch(name); m != nil {
		return episodeCode(m[1], m[2], m[3])
	}
	if m := crossEpisode.FindStringSubmatch(name); m != nil {
		return episodeCode(m[1], m[2], "")
	}
	season := folderSeason(dir)
	if m := episodeOnly.FindStringSubmatch(name); m != nil {
		return episodeCode(season, m[1]+m[2], "")
	}
	if m := leadingNumber.FindStringSubmatch(name); m != nil && season != "" {
		return episodeCode(season, m[1], "")
	}
	return ""
}

// folderSeason is the season the nearest folder names, or "".
func folderSeason(dir string) string {
	folders := strings.Split(strings.Trim(dir, "/"), "/")
	for i := len(folders) - 1; i >= 0; i-- {
		if m := seasonFolder.FindStringSubmatch(folders[i]); m != nil {
			return m[1]
		}
	}
	return ""
}

func episodeCode(season, episode, last string) string {
	code := fmt.Sprintf("E%02d", atoi(episode))
	if season != "" {
		code = fmt.Sprintf("S%02d", atoi(season)) + code
	}
	if last != "" {
		code += fmt.Sprintf("–E%02d", atoi(last))
	}
	return code
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
