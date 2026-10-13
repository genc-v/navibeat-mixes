package library

import (
	"net/url"
	"strings"
	"testing"
)

// fakeLists answers getPlaylists from a fixed list and records every
// rename and create, which is all EnsureMix may do.
func fakeLists(t *testing.T, lists string, calls *[]string) Caller {
	return func(uri string) (string, error) {
		*calls = append(*calls, uri)
		switch {
		case strings.HasPrefix(uri, "getPlaylists?"):
			return `{"subsonic-response":{"status":"ok","playlists":{"playlist":` + lists + `}}}`, nil
		case strings.HasPrefix(uri, "createPlaylist?"):
			return `{"subsonic-response":{"status":"ok","playlist":{"id":"new"}}}`, nil
		}
		return `{"subsonic-response":{"status":"ok"}}`, nil
	}
}

func renames(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "updatePlaylist?") {
			q, _ := url.ParseQuery(strings.TrimPrefix(c, "updatePlaylist?"))
			if n := q.Get("name"); n != "" {
				out = append(out, q.Get("playlistId")+"="+n)
			}
		}
	}
	return out
}

const lists = `[
	{"id":"p1","name":"\ud83d\udfe0 Daily Mix 1","owner":"me","comment":"x\nnb1:dailymix:dailymix-1:2026-10-13:fallback:30"},
	{"id":"p2","name":"Blur Mix","owner":"me","comment":"x\nnb1:dailymix:dailymix-2:2026-10-13:fallback:30"},
	{"id":"p3","name":"\ud83d\udfe0 Artist Radio 1","owner":"me","comment":"lost its machine line"},
	{"id":"p4","name":"Muse Mix","owner":"someone else","comment":"x\nnb1:dailymix:dailymix-1:2026-10-13:fallback:30"}
]`

func TestEnsureMixRenamesTheSlotsOwnPlaylistInPlace(t *testing.T) {
	var calls []string
	c := New(fakeLists(t, lists, &calls), "me")
	id, err := c.EnsureMix("dailymix-1", "Muse Mix", "Daily Mix 1", "\U0001F7E0 Daily Mix 1")
	if err != nil || id != "p1" {
		t.Fatalf("got %q, %v; want the slot's own playlist p1, never someone else's", id, err)
	}
	if got := renames(calls); len(got) != 1 || got[0] != "p1=Muse Mix" {
		t.Errorf("renames %v", got)
	}
}

func TestEnsureMixNeverTakesAnotherSlotsPlaylist(t *testing.T) {
	var calls []string
	c := New(fakeLists(t, lists, &calls), "me")
	// "Blur Mix" belongs to dailymix-2: dailymix-3 wanting the same name this
	// week must get a playlist of its own.
	id, err := c.EnsureMix("dailymix-3", "Blur Mix", "Daily Mix 3")
	if err != nil || id != "new" {
		t.Fatalf("got %q, %v; want a new playlist", id, err)
	}
}

func TestEnsureMixAdoptsALegacyNameWithoutAMachineLine(t *testing.T) {
	var calls []string
	c := New(fakeLists(t, lists, &calls), "me")
	id, err := c.EnsureMix("artistradio-1", "Muse Radio", "Artist Radio 1", "\U0001F7E0 Artist Radio 1")
	if err != nil || id != "p3" {
		t.Fatalf("got %q, %v; want p3 adopted by its old name", id, err)
	}
	// Unchanged name: no write at all.
	calls = nil
	if _, err := c.EnsureMix("dailymix-2", "Blur Mix"); err != nil || len(renames(calls)) != 0 {
		t.Errorf("an unchanged name was rewritten: %v", renames(calls))
	}
}
