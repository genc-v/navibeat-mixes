package main

// New to You's similar songs: fetched once per user per day and kept in the
// kvstore, so the continuation calls of one run do not ask the agents again.

import (
	"encoding/json"
	"time"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"

	"github.com/nenadjokic/navibeat-mixes/internal/config"
	"github.com/nenadjokic/navibeat-mixes/internal/library"
	"github.com/nenadjokic/navibeat-mixes/internal/mixes"
)

const (
	// Seeds asked about, and songs asked for per seed. Three lookups through
	// the agents is a few seconds on a slow server, which the budget allows.
	similarSeeds = 3
	similarCount = 40
	similarTTL   = 2 * 24 * 60 * 60
)

type similarCache struct {
	Day    string        `json:"d"`
	Tracks []mixes.Track `json:"t"`
}

func similarKey(username string) string { return "similar:" + username }

// loadSimilar returns today's similar songs for the user, and whether they
// were cached at all: an empty list that WAS asked for is still an answer.
func loadSimilar(username, day string) ([]mixes.Track, bool) {
	data, ok, err := host.KVStoreGet(similarKey(username))
	if err != nil || !ok {
		return nil, false
	}
	var c similarCache
	if json.Unmarshal(data, &c) != nil || c.Day != day {
		return nil, false
	}
	return c.Tracks, true
}

func saveSimilar(username, day string, tracks []mixes.Track) {
	data, err := json.Marshal(similarCache{Day: day, Tracks: tracks})
	if err != nil {
		return
	}
	if err := host.KVStoreSetWithTTL(similarKey(username), data, similarTTL); err != nil {
		logf("user %s: could not keep the similar songs (%d bytes): %v", username, len(data), err)
	}
}

// fetchSimilar asks Navidrome for songs like the most recent play of each of
// the user's top artists lately.
func fetchSimilar(client *library.Client, cfg config.Config, tracks []mixes.Track, now time.Time, window time.Duration, username string) []mixes.Track {
	// getSimilarSongs2 answers from the whole server and cannot be scoped to
	// one library, so an account narrowed to some libraries gets New to You
	// from its own pool alone rather than songs it was told to leave out.
	if len(cfg.Libraries) > 0 {
		return nil
	}
	seeds := mixes.TopArtistsLately(tracks, now, window, similarSeeds, 1)
	if len(seeds) == 0 {
		seeds = mixes.TopArtistsOwning(tracks, similarSeeds, 1)
	}
	var out []mixes.Track
	ids := mixes.SeedTracks(tracks, seeds)
	for _, id := range ids {
		got, err := client.SimilarSongs(id, similarCount)
		if err != nil {
			logf("user %s: similar songs: %v", username, err)
			continue
		}
		out = append(out, got...)
	}
	logf("user %s: %d similar songs for New to You, from %d of your recent artists", username, len(out), len(ids))
	return out
}
