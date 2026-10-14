package mixes

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"
)

// NewToYou is the mix of songs the user owns and has never played.
const NewToYou Slot = "newtoyou"

// What "lately" means, and the two mixes built on it.
//
// The upstream rankings (TopArtistsOwning, TopGenresOwning) count ALL-TIME
// plays, so an artist played to death two years ago anchors a Daily Mix over
// the one played every day this month. "Lately" ranks by how recently each
// track was last played instead: Navidrome's own last-played date, which
// covers every client and all of the history from before the plugin was
// installed, where the plugin's own collector only knows what it has seen.

// latelyWeight is how much one track's last play says about what the user
// is into now: 0 when it falls outside the window, rising to 3 for a play
// today, so this week counts more than three weeks ago without the edge of
// the window being a cliff.
func latelyWeight(t Track, now time.Time, window time.Duration) float64 {
	if window <= 0 || t.LastPlayed.IsZero() {
		return 0
	}
	age := max(now.Sub(t.LastPlayed), 0)
	if age > window {
		return 0
	}
	return 1 + 2*(1-float64(age)/float64(window))
}

type ranked struct {
	name  string
	score float64
}

func topRanked(scores map[string]float64, display map[string]string, n int, keep func(key string) bool) []string {
	all := make([]ranked, 0, len(scores))
	for key, s := range scores {
		if s <= 0 || !keep(key) {
			continue
		}
		all = append(all, ranked{key, s})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].name < all[j].name
	})
	out := make([]string, 0, n)
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, display[all[i].name])
	}
	return out
}

// TopArtistsLately is the n artists the user has played most lately, among
// those owning at least minOwned tracks (so each can fill a mix).
func TopArtistsLately(tracks []Track, now time.Time, window time.Duration, n, minOwned int) []string {
	scores := map[string]float64{}
	owned := map[string]int{}
	display := map[string]string{}
	for _, t := range tracks {
		a := strings.TrimSpace(t.Artist)
		if a == "" {
			continue
		}
		key := strings.ToLower(a)
		if _, ok := display[key]; !ok {
			display[key] = a
		}
		owned[key]++
		scores[key] += latelyWeight(t, now, window)
	}
	return topRanked(scores, display, n, func(key string) bool { return owned[key] >= minOwned })
}

// TopGenresLately is the n genres the user has played most lately, among
// those that can fill a mix under maxPerArtist (the same capacity rule as
// TopGenresOwning).
func TopGenresLately(tracks []Track, now time.Time, window time.Duration, n, minOwned, maxPerArtist int) []string {
	scores := map[string]float64{}
	display := map[string]string{}
	byArtist := map[string]map[string]int{}
	for _, t := range tracks {
		w := latelyWeight(t, now, window)
		artist := strings.ToLower(t.Artist)
		for _, g := range t.AllGenres() {
			key := strings.ToLower(g)
			if _, ok := display[key]; !ok {
				display[key] = g
			}
			if byArtist[key] == nil {
				byArtist[key] = map[string]int{}
			}
			byArtist[key][artist]++
			scores[key] += w
		}
	}
	return topRanked(scores, display, n, func(key string) bool {
		return genreCapacity(byArtist[key], maxPerArtist) >= minOwned
	})
}

// FillFrom is primary topped up to n from fallback, without repeats (names
// compared ignoring case). A user with little recent listening still gets
// every mix, filled from the all-time ranking.
func FillFrom(primary, fallback []string, n int) []string {
	out := make([]string, 0, n)
	seen := map[string]bool{}
	for _, list := range [][]string{primary, fallback} {
		for _, v := range list {
			if len(out) >= n {
				return out
			}
			k := strings.ToLower(strings.TrimSpace(v))
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

// SeedTracks is the most recently played track of each artist, the ids
// Navidrome's similar songs lookup is asked about.
func SeedTracks(tracks []Track, artists []string) []string {
	latest := map[string]Track{}
	for _, t := range tracks {
		if t.LastPlayed.IsZero() {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(t.Artist))
		if cur, ok := latest[key]; !ok || t.LastPlayed.After(cur.LastPlayed) {
			latest[key] = t
		}
	}
	out := make([]string, 0, len(artists))
	for _, a := range artists {
		if t, ok := latest[strings.ToLower(strings.TrimSpace(a))]; ok {
			out = append(out, t.ID)
		}
	}
	return out
}

// NewToYouOptions controls the New to You mix.
type NewToYouOptions struct {
	Now          time.Time
	Window       time.Duration
	Week         int
	Size         int
	MaxPerArtist int
}

// BuildNewToYou is songs the user owns and has NEVER played, close to what
// they play lately.
//
// Two sources. `similar` is Navidrome's own answer to "songs like these"
// (getSimilarSongs2, the engine behind its Instant Mix) for the user's recent
// artists, and ranks first, in its own order. Behind it, any unplayed track
// in one of the genres the user plays lately. An artist the user has never
// played at all gets a lift: that is the discovery in the name.
//
// Equal scores are ordered by a hash of the track and the WEEK, so the mix
// does not reshuffle daily but does turn over weekly, the way a "discover"
// playlist is expected to.
func BuildNewToYou(tracks, similar []Track, o NewToYouOptions) Selection {
	artistPlays := map[string]int{}
	for _, t := range tracks {
		artistPlays[strings.ToLower(strings.TrimSpace(t.Artist))] += t.PlayCount
	}
	genres := map[string]bool{}
	recent := TopGenresLately(tracks, o.Now, o.Window, 5, 1, 0)
	if len(recent) == 0 {
		recent = TopGenresOwning(tracks, 5, 1, 0)
	}
	for _, g := range recent {
		genres[strings.ToLower(g)] = true
	}

	scores := map[string]int{}
	byID := map[string]Track{}
	consider := func(t Track, score int) {
		if t.ID == "" || t.PlayCount > 0 || !t.LastPlayed.IsZero() {
			return
		}
		if artistPlays[strings.ToLower(strings.TrimSpace(t.Artist))] == 0 {
			score += 15
		}
		if score > scores[t.ID] {
			scores[t.ID] = score
			byID[t.ID] = t
		}
	}
	for i, t := range similar {
		// 50 for the best match down to 20 for the last one asked for.
		consider(t, 20+30*(len(similar)-i)/len(similar))
	}
	for _, t := range tracks {
		if t.sharesAnyGenre(genres) {
			consider(t, 10)
		}
	}

	scored := make([]scoredTrack, 0, len(scores))
	for id, s := range scores {
		scored = append(scored, scoredTrack{track: byID[id], score: s})
	}
	week := strconv.Itoa(o.Week)
	shuffle := func(id string) uint32 {
		h := fnv.New32a()
		_, _ = h.Write([]byte(id + "|" + week))
		return h.Sum32()
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return shuffle(scored[i].track.ID) < shuffle(scored[j].track.ID)
	})
	return Selection{
		Slot:     NewToYou,
		Mode:     ModeFallback,
		TrackIDs: takeIDsCapped(scored, o.Size, o.MaxPerArtist),
	}
}
