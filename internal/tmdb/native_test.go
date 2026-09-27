package tmdb

import (
	"context"
	"testing"
)

// TMDB gives some titles an original title in another script than their
// language: the Russian series 234763 has "The Boy's Word :Blood on the
// Asphalt". Its title in its own language is among the alternative titles
// of its country.
func TestAnOriginalTitleInAnotherScriptIsReplacedByTheTitleInItsLanguage(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{
		"/3/tv/234763": `{"id": 234763, "name": "The Boy's Word: Blood on the Asphalt",
			"original_name": "The Boy's Word :Blood on the Asphalt", "original_language": "ru", "origin_country": ["RU"],
			"alternative_titles": {"results": [
				{"iso_3166_1": "IL", "title": "מילה של גבר"},
				{"iso_3166_1": "RS", "title": "Дечачка реч. Крв на асфалту"},
				{"iso_3166_1": "RU", "title": "Slovo.pacana.Krov.na.asfalte"},
				{"iso_3166_1": "RU", "title": "Слово пацана. Кровь на асфальте"},
				{"iso_3166_1": "UA", "title": "Слово пацана. Кров на асфальті"}
			]}}`,
		"/3/movie/4": `{"id": 4, "title": "Drishyam", "original_title": "Drishyam", "original_language": "ml", "origin_country": ["IN"],
			"alternative_titles": {"titles": [{"iso_3166_1": "IN", "title": "ദൃശ്യം"}]}}`,
		"/3/movie/1": `{"id": 1, "title": "Bigfoot", "original_title": "Bigfoot", "original_language": "ja", "origin_country": ["JP"],
			"alternative_titles": {"titles": [{"iso_3166_1": "JP", "title": "ビッグフット"}]}}`,
	})

	tv, err := client.TV(context.Background(), 234763)
	if err != nil {
		t.Fatal(err)
	}
	if tv.OriginalTitle != "Слово пацана. Кровь на асфальте" {
		t.Errorf("series OriginalTitle = %q, want its Russian title", tv.OriginalTitle)
	}
	movie, err := client.Movie(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if movie.OriginalTitle != "ビッグフット" {
		t.Errorf("movie OriginalTitle = %q, want its Japanese title", movie.OriginalTitle)
	}
	if m, err := client.Movie(context.Background(), 4); err != nil || m.OriginalTitle != "ദൃശ്യം" {
		t.Errorf("Malayalam movie OriginalTitle = %q, %v, want its Malayalam title", m.OriginalTitle, err)
	}
}

func TestAnOriginalTitleInItsOwnScriptStays(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{
		// Already in Cyrillic.
		"/3/tv/1": `{"id": 1, "name": "Brother", "original_name": "Брат", "original_language": "ru", "origin_country": ["RU"],
			"alternative_titles": {"results": [{"iso_3166_1": "RU", "title": "Брат 1"}]}}`,
		// Languages in Latin letters keep theirs, accents or not.
		"/3/movie/2": `{"id": 2, "title": "The Intouchables", "original_title": "Intouchables", "original_language": "fr", "origin_country": ["FR"],
			"alternative_titles": {"titles": [{"iso_3166_1": "FR", "title": "Les Intouchables"}]}}`,
		// No title of its country in its script: nothing better to take.
		"/3/movie/3": `{"id": 3, "title": "Leviathan", "original_title": "Leviathan", "original_language": "ru", "origin_country": ["RU"],
			"alternative_titles": {"titles": [{"iso_3166_1": "UA", "title": "Левіафан"}, {"iso_3166_1": "RU", "title": "Leviafan"}]}}`,
	})

	if tv, err := client.TV(context.Background(), 1); err != nil || tv.OriginalTitle != "Брат" {
		t.Errorf("series OriginalTitle = %q, %v, want Брат", tv.OriginalTitle, err)
	}
	for id, want := range map[int]string{2: "Intouchables", 3: "Leviathan"} {
		if m, err := client.Movie(context.Background(), id); err != nil || m.OriginalTitle != want {
			t.Errorf("movie %d OriginalTitle = %q, %v, want %q", id, m.OriginalTitle, err, want)
		}
	}
}
