package cover

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// solid is a flat test image.
func solid(c color.RGBA, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, c)
	return img
}

var everyLayout = []Layout{LayoutDaily, LayoutRadio, LayoutBand, LayoutPoster, LayoutEditorial, LayoutHorizon}

// Every layout must draw with its artwork, without any, and with a long
// title, and never come out blank. A panic here is a playlist with no cover
// on a real server, so this covers the fallbacks as much as the happy path.
func TestEveryLayoutRendersWithAndWithoutArtwork(t *testing.T) {
	photo := solid(color.RGBA{200, 60, 40, 255}, 300, 400)
	albums := []image.Image{
		solid(color.RGBA{30, 120, 200, 255}, 200, 200), solid(color.RGBA{240, 200, 40, 255}, 200, 200),
		solid(color.RGBA{40, 180, 90, 255}, 200, 200), solid(color.RGBA{120, 40, 160, 255}, 200, 200),
	}
	for _, layout := range everyLayout {
		for _, withArt := range []bool{true, false} {
			for _, title := range []string{"Daily Mix", "Progressive Psychedelic Space Rock Mix"} {
				s := Spec{Layout: layout, Size: 320, Title: title, Tag: "01", Accent: Brand, Variant: 2}
				if withArt {
					s.Artist, s.Albums = photo, albums
				}
				img := Render(s)
				if img.Bounds() != image.Rect(0, 0, 320, 320) {
					t.Fatalf("%s: bounds %v", layout, img.Bounds())
				}
				if flat(img) {
					t.Errorf("%s (art %v, %q): came out one flat colour", layout, withArt, title)
				}
				if _, err := EncodeJPEG(img); err != nil {
					t.Errorf("%s: encode: %v", layout, err)
				}
			}
		}
	}
}

func flat(img *image.RGBA) bool {
	first := img.RGBAAt(0, 0)
	for y := 0; y < img.Rect.Dy(); y += 7 {
		for x := 0; x < img.Rect.Dx(); x += 7 {
			if img.RGBAAt(x, y) != first {
				return false
			}
		}
	}
	return true
}

// The same playlist must render the same bytes every night, or every run
// would upload a "new" cover and every client would download it again.
func TestRenderIsDeterministic(t *testing.T) {
	for _, layout := range everyLayout {
		s := Spec{Layout: layout, Size: 200, Title: "On Repeat", Tag: "02", Accent: Brand, Variant: 7,
			Albums: []image.Image{solid(color.RGBA{10, 200, 90, 255}, 50, 50)}}
		a, b := Render(s), Render(s)
		if string(a.Pix) != string(b.Pix) {
			t.Errorf("%s renders differently on two calls", layout)
		}
	}
}

func TestPlanForPicksTheLayoutAndWordsOfEachMix(t *testing.T) {
	tracks := []Track{
		{"Muse", "ar1", "alA"}, {"Muse", "ar1", "alA"}, {"Muse", "ar1", "alB"},
		{"Arctic Monkeys", "ar2", "alC"}, {"Arctic Monkeys", "ar2", "alC"}, {"Arctic Monkeys", "ar2", "alC"},
		{"Arctic Monkeys", "ar2", "alD"},
	}
	cases := []struct {
		mix                     Mix
		layout                  Layout
		title, tag, artist, alb string
	}{
		{Mix{Kind: "dailymix", Slot: "dailymix-2", Name: "Muse Mix", Subject: "Muse"}, LayoutDaily, "Muse Mix", "", "ar-ar1", "al-alA"},
		{Mix{Kind: "artistradio", Slot: "artistradio-1", Name: "Artist Radio 1", Subject: "Muse"}, LayoutRadio, "Muse", "RADIO", "ar-ar1", "al-alA"},
		{Mix{Kind: "genreradio", Slot: "genreradio-1", Name: "Genre Radio 1", Subject: "Blues"}, LayoutBand, "Blues Mix", "", "ar-ar2", "al-alC"},
		{Mix{Kind: "decade", Slot: "decade-2000", Name: "2000s Mix", Subject: "2000s"}, LayoutBand, "2000s Mix", "", "ar-ar2", "al-alC"},
		{Mix{Kind: "onrepeat", Slot: "onrepeat", Name: "On Repeat"}, LayoutEditorial, "On Repeat", "", "ar-ar2", "al-alC"},
		{Mix{Kind: "discovery", Slot: "discovery", Name: "Weekly Discovery"}, LayoutPoster, "Weekly Discovery", "", "", "al-alC"},
		{Mix{Kind: "timeofday", Slot: "morning", Name: "Jutro"}, LayoutHorizon, "Jutro", "", "", "al-alC"},
	}
	for _, c := range cases {
		p := PlanFor(c.mix, tracks)
		if p.Layout != c.layout || p.Title != c.title || p.Tag != c.tag || p.Artist != c.artist {
			t.Errorf("%s: got %s %q %q %q, want %s %q %q %q", c.mix.Slot, p.Layout, p.Title, p.Tag, p.Artist,
				c.layout, c.title, c.tag, c.artist)
		}
		if len(p.Albums) == 0 || p.Albums[0] != c.alb {
			t.Errorf("%s: first album %v, want %s", c.mix.Slot, p.Albums, c.alb)
		}
	}
	// The time of day keeps its own sky and sun.
	if p := PlanFor(Mix{Kind: "timeofday", Slot: "night", Name: "Night"}, tracks); p.Sky[0] == (color.RGBA{}) || p.Sun[2] == 0 {
		t.Errorf("night has no sky or sun: %+v", p)
	}
}

func TestSignatureMovesOnlyWithTheCover(t *testing.T) {
	tracks := []Track{{"Muse", "ar1", "alA"}, {"Muse", "ar1", "alB"}}
	mix := Mix{Kind: "dailymix", Slot: "dailymix-1", Name: "Daily Mix 1", Subject: "Muse", Base: "9B6BD6"}
	a := PlanFor(mix, tracks).Signature()
	if b := PlanFor(mix, tracks).Signature(); a != b {
		t.Fatalf("signature not stable: %q vs %q", a, b)
	}
	// Same artwork, tracks shuffled: no new cover.
	if b := PlanFor(mix, []Track{tracks[1], tracks[0]}).Signature(); a != b {
		t.Errorf("a reorder with the same artwork changed the signature")
	}
	// A different artist at the centre: a new cover.
	other := []Track{{"Blur", "ar9", "alZ"}, {"Blur", "ar9", "alZ"}}
	mix.Subject = "Blur"
	if b := PlanFor(mix, other).Signature(); a == b {
		t.Errorf("a new subject kept the old signature")
	}
	// The owner choosing a colour: a new cover.
	mix.Subject, mix.Accent = "Muse", "#FF0000"
	if b := PlanFor(mix, tracks).Signature(); a == b {
		t.Errorf("a fixed colour kept the old signature")
	}
}

func TestSubjectFallsBackToTheTopArtist(t *testing.T) {
	tracks := []Track{{"Blur", "ar9", "alZ"}, {"Blur", "ar9", "alZ"}, {"Muse", "ar1", "alA"}}
	if p := PlanFor(Mix{Kind: "artistradio", Slot: "artistradio-1", Subject: "Nobody"}, tracks); p.Artist != "ar-ar9" {
		t.Errorf("artist %q, want the most frequent one", p.Artist)
	}
}

func TestIsPlaceholderKnowsNavidromesImagesOnly(t *testing.T) {
	if IsPlaceholder([]byte("not an image")) {
		t.Error("random bytes taken for a placeholder")
	}
}

func TestHex(t *testing.T) {
	for in, ok := range map[string]bool{"F28C28": true, "#f28c28": true, " 00ff00 ": true, "F2C": false, "zzzzzz": false, "": false} {
		if _, got := Hex(in); got != ok {
			t.Errorf("Hex(%q) ok = %v, want %v", in, got, ok)
		}
	}
}

func TestDominantIgnoresGrey(t *testing.T) {
	if _, ok := Dominant(solid(color.RGBA{128, 128, 128, 255}, 40, 40)); ok {
		t.Error("a grey image produced a dominant colour")
	}
	c, ok := Dominant(solid(color.RGBA{220, 40, 40, 255}, 40, 40))
	if !ok || c.R < 200 || c.G > 60 {
		t.Errorf("red image gave %v, %v", c, ok)
	}
}

func TestTrimFrameRemovesAWhiteBorderOnly(t *testing.T) {
	img := solid(color.RGBA{250, 250, 250, 255}, 100, 100)
	inner := image.Rect(5, 5, 95, 95)
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for x := inner.Min.X; x < inner.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{20, 30, 40, 255})
		}
	}
	if got := trimFrame(img); !got.In(inner.Inset(-1)) || got.Dx() < 85 {
		t.Errorf("framed image trimmed to %v, want about %v", got, inner)
	}
	// A pale sky along the top only is a photo, not a frame.
	sky := solid(color.RGBA{20, 30, 40, 255}, 100, 100)
	for y := 0; y < 6; y++ {
		for x := 0; x < 100; x++ {
			sky.SetRGBA(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	if got := trimFrame(sky); got != sky.Bounds() {
		t.Errorf("one light edge was trimmed to %v", got)
	}
}

func TestSplitLines(t *testing.T) {
	for in, want := range map[string]string{
		"NEW MUSIC":              "NEW|MUSIC",
		"REDISCOVER":             "REDISCOVER",
		"YOUR LOVED SONGS":       "YOUR|LOVED|SONGS",
		"MY VERY OWN FAVOURITES": "MY VERY OWN|FAVOURITES",
	} {
		if got := strings.Join(splitLines(in), "|"); got != want {
			t.Errorf("splitLines(%q) = %q, want %q", in, got, want)
		}
	}
}
