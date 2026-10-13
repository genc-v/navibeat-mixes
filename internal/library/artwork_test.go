package library

import (
	"errors"
	"strings"
	"testing"
)

func TestPlaylistEntriesReadsTheArtworkIDs(t *testing.T) {
	c := New(func(uri string) (string, error) {
		if !strings.HasPrefix(uri, "getPlaylist?") || !strings.Contains(uri, "id=pl1") {
			t.Fatalf("unexpected call %s", uri)
		}
		return `{"subsonic-response":{"status":"ok","playlist":{"id":"pl1","entry":[
			{"id":"t1","artist":"Muse","artistId":"ar1","albumId":"al1"},
			{"id":"t2","artist":"Blur","artistId":"ar2","albumId":"al2"}]}}}`, nil
	}, "admin")
	got, err := c.PlaylistEntries("pl1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ArtistID != "ar2" || got[0].AlbumID != "al1" || got[0].Artist != "Muse" {
		t.Errorf("entries %+v", got)
	}
}

func TestCoverArtRefusesAnErrorDocument(t *testing.T) {
	c := New(nil, "admin").WithRaw(func(uri string) (string, []byte, error) {
		if strings.Contains(uri, "id=ar-missing") {
			return "application/json", []byte(`{"subsonic-response":{"status":"failed"}}`), nil
		}
		if !strings.Contains(uri, "size=640") || !strings.Contains(uri, "u=admin") {
			t.Errorf("unexpected call %s", uri)
		}
		return "image/jpeg", []byte("JPEG"), nil
	})
	if data, err := c.CoverArt("ar-1", 640); err != nil || string(data) != "JPEG" {
		t.Errorf("got %q, %v", data, err)
	}
	if _, err := c.CoverArt("ar-missing", 640); !errors.Is(err, ErrNotImage) {
		t.Errorf("an error document gave %v", err)
	}
}
