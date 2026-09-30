package main

import "testing"

func TestTheAppListensOnThePortCloudRunGives(t *testing.T) {
	t.Setenv("PORT", "9000")
	if got := listenAddr(); got != ":9000" {
		t.Fatalf("listenAddr() = %q, want %q", got, ":9000")
	}
}

func TestTheAppListensOn8080WithoutAPort(t *testing.T) {
	t.Setenv("PORT", "")
	if got := listenAddr(); got != ":8080" {
		t.Fatalf("listenAddr() = %q, want %q", got, ":8080")
	}
}

func TestSignInSettingsComeFromTheEnvironment(t *testing.T) {
	t.Setenv("MT_WEB_BASE_URL", "https://web.example/")
	t.Setenv("MT_WEB_SESSION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MT_WEB_GOOGLE_CLIENT_ID", "id.apps.googleusercontent.com")
	t.Setenv("MT_WEB_GOOGLE_CLIENT_SECRET", "shh")
	cfg := configFromEnv()
	if cfg.BaseURL != "https://web.example" {
		t.Errorf("BaseURL = %q, want it without the trailing slash", cfg.BaseURL)
	}
	if string(cfg.SessionKey) != "0123456789abcdef0123456789abcdef" || cfg.Google.ClientID != "id.apps.googleusercontent.com" || cfg.Google.ClientSecret != "shh" {
		t.Errorf("config = %+v, want the session key and Google client from the environment", cfg)
	}
}

func TestWithoutAFirestoreProjectUsersDataIsKeptInMemory(t *testing.T) {
	t.Setenv("MT_WEB_FIRESTORE_PROJECT", "")
	users, err := openStore(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = users.Close() }()
	if _, ok := users.(memoryStore); !ok {
		t.Errorf("openStore() = %T, want the in-memory store", users)
	}
}

func TestTheCatalogUsesTheTMDBKeyAndIMDbServiceFromTheEnvironment(t *testing.T) {
	t.Setenv("MT_WEB_TMDB_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("MT_WEB_IMDB_URL", "")
	cfg := configFromEnv()
	if cfg.Sources.Catalog == nil || cfg.Sources.Details == nil {
		t.Error("with a TMDB key, the catalog has no TMDB client")
	}
	if cfg.Sources.IMDb == nil {
		t.Error("without MT_WEB_IMDB_URL, the catalog does not use the default IMDb rating service")
	}
	if cfg.Sources.Torrents != nil {
		t.Error("the web app searches torrents before it is set up to")
	}
}
