// Package nativeapi uploads playlist covers through Navidrome's own REST API.
//
// The Subsonic API has no way to set a playlist's image, and Navidrome's
// plugin host services can read artwork URLs but never write one (checked
// against plugins/host in 0.64.2). The web UI's upload endpoint,
// POST /api/playlist/{id}/image, is the one way in, and it needs a logged in
// user: an admin, or the owner of the playlist. So the plugin logs in as the
// account its owner configured, over the http permission, on loopback.
//
// Like internal/library, this package stays free of the plugin SDK: the HTTP
// call is injected, so it is tested as ordinary Go.
package nativeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
)

// Request is one HTTP call.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// Response is what came back. A 4xx or 5xx is a Response, not an error.
type Response struct {
	Status int
	Body   []byte
}

// Sender performs one HTTP call. Errors are transport failures only.
type Sender func(Request) (Response, error)

// ErrForbidden means the server refused the upload for this account: it is
// neither an admin nor the playlist's owner, or artwork upload is disabled.
var ErrForbidden = errors.New("navidrome refused the upload (the account must be an admin or own the playlist, and EnableArtworkUpload must be on)")

// ErrLogin means the configured username or password was rejected.
var ErrLogin = errors.New("navidrome rejected the configured username or password")

// Client talks to one Navidrome as one user.
type Client struct {
	base  string
	user  string
	pass  string
	send  Sender
	token string
}

// New returns a client for the server at base (scheme, host, port and any
// BaseURL path, e.g. "http://127.0.0.1:4533/music").
func New(base, user, pass string, send Sender) *Client {
	return &Client{base: strings.TrimRight(strings.TrimSpace(base), "/"), user: user, pass: pass, send: send}
}

// Token is the session token, "" before the first login. Exposed so the
// caller can keep it between plugin calls instead of logging in on each.
func (c *Client) Token() string { return c.token }

// SetToken restores a token kept from an earlier call.
func (c *Client) SetToken(t string) { c.token = t }

// Login trades the configured credentials for a session token.
func (c *Client) Login() error {
	body, err := json.Marshal(map[string]string{"username": c.user, "password": c.pass})
	if err != nil {
		return err
	}
	resp, err := c.send(Request{
		Method:  "POST",
		URL:     c.base + "/auth/login",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if resp.Status == 401 || resp.Status == 403 {
		return ErrLogin
	}
	if resp.Status != 200 {
		return fmt.Errorf("login: HTTP %d: %s", resp.Status, snippet(resp.Body))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil || out.Token == "" {
		return fmt.Errorf("login: no token in the answer: %s", snippet(resp.Body))
	}
	c.token = out.Token
	return nil
}

// UploadPlaylistImage sets a playlist's cover. It logs in first when it has
// no token, and once more when the token it had has expired.
func (c *Client) UploadPlaylistImage(playlistID string, image []byte, filename string) error {
	if c.token == "" {
		if err := c.Login(); err != nil {
			return err
		}
	}
	body, contentType, err := imageForm(image, filename)
	if err != nil {
		return err
	}
	upload := func() (Response, error) {
		return c.send(Request{
			Method: "POST",
			URL:    c.base + "/api/playlist/" + url.PathEscape(playlistID) + "/image",
			Headers: map[string]string{
				"Content-Type":       contentType,
				"X-ND-Authorization": "Bearer " + c.token,
			},
			Body: body,
		})
	}
	resp, err := upload()
	if err == nil && resp.Status == 401 {
		// A kept token outlives its session sooner or later; one fresh login
		// is the whole of the recovery.
		if err := c.Login(); err != nil {
			return err
		}
		resp, err = upload()
	}
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	switch resp.Status {
	case 200:
		return nil
	case 403:
		return ErrForbidden
	default:
		return fmt.Errorf("upload: HTTP %d: %s", resp.Status, snippet(resp.Body))
	}
}

// imageForm builds the multipart body the upload endpoint reads: a single
// file field named "image", which is what Navidrome's handleImageUpload asks
// r.FormFile for.
func imageForm(image []byte, filename string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, filename))
	h.Set("Content-Type", "image/jpeg")
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(image); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// snippet keeps an error body short enough for a log line.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}
