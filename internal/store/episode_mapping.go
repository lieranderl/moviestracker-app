package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrEpisodeMapping identifies invalid confirmation input.
var ErrEpisodeMapping = errors.New("store: invalid episode mapping")

// MaxMappedEpisodes bounds one confirmed range, well beyond any real season,
// so a stored record can never make completion allocate without limit.
const MaxMappedEpisodes = 10000

// EpisodeMapping is one account's confirmed association of a torrent file
// with an inclusive episode range in a canonical series and season.
type EpisodeMapping struct {
	Hash   string `json:"hash" firestore:"hash"`
	Index  int    `json:"index" firestore:"index"`
	File   string `json:"file" firestore:"file"`
	TMDBID int    `json:"tmdbId" firestore:"tmdbId"`
	Season int    `json:"season" firestore:"season"`
	First  int    `json:"first" firestore:"first"`
	Last   int    `json:"last" firestore:"last"`
}

// EpisodeMappingStore keeps confirmations independently of recent progress.
type EpisodeMappingStore interface {
	EpisodeMapping(ctx context.Context, uid, hash string, index int) (EpisodeMapping, bool, error)
	SaveEpisodeMapping(ctx context.Context, uid string, m EpisodeMapping) error
	ClearEpisodeMapping(ctx context.Context, uid, hash string, index int) error
}

func checkMappingOwner(uid string) error {
	if strings.TrimSpace(uid) == "" || strings.Contains(uid, "/") {
		return fmt.Errorf("%w: invalid account identity", ErrEpisodeMapping)
	}
	return nil
}

func episodeMappingID(uid, hash string, index int) (string, error) {
	if err := checkMappingOwner(uid); err != nil {
		return "", err
	}
	id, err := progressID(hash, index)
	if err != nil {
		return "", fmt.Errorf("%w: invalid file identity", ErrEpisodeMapping)
	}
	return id, nil
}

func checkEpisodeMapping(uid string, m EpisodeMapping) (EpisodeMapping, string, error) {
	id, err := episodeMappingID(uid, m.Hash, m.Index)
	if err != nil {
		return EpisodeMapping{}, "", err
	}
	if m.File == "" || len(m.File) > 4096 || m.TMDBID <= 0 || m.Season < 0 || m.First <= 0 || m.Last < m.First || m.Last-m.First >= MaxMappedEpisodes {
		return EpisodeMapping{}, "", fmt.Errorf("%w: invalid episode or file context", ErrEpisodeMapping)
	}
	m.Hash = strings.ToLower(m.Hash)
	return m, id, nil
}

// servedEpisodeMapping fails closed on a stored record that saving would
// refuse, such as one edited by hand.
func servedEpisodeMapping(uid string, m EpisodeMapping, found bool) (EpisodeMapping, bool, error) {
	if !found {
		return EpisodeMapping{}, false, nil
	}
	if _, _, err := checkEpisodeMapping(uid, m); err != nil {
		return EpisodeMapping{}, false, err
	}
	return m, true, nil
}
