// coverpreview renders every cover layout to PNG files, so a design change
// can be looked at without building the plugin or running a server.
//
// Usage:
//
//	go run ./dev/coverpreview -art DIR -out DIR
//
// DIR holds the artwork to draw with: artist_<Name>.<ext> files are artist
// photos and album_<anything>.<ext> files are album covers. Without -art, the
// covers are drawn with no artwork at all, which is the fallback every layout
// must also survive. Besides one PNG per cover, the tool writes sheet.png, a
// contact sheet of all of them side by side.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/nenadjokic/navibeat-mixes/internal/config"
	"github.com/nenadjokic/navibeat-mixes/internal/cover"
)

func main() {
	artDir := flag.String("art", "", "folder of artist_*.jpg and album_*.jpg files")
	out := flag.String("out", "covers", "folder to write the PNGs into")
	flag.Parse()

	art := load(*artDir)
	artists, albums := names(art, "artist_"), names(art, "album_")
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}

	// pick returns the n-th artist or album, cycling, or "" when there are none.
	pick := func(list []string, n int) string {
		if len(list) == 0 {
			return ""
		}
		return list[n%len(list)]
	}
	// tracks fakes the entries of a playlist: `count` tracks by the n-th
	// artist across a few albums, then a tail of other artists.
	tracks := func(n int) []cover.Track {
		var ts []cover.Track
		for i := range 8 {
			ts = append(ts, cover.Track{Artist: pick(artists, n), ArtistID: pick(artists, n), AlbumID: pick(albums, n+i%3)})
		}
		for i := range 6 {
			ts = append(ts, cover.Track{Artist: pick(artists, n+1+i%3), ArtistID: pick(artists, n+1+i%3), AlbumID: pick(albums, n+3+i)})
		}
		return ts
	}
	display := func(id string) string { return strings.ReplaceAll(id, "_", " ") }
	base := func(key string) string { _, c := config.Defaults().ButtonFor(key); return c }

	type job struct {
		file string
		mix  cover.Mix
		ts   []cover.Track
	}
	var jobs []job
	for i := 1; i <= 3; i++ {
		jobs = append(jobs, job{fmt.Sprintf("dailymix-%d", i), cover.Mix{Kind: "dailymix", Slot: fmt.Sprintf("dailymix-%d", i),
			Name: fmt.Sprintf("Daily Mix %d", i), Subject: pick(artists, i-1), Base: base("dailymix")}, tracks(i - 1)})
	}
	for i := 1; i <= 2; i++ {
		a := pick(artists, i+2)
		jobs = append(jobs, job{fmt.Sprintf("artistradio-%d", i), cover.Mix{Kind: "artistradio", Slot: fmt.Sprintf("artistradio-%d", i),
			Name: fmt.Sprintf("Artist Radio %d", i), Subject: display(a), Base: base("artistradio")}, rename(tracks(i+2), a, display(a))})
	}
	for i, g := range []string{"Blues", "Jazz"} {
		jobs = append(jobs, job{"genreradio-" + strings.ToLower(g), cover.Mix{Kind: "genreradio", Slot: fmt.Sprintf("genreradio-%d", i+1),
			Name: fmt.Sprintf("Genre Radio %d", i+1), Subject: g, Base: base("genreradio")}, tracks(i + 4)})
	}
	for i, d := range []string{"2000s", "1970s"} {
		jobs = append(jobs, job{"decade-" + d, cover.Mix{Kind: "decade", Slot: "decade-" + strings.TrimSuffix(d, "s"),
			Name: d, Base: base("decade")}, tracks(i + 6)})
	}
	for i, m := range []struct{ kind, name string }{
		{"discovery", "Weekly Discovery"}, {"newmusic", "New Music"}, {"rediscover", "Rediscover"},
		{"onrepeat", "On Repeat"}, {"essentials", "Your Essentials"}, {"loved", "Your Loved Songs"},
		{"wrapped", "Wrapped " + fmt.Sprint(time.Now().Year())},
	} {
		jobs = append(jobs, job{m.kind, cover.Mix{Kind: m.kind, Slot: m.kind, Name: m.name, Base: base(m.kind)}, tracks(i + 1)})
	}
	for i, slot := range []string{"morning", "afternoon", "evening", "night"} {
		jobs = append(jobs, job{slot, cover.Mix{Kind: "timeofday", Slot: slot, Name: strings.ToUpper(slot[:1]) + slot[1:],
			Base: base(slot)}, tracks(i)})
	}

	var rendered []image.Image
	for _, j := range jobs {
		plan := cover.PlanFor(j.mix, j.ts)
		var artist image.Image
		if id := strings.TrimPrefix(plan.Artist, "ar-"); id != "" {
			artist = art["artist_"+id]
		}
		var covers []image.Image
		for _, a := range plan.Albums {
			if img := art["album_"+strings.TrimPrefix(a, "al-")]; img != nil {
				covers = append(covers, img)
			}
		}
		start := time.Now()
		img := cover.Render(plan.Spec(artist, covers))
		jpg, err := cover.EncodeJPEG(img)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%-22s %-9s %4d KB  %v\n", j.file, plan.Layout, len(jpg)/1024, time.Since(start).Round(time.Millisecond))
		write(filepath.Join(*out, j.file+".png"), img)
		rendered = append(rendered, img)
	}
	write(filepath.Join(*out, "sheet.png"), sheet(rendered, 5, 300, 16))
}

// rename gives the tracks of `id` a display name, the way a real playlist
// carries "Arctic Monkeys" and not a file name.
func rename(ts []cover.Track, id, name string) []cover.Track {
	for i := range ts {
		if ts[i].ArtistID == id {
			ts[i].Artist = name
		}
	}
	return ts
}

func load(dir string) map[string]image.Image {
	out := map[string]image.Image{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail(err)
	}
	for _, e := range entries {
		name := e.Name()
		key := strings.TrimSuffix(name, filepath.Ext(name))
		if !strings.HasPrefix(key, "artist_") && !strings.HasPrefix(key, "album_") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fail(err)
		}
		img, err := cover.Decode(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", name, err)
			continue
		}
		out[key] = img
	}
	return out
}

// names lists the ids of one kind of artwork, sorted so a run is repeatable.
func names(art map[string]image.Image, prefix string) []string {
	var out []string
	for k := range art {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(out)
	return out
}

func sheet(imgs []image.Image, cols, tile, gap int) image.Image {
	rows := (len(imgs) + cols - 1) / cols
	w, h := cols*tile+(cols+1)*gap, rows*tile+(rows+1)*gap
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(image.Black), image.Point{}, draw.Src)
	for i, img := range imgs {
		x, y := gap+(i%cols)*(tile+gap), gap+(i/cols)*(tile+gap)
		xdraw.CatmullRom.Scale(dst, image.Rect(x, y, x+tile, y+tile), img, img.Bounds(), xdraw.Src, nil)
	}
	return dst
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
