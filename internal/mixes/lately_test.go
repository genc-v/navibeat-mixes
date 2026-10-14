package mixes

import (
	"strconv"
	"testing"
	"time"
)

var day0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// track makes n tracks by one artist in one genre, the given days ago (-1 for
// never played) with the given play count.
func tracksBy(artist, genre string, n, daysAgo, plays int) []Track {
	out := make([]Track, n)
	for i := range out {
		t := Track{ID: artist + strconv.Itoa(i), Artist: artist, Genre: genre, PlayCount: plays}
		if daysAgo >= 0 {
			t.LastPlayed = day0.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		}
		out[i] = t
	}
	return out
}

func join(lists ...[]Track) []Track {
	var out []Track
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

const threeWeeks = 21 * 24 * time.Hour

func TestLatelyBeatsAllTime(t *testing.T) {
	tracks := join(
		tracksBy("Old Favourite", "Rock", 12, 200, 90), // played to death, long ago
		tracksBy("This Month", "Jazz", 12, 2, 3),       // played a little, this week
		tracksBy("Last Month", "Blues", 12, 30, 5),     // outside the window
	)
	if got := TopArtistsLately(tracks, day0, threeWeeks, 3, 10); len(got) != 1 || got[0] != "This Month" {
		t.Errorf("artists lately %v, want only This Month", got)
	}
	if got := TopArtistsOwning(tracks, 1, 10); got[0] != "Old Favourite" {
		t.Errorf("all-time ranking changed: %v", got)
	}
	if got := TopGenresLately(tracks, day0, threeWeeks, 3, 10, 0); len(got) != 1 || got[0] != "Jazz" {
		t.Errorf("genres lately %v, want only Jazz", got)
	}
	// Owning too few tracks to fill a mix keeps an artist out, however recent.
	few := tracksBy("Two Songs", "Pop", 2, 0, 9)
	if got := TopArtistsLately(join(tracks, few), day0, threeWeeks, 3, 10); len(got) != 1 {
		t.Errorf("an artist who cannot fill a mix was ranked: %v", got)
	}
}

func TestLatelyWeightsTheRecentHigher(t *testing.T) {
	tracks := join(tracksBy("Yesterday", "Rock", 10, 1, 1), tracksBy("Three Weeks", "Rock", 10, 19, 1))
	if got := TopArtistsLately(tracks, day0, threeWeeks, 2, 1); got[0] != "Yesterday" {
		t.Errorf("order %v", got)
	}
}

func TestFillFrom(t *testing.T) {
	got := FillFrom([]string{"Muse"}, []string{"muse", "Blur", "Oasis", "Pulp"}, 3)
	if len(got) != 3 || got[0] != "Muse" || got[1] != "Blur" || got[2] != "Oasis" {
		t.Errorf("FillFrom = %v", got)
	}
}

func TestSeedTracksIsTheLatestPlayOfEachArtist(t *testing.T) {
	tracks := []Track{
		{ID: "a1", Artist: "Muse", LastPlayed: day0.Add(-48 * time.Hour)},
		{ID: "a2", Artist: "Muse", LastPlayed: day0.Add(-1 * time.Hour)},
		{ID: "b1", Artist: "Blur"},
	}
	if got := SeedTracks(tracks, []string{"muse", "Blur"}); len(got) != 1 || got[0] != "a2" {
		t.Errorf("seeds %v", got)
	}
}

func TestNewToYouIsOnlyNeverPlayedAndPrefersSimilarNewArtists(t *testing.T) {
	library := join(
		tracksBy("Muse", "Rock", 12, 1, 4),          // what is played lately
		tracksBy("Muse Unplayed", "Rock", 5, -1, 0), // unplayed, recent genre
		tracksBy("Polka", "Polka", 5, -1, 0),        // unplayed, unrelated genre
	)
	similar := join(
		tracksBy("Royal Blood", "Rock", 4, -1, 0), // never played artist, similar
		tracksBy("Muse", "Rock", 2, 1, 4),         // similar but already played
	)
	sel := BuildNewToYou(library, similar, NewToYouOptions{Now: day0, Window: threeWeeks, Week: 1, Size: 30, MaxPerArtist: 3})
	got := map[string]bool{}
	for _, id := range sel.TrackIDs {
		got[id] = true
	}
	for _, id := range sel.TrackIDs {
		if id[:4] == "Muse" && id[:13] != "Muse Unplayed" {
			t.Errorf("a played track %s made it in", id)
		}
		if len(id) >= 5 && id[:5] == "Polka" {
			t.Errorf("an unrelated genre made it in: %s", id)
		}
	}
	if len(sel.TrackIDs) != 6 {
		t.Errorf("got %d tracks %v, want 3 Royal Blood (capped) + 3 Muse Unplayed", len(sel.TrackIDs), sel.TrackIDs)
	}
	if sel.TrackIDs[0][:11] != "Royal Blood" {
		t.Errorf("the similar, never played artist should lead: %v", sel.TrackIDs)
	}
	if sel.Slot != NewToYou {
		t.Errorf("slot %q", sel.Slot)
	}
	// Same week, same mix.
	again := BuildNewToYou(library, similar, NewToYouOptions{Now: day0, Window: threeWeeks, Week: 1, Size: 30, MaxPerArtist: 3})
	for i := range again.TrackIDs {
		if again.TrackIDs[i] != sel.TrackIDs[i] {
			t.Fatal("not stable within a week")
		}
	}
}
