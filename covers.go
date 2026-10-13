package main

// Covers: the Spotify style cover images (internal/cover) drawn for every mix
// right after it is written, and uploaded through Navidrome's own API so
// every client shows them. See internal/nativeapi for why the upload needs a
// login rather than a host service.

import (
	"errors"
	"image"
	"strings"
	"time"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"

	"github.com/nenadjokic/navibeat-mixes/internal/config"
	"github.com/nenadjokic/navibeat-mixes/internal/cover"
	"github.com/nenadjokic/navibeat-mixes/internal/library"
	"github.com/nenadjokic/navibeat-mixes/internal/nativeapi"
)

const (
	// The session token is kept between calls, so a continuation chain of a
	// dozen calls logs in once rather than a dozen times. Navidrome's own
	// sessions last longer than this.
	coverTokenTTLSec = 6 * 60 * 60
	// Artwork sizes fetched from getCoverArt: the photo fills the whole
	// cover, an album at most half of it.
	photoSize = 640
	albumSize = 400
)

// coverRun is one generating call's cover state. The plugin is one WASM
// instance serving one call at a time, so a package variable is the whole of
// it, the same reasoning as runningScheduleID.
type coverRun struct {
	api *nativeapi.Client
	key string
	// stopped is a login or permission failure. One wrong password is one
	// log line per call, not one per playlist.
	stopped error
}

var covers *coverRun

// beginCovers sets up the cover state for one generating call, or leaves it
// nil when covers are off or have no login to upload with.
func beginCovers(cfg config.Config) {
	covers = nil
	if !cfg.Covers {
		return
	}
	if !cfg.CoversReady() {
		logf("covers are on but no Navidrome login is set (Covers in the plugin settings), so no cover is drawn")
		return
	}
	r := &coverRun{
		api: nativeapi.New(cfg.CoverServer, cfg.CoverUser, cfg.CoverPassword, sendHTTP),
		key: "cover:token:" + cfg.CoverUser + "@" + cfg.CoverServer,
	}
	if tok, ok, err := host.KVStoreGet(r.key); err == nil && ok {
		r.api.SetToken(string(tok))
	}
	covers = r
}

// endCovers keeps a fresh token for the next call.
func endCovers() {
	if covers != nil && covers.api.Token() != "" {
		_ = host.KVStoreSetWithTTL(covers.key, []byte(covers.api.Token()), coverTokenTTLSec)
	}
	covers = nil
}

func sendHTTP(r nativeapi.Request) (nativeapi.Response, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: r.Method, URL: r.URL, Headers: r.Headers, Body: r.Body, TimeoutMs: 8000,
	})
	if err != nil {
		return nativeapi.Response{}, err
	}
	return nativeapi.Response{Status: int(resp.StatusCode), Body: resp.Body}, nil
}

func coverKey(playlistID string) string { return "cover:" + playlistID }

// ensureCover draws and uploads the cover of one playlist that was just
// written, unless the one it already has would come out the same.
//
// Never fatal: a cover that cannot be made leaves the playlist with whatever
// image it had, and the mix itself is already written.
func ensureCover(client *library.Client, cfg config.Config, e entry, playlistID, username string) {
	if covers == nil || covers.stopped != nil {
		return
	}
	start := wallNow()
	entries, err := client.PlaylistEntries(playlistID)
	if err != nil {
		logf("user %s: cover for %s: reading the playlist back: %v", username, e.name, err)
		return
	}
	tracks := make([]cover.Track, len(entries))
	for i, t := range entries {
		tracks[i] = cover.Track{Artist: t.Artist, ArtistID: t.ArtistID, AlbumID: t.AlbumID}
	}
	colorKey := e.kind
	if e.kind == "timeofday" {
		colorKey = e.slot
	}
	override, base := cfg.CoverColors(colorKey)
	plan := cover.PlanFor(cover.Mix{
		Kind: e.kind, Slot: e.slot, Subject: e.subject,
		Name:   strings.TrimSpace(strings.TrimPrefix(e.name, cfg.Prefix)),
		Accent: override, Base: base,
	}, tracks)

	// "done" is a cover drawn with everything it asked for; "partial" is one
	// drawn while the artist photo was still Navidrome's placeholder, and is
	// drawn again on the next run in case the photo has arrived since.
	sig := plan.Signature()
	if prev, ok, err := host.KVStoreGet(coverKey(playlistID)); err == nil && ok && string(prev) == sig+"|done" {
		return
	}
	var photo image.Image
	partial := false
	if plan.Artist != "" {
		if photo = fetchArt(client, plan.Artist, photoSize); photo == nil {
			partial = true
		}
	}
	var albums []image.Image
	for _, id := range plan.Albums {
		if img := fetchArt(client, id, albumSize); img != nil {
			albums = append(albums, img)
		}
	}
	jpg, err := cover.EncodeJPEG(cover.Render(plan.Spec(photo, albums)))
	if err != nil {
		logf("user %s: cover for %s: encoding: %v", username, e.name, err)
		return
	}
	if err := covers.api.UploadPlaylistImage(playlistID, jpg, "navibeat-cover.jpg"); err != nil {
		if errors.Is(err, nativeapi.ErrLogin) || errors.Is(err, nativeapi.ErrForbidden) {
			covers.stopped = err
			logf("covers stopped for this run: %v", err)
			return
		}
		logf("user %s: cover for %s: %v", username, e.name, err)
		return
	}
	state := "|done"
	if partial {
		state = "|partial"
	}
	if err := host.KVStoreSet(coverKey(playlistID), []byte(sig+state)); err != nil {
		logf("user %s: cover for %s uploaded but not remembered: %v", username, e.name, err)
	}
	logf("user %s: cover for %s uploaded (%s, %d KB, %v)", username, e.name, string(plan.Layout),
		len(jpg)/1024, wallNow().Sub(start).Round(time.Millisecond))
}

// fetchArt reads one artwork id, or nil when the server has none: an error,
// something that is not an image, or Navidrome's placeholder.
func fetchArt(client *library.Client, id string, size int) image.Image {
	data, err := client.CoverArt(id, size)
	if err != nil || cover.IsPlaceholder(data) {
		return nil
	}
	img, err := cover.Decode(data)
	if err != nil {
		return nil
	}
	return img
}
