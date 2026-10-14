package library

import (
	"net/url"
	"strconv"

	"github.com/nenadjokic/navibeat-mixes/internal/mixes"
)

// SimilarSongs asks Navidrome for songs like one track (getSimilarSongs2), the
// engine behind its own Instant Mix: the configured agents (Last.fm, Deezer,
// AudioMuse-AI...) name similar artists, and Navidrome answers with their
// songs from THIS library, so everything it returns can be played.
func (c *Client) SimilarSongs(id string, count int) ([]mixes.Track, error) {
	env, err := c.do("getSimilarSongs2", url.Values{"id": {id}, "count": {strconv.Itoa(count)}})
	if err != nil {
		return nil, err
	}
	songs := env.Response.SimilarSongs2.Song
	out := make([]mixes.Track, 0, len(songs))
	for _, s := range songs {
		out = append(out, mixes.Track{
			ID:         s.ID,
			Title:      s.Title,
			Artist:     s.Artist,
			Genre:      s.Genre,
			Genres:     s.genreNames(),
			Year:       s.Year,
			PlayCount:  s.PlayCount,
			LastPlayed: parseTime(s.Played),
			Added:      parseTime(s.Created),
			Starred:    s.Starred != "",
		})
	}
	return out, nil
}
