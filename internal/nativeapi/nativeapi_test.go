package nativeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

// fakeServer answers like Navidrome's /auth/login and playlist image upload.
type fakeServer struct {
	password string
	valid    map[string]bool
	logins   int
	uploads  int
	refuse   bool
	expire   bool
	got      []byte
}

func (f *fakeServer) send(r Request) (Response, error) {
	switch {
	case r.URL == "http://nd:4533/auth/login":
		var creds map[string]string
		_ = json.Unmarshal(r.Body, &creds)
		if creds["password"] != f.password {
			return Response{Status: 401, Body: []byte(`{"error":"Invalid username or password"}`)}, nil
		}
		f.logins++
		tok := "tok" + string(rune('0'+f.logins))
		f.valid[tok] = true
		return Response{Status: 200, Body: []byte(`{"token":"` + tok + `","username":"admin"}`)}, nil
	case strings.HasSuffix(r.URL, "/api/playlist/pl%201/image"):
		tok := strings.TrimPrefix(r.Headers["X-ND-Authorization"], "Bearer ")
		if f.expire {
			f.expire = false
			delete(f.valid, tok)
		}
		if !f.valid[tok] {
			return Response{Status: 401}, nil
		}
		if f.refuse {
			return Response{Status: 403, Body: []byte("not authorized")}, nil
		}
		_, params, err := mime.ParseMediaType(r.Headers["Content-Type"])
		if err != nil {
			return Response{Status: 400}, nil
		}
		mr := multipart.NewReader(bytes.NewReader(r.Body), params["boundary"])
		part, err := mr.NextPart()
		if err != nil || part.FormName() != "image" {
			return Response{Status: 400, Body: []byte("missing image file")}, nil
		}
		f.got, _ = io.ReadAll(part)
		f.uploads++
		return Response{Status: 200, Body: []byte(`{"status":"ok"}`)}, nil
	}
	return Response{Status: 404}, nil
}

func newFake() *fakeServer { return &fakeServer{password: "pw", valid: map[string]bool{}} }

func TestUploadLogsInOnceAndSendsTheImageField(t *testing.T) {
	f := newFake()
	c := New("http://nd:4533/", "admin", "pw", f.send)
	for range 3 {
		if err := c.UploadPlaylistImage("pl 1", []byte("JPEGDATA"), "c.jpg"); err != nil {
			t.Fatal(err)
		}
	}
	if f.logins != 1 || f.uploads != 3 {
		t.Errorf("logins %d uploads %d, want 1 and 3", f.logins, f.uploads)
	}
	if string(f.got) != "JPEGDATA" {
		t.Errorf("server got %q", f.got)
	}
}

func TestAnExpiredTokenIsRenewedOnce(t *testing.T) {
	f := newFake()
	c := New("http://nd:4533", "admin", "pw", f.send)
	c.SetToken("stale")
	if err := c.UploadPlaylistImage("pl 1", []byte("x"), "c.jpg"); err != nil {
		t.Fatal(err)
	}
	if f.logins != 1 || c.Token() != "tok1" {
		t.Errorf("logins %d token %q", f.logins, c.Token())
	}
	f.expire = true
	if err := c.UploadPlaylistImage("pl 1", []byte("x"), "c.jpg"); err != nil {
		t.Fatal(err)
	}
	if f.logins != 2 {
		t.Errorf("an expired session was not renewed: %d logins", f.logins)
	}
}

func TestWrongPasswordAndRefusalAreNamedErrors(t *testing.T) {
	f := newFake()
	if err := New("http://nd:4533", "admin", "nope", f.send).UploadPlaylistImage("pl 1", []byte("x"), "c.jpg"); !errors.Is(err, ErrLogin) {
		t.Errorf("wrong password gave %v", err)
	}
	f.refuse = true
	if err := New("http://nd:4533", "admin", "pw", f.send).UploadPlaylistImage("pl 1", []byte("x"), "c.jpg"); !errors.Is(err, ErrForbidden) {
		t.Errorf("a 403 gave %v", err)
	}
}
