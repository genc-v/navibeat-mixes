package library

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// RawCaller performs a Subsonic request whose answer is binary, getCoverArt
// here, and returns the content type with the bytes.
type RawCaller func(uri string) (contentType string, data []byte, err error)

// PlaylistEntry is one track of a playlist as getPlaylist returns it, kept to
// the fields a cover is drawn from.
type PlaylistEntry struct {
	ID       string `json:"id"`
	Artist   string `json:"artist"`
	ArtistID string `json:"artistId"`
	AlbumID  string `json:"albumId"`
}

// WithRaw gives the client the binary caller it needs for CoverArt, and
// returns it so construction stays one expression.
func (c *Client) WithRaw(raw RawCaller) *Client {
	c.raw = raw
	return c
}

// PlaylistEntries reads back the tracks of one playlist, in order.
//
// Read back rather than taken from the selection that was just written,
// because the selection only carries track ids: the artist and album ids a
// cover needs come from the server, and asking for the written playlist
// costs one call where looking the tracks up one by one would cost thirty.
func (c *Client) PlaylistEntries(playlistID string) ([]PlaylistEntry, error) {
	env, err := c.do("getPlaylist", url.Values{"id": {playlistID}})
	if err != nil {
		return nil, err
	}
	return env.Response.Playlist.Entry, nil
}

// ErrNotImage is returned by CoverArt when the server answered with
// something other than an image, which is how getCoverArt reports a missing
// id: a Subsonic error document with an HTTP 200.
var ErrNotImage = errors.New("getCoverArt did not return an image")

// CoverArt fetches one artwork id ("ar-...", "al-...") at the given size in
// pixels, 0 for the original.
func (c *Client) CoverArt(id string, size int) ([]byte, error) {
	if c.raw == nil {
		return nil, errors.New("getCoverArt: no binary caller configured")
	}
	params := url.Values{"id": {id}, "u": {c.user}}
	if size > 0 {
		params.Set("size", strconv.Itoa(size))
	}
	contentType, data, err := c.raw("getCoverArt?" + params.Encode())
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, ErrNotImage
	}
	return data, nil
}
