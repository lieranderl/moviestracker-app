package tmdb

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const (
	maxKnownFor       = 12
	maxSearchResults  = 20
	portraitImageBase = "https://image.tmdb.org/t/p/h632"
)

// Credit is one title in a person's filmography with their role on it.
type Credit struct {
	MediaItem
	// Role is the character played, or the crew job.
	Role string
}

// Person is the full person page model.
type Person struct {
	ID                 int
	Name               string
	Biography          string
	Birthday           string
	Deathday           string
	PlaceOfBirth       string
	ProfilePath        string
	KnownForDepartment string
	ImdbID             string
	Homepage           string
	Links              ExternalIDs
	KnownFor           []MediaItem
	Credits            []Credit
}

// ProfileURL returns the h632 portrait URL or empty string.
func (p Person) ProfileURL() string {
	if !validAssetPath(p.ProfilePath) {
		return ""
	}
	return portraitImageBase + p.ProfilePath
}

// BiographyParagraphs splits the biography on blank lines.
func (p Person) BiographyParagraphs() []string {
	var out []string
	for para := range strings.SplitSeq(p.Biography, "\n") {
		if para = strings.TrimSpace(para); para != "" {
			out = append(out, para)
		}
	}
	return out
}

// MovieCredits returns movie credits, newest first.
func (p Person) MovieCredits() []Credit { return p.creditsOf("movie") }

// TVCredits returns series credits, newest first.
func (p Person) TVCredits() []Credit { return p.creditsOf("tv") }

func (p Person) creditsOf(mediaType string) []Credit {
	var out []Credit
	for _, c := range p.Credits {
		if c.MediaType == mediaType {
			out = append(out, c)
		}
	}
	return out
}

type rawCredit struct {
	rawTMDBResult
	Character string `json:"character"`
	Job       string `json:"job"`
}

type rawPerson struct {
	ID                 int         `json:"id"`
	Name               string      `json:"name"`
	Biography          string      `json:"biography"`
	Birthday           string      `json:"birthday"`
	Deathday           string      `json:"deathday"`
	PlaceOfBirth       string      `json:"place_of_birth"`
	ProfilePath        string      `json:"profile_path"`
	KnownForDepartment string      `json:"known_for_department"`
	ImdbID             string      `json:"imdb_id"`
	Homepage           string      `json:"homepage"`
	ExternalIDs        ExternalIDs `json:"external_ids"`
	CombinedCredits    struct {
		Cast []rawCredit `json:"cast"`
		Crew []rawCredit `json:"crew"`
	} `json:"combined_credits"`
}

// Person returns a person's profile and filmography, cached for the client TTL.
func (c *Client) Person(ctx context.Context, id int) (*Person, error) {
	return cached(ctx, c, fmt.Sprintf("person/%d", id), func(ctx context.Context) (*Person, error) {
		var raw rawPerson
		query := url.Values{"append_to_response": {"combined_credits,external_ids"}}
		if err := c.getJSON(ctx, fmt.Sprintf("/3/person/%d", id), query, &raw); err != nil {
			return nil, err
		}
		p := &Person{
			ID:                 raw.ID,
			Name:               raw.Name,
			Biography:          raw.Biography,
			Birthday:           raw.Birthday,
			Deathday:           raw.Deathday,
			PlaceOfBirth:       raw.PlaceOfBirth,
			ProfilePath:        raw.ProfilePath,
			KnownForDepartment: raw.KnownForDepartment,
			ImdbID:             raw.ImdbID,
			Homepage:           raw.Homepage,
			Links:              raw.ExternalIDs,
		}
		p.Credits = mergeCredits(raw.CombinedCredits.Cast, raw.CombinedCredits.Crew)
		source := raw.CombinedCredits.Cast
		if raw.KnownForDepartment != "" && raw.KnownForDepartment != "Acting" {
			source = raw.CombinedCredits.Crew
		}
		p.KnownFor = knownFor(source, maxKnownFor)
		return p, nil
	})
}

// mergeCredits returns one credit per title with all of the person's roles
// on it, newest release first and undated (unannounced) titles last.
func mergeCredits(cast, crew []rawCredit) []Credit {
	var credits []Credit
	index := make(map[string]int)
	add := func(c rawCredit, role string) {
		item := c.mediaItem("movie")
		key := fmt.Sprintf("%s/%d", item.MediaType, item.ID)
		i, seen := index[key]
		if !seen {
			index[key] = len(credits)
			credits = append(credits, Credit{MediaItem: item, Role: role})
			return
		}
		if role != "" && !slices.Contains(strings.Split(credits[i].Role, ", "), role) {
			credits[i].Role = strings.TrimPrefix(credits[i].Role+", "+role, ", ")
		}
	}
	for _, c := range cast {
		add(c, c.Character)
	}
	for _, c := range crew {
		add(c, c.Job)
	}
	slices.SortStableFunc(credits, func(a, b Credit) int { return newestFirst(a.ReleaseDate, b.ReleaseDate) })
	return credits
}

// knownFor picks the most-voted unique titles that have a poster and lists
// them newest first.
func knownFor(credits []rawCredit, n int) []MediaItem {
	sorted := slices.Clone(credits)
	slices.SortStableFunc(sorted, func(a, b rawCredit) int { return cmp.Compare(b.VoteCount, a.VoteCount) })
	seen := make(map[string]bool)
	var out []MediaItem
	for _, c := range sorted {
		item := c.mediaItem("movie")
		key := fmt.Sprintf("%s/%d", item.MediaType, item.ID)
		if item.PosterURL() == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
		if len(out) == n {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b MediaItem) int { return newestFirst(a.ReleaseDate, b.ReleaseDate) })
	return out
}

// PersonSummary is a person as listed in search results.
type PersonSummary struct {
	ID                 int
	Name               string
	ProfilePath        string
	KnownForDepartment string
	KnownFor           []string
}

// ProfileURL returns the w185 headshot URL or empty string.
func (p PersonSummary) ProfileURL() string { return profileURL(p.ProfilePath) }

// KnownForTitles joins the titles the person is best known for.
func (p PersonSummary) KnownForTitles() string { return strings.Join(p.KnownFor, ", ") }

// SearchResults groups a multi-search by kind.
type SearchResults struct {
	Query  string
	Movies []MediaItem
	Series []MediaItem
	People []PersonSummary
}

// Empty reports whether nothing matched.
func (s SearchResults) Empty() bool {
	return len(s.Movies) == 0 && len(s.Series) == 0 && len(s.People) == 0
}

type rawSearchResult struct {
	rawTMDBResult
	ProfilePath        string          `json:"profile_path"`
	KnownForDepartment string          `json:"known_for_department"`
	KnownFor           []rawTMDBResult `json:"known_for"`
}

// Search runs a TMDB multi-search across movies, series and people.
func (c *Client) Search(ctx context.Context, query string) (*SearchResults, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return &SearchResults{}, nil
	}
	return cached(ctx, c, "search/"+strings.ToLower(query), func(ctx context.Context) (*SearchResults, error) {
		var raw struct {
			Results []rawSearchResult `json:"results"`
		}
		params := url.Values{"query": {query}, "include_adult": {"false"}}
		if err := c.getJSON(ctx, "/3/search/multi", params, &raw); err != nil {
			return nil, err
		}
		res := &SearchResults{Query: query}
		for _, r := range raw.Results {
			switch r.MediaType {
			case "movie":
				res.Movies = appendMax(res.Movies, r.mediaItem("movie"))
			case "tv":
				res.Series = appendMax(res.Series, r.mediaItem("tv"))
			case "person":
				p := PersonSummary{ID: r.ID, Name: r.Name, ProfilePath: r.ProfilePath, KnownForDepartment: r.KnownForDepartment}
				for _, k := range r.KnownFor {
					p.KnownFor = append(p.KnownFor, k.mediaItem("movie").Title)
				}
				res.People = appendMax(res.People, p)
			}
		}
		return res, nil
	})
}

func appendMax[T any](s []T, v T) []T {
	if len(s) >= maxSearchResults {
		return s
	}
	return append(s, v)
}
