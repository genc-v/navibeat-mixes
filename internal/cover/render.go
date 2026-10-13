// Package cover draws Spotify style playlist covers for the mixes: artist
// photos and album art from the mix itself, a colour taken from that artwork,
// and the mix's name set large over it, in six designs modelled on the ones a
// streaming service uses for its own generated playlists.
//
// It is pure Go with no plugin SDK, for the same reason internal/library is:
// so every layout can be rendered and looked at off target. dev/coverpreview
// writes them all to PNG files.
//
// Navidrome shows an uploaded playlist image to EVERY client, so unlike the
// NaviBeat app's own generated artwork, these covers appear in the web UI,
// Symfonium, Feishin, Substreamer and anything else that asks getCoverArt.
package cover

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	_ "image/gif" // decoders for whatever getCoverArt hands back
	"image/jpeg"
	_ "image/png"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// DefaultSize is the side of a cover in pixels: what Spotify serves, and
// more than any client draws a playlist tile at.
const DefaultSize = 640

// Layout is one of the cover designs.
type Layout string

const (
	// LayoutDaily is a full bleed artist photo with two neon tags, "Daily
	// Mix" and the number: Daily Mix 1 to 3.
	LayoutDaily Layout = "daily"
	// LayoutRadio is three circles on a flat colour, the artist in the middle
	// and their albums cut off at the edges, "RADIO" in the corner: Artist
	// Radio.
	LayoutRadio Layout = "radio"
	// LayoutBand is a colour graded photo with a bright band carrying the
	// name: Genre Radio and the decade mixes ("Blues Mix", "2000s Mix").
	LayoutBand Layout = "band"
	// LayoutPoster is huge condensed type between slanted coloured blocks on
	// a dark ground: Weekly Discovery, New Music, Rediscover.
	LayoutPoster Layout = "poster"
	// LayoutEditorial is a duotone photo under a giant retro title: On
	// Repeat, Your Essentials, Loved, Wrapped.
	LayoutEditorial Layout = "editorial"
	// LayoutHorizon is the mix's albums packed into a sun (or moon) standing
	// over a horizon at the height of its time of day, the name set in a
	// serif italic on the ground: Morning, Afternoon, Evening, Night.
	LayoutHorizon Layout = "horizon"
)

// Brand is the NaviBeat orange, the circle every mix name starts with. It
// sits in the top left of every cover the way a streaming service's logo
// sits on its own playlists.
var Brand = color.RGBA{0xF2, 0x8C, 0x28, 255}

// Spec is one cover, with its artwork already decoded.
type Spec struct {
	Layout Layout
	Size   int
	// Title is the name set on the cover.
	Title string
	// Tag is the second piece of type a layout carries: the number on a
	// Daily Mix ("01"), the word in a radio's corner ("RADIO").
	Tag string
	// Accent is the colour the cover is built around. Unless Fixed, it is only
	// the fallback: the artwork's own dominant colour wins, which is what makes
	// two Artist Radios different colours without anybody choosing.
	Accent color.RGBA
	Fixed  bool
	// Horizon only: the sky, top then bottom. When both are set they win over
	// the accent, so each time of day keeps its own light whatever the albums
	// inside look like.
	Sky [2]color.RGBA
	// Horizon only: where the sun stands, the centre as fractions of the
	// side, and its diameter. Zero means the middle of the sky.
	Sun [3]float64
	// Variant varies the details between siblings (the tag colour of Daily
	// Mix 2, where the poster's blocks fall) so they are not one cover three
	// times. It must be stable per playlist, or the cover changes every night.
	Variant int
	// Artist is the artist photo, nil when there is none.
	Artist image.Image
	// Albums are album covers in order of importance.
	Albums []image.Image
}

// Render draws s.
func Render(s Spec) *image.RGBA {
	if s.Size <= 0 {
		s.Size = DefaultSize
	}
	dst := newCanvas(s.Size)
	accent := s.accent()
	switch s.Layout {
	case LayoutDaily:
		renderDaily(dst, s, accent)
	case LayoutRadio:
		renderRadio(dst, s, accent)
	case LayoutBand:
		renderBand(dst, s, accent)
	case LayoutPoster:
		renderPoster(dst, s, accent)
	case LayoutEditorial:
		renderEditorial(dst, s, accent)
	default:
		renderHorizon(dst, s, accent)
	}
	return dst
}

// accent decides the colour: a fixed one, else the artwork's, else the
// fallback.
func (s Spec) accent() color.RGBA {
	if s.Fixed {
		return s.Accent
	}
	if c, ok := Dominant(s.Artist); ok {
		return c
	}
	for _, a := range s.Albums {
		if c, ok := Dominant(a); ok {
			return c
		}
	}
	return s.Accent
}

// photo is the image a photo layout uses: the artist, else the first album.
func (s Spec) photo() image.Image {
	if s.Artist != nil {
		return s.Artist
	}
	if len(s.Albums) > 0 {
		return s.Albums[0]
	}
	return nil
}

func (s Spec) album(i int) image.Image {
	if i < len(s.Albums) {
		return s.Albums[i]
	}
	return nil
}

var (
	white = color.RGBA{255, 255, 255, 255}
	black = color.RGBA{0, 0, 0, 255}
	ink   = color.RGBA{18, 18, 18, 255}
)

// neon is the tag palette of the Daily Mix covers: one colour per number,
// both tags in it, the way the reference covers do it.
var neon = []color.RGBA{
	{0x45, 0xE3, 0xEE, 255}, // cyan
	{0xF3, 0xEE, 0x4C, 255}, // yellow
	{0xF2, 0x5E, 0x47, 255}, // coral
	{0xF4, 0xA6, 0xE4, 255}, // pink
	{0x5C, 0xE4, 0x86, 255}, // green
	{0xB8, 0xA8, 0xF6, 255}, // lavender
}

// brandDot draws the orange circle in the top left.
func brandDot(dst *image.RGBA) {
	S := float64(dst.Bounds().Dx())
	d := int(math.Round(S * 0.052))
	at := int(math.Round(S * 0.045))
	fillMasked(dst, image.Rect(at, at, at+d, at+d), Brand, circleMask(d))
}

func renderDaily(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	full := dst.Bounds()
	tag := neon[pmod(s.Variant, len(neon))]
	if p := s.photo(); p != nil {
		place(dst, full, p)
		// Three treatments in turn, so three Daily Mixes side by side read as
		// a set rather than three snapshots: natural, a duotone in the tag's
		// colour, and black and white.
		switch pmod(s.Variant, 3) {
		case 1:
			duotone(dst, full, tone(tag, 0.13, 0.5), tone(tag, 0.8, 0.9), 0.9)
		case 2:
			duotone(dst, full, color.RGBA{12, 12, 12, 255}, color.RGBA{238, 238, 232, 255}, 1)
		default:
			tint(dst, full, tone(accent, 0.4, 0.4), 0.08)
		}
	} else {
		gradient(dst, full, tone(tag, 0.55, 0.6), tone(shiftHue(tag, 30), 0.2, 0.5), 60)
	}
	veil(dst, image.Rect(0, int(S*0.6), s.Size, s.Size), black, 0, 0.35)
	veil(dst, image.Rect(0, 0, s.Size, int(S*0.2)), black, 0.25, 0)

	// The two tags share a bottom edge; the number is the bigger box.
	edge := S * 0.05
	bottom := S - edge
	name := strings.TrimSpace(s.Title)
	if name == "" {
		name = "Daily Mix"
	}
	nameFace := face(heavy, int(S*0.07))
	padX, padY := S*0.02, S*0.018
	nameW := measure(nameFace, name, 0)
	if s.Tag != "" {
		numFace := face(heavy, int(S*0.13))
		numTrack := -S * 0.003
		numW := measure(numFace, s.Tag, numTrack)
		numBox := image.Rect(int(S-edge-numW-2*padX), int(bottom-capHeight(numFace)-2*padY*1.15), int(S-edge), int(bottom))
		fillRect(dst, numBox, tag)
		text(dst, numFace, s.Tag, float64(numBox.Min.X)+padX, bottom-padY*1.15, numTrack, ink, 0)
		// Room left for the name is whatever the number did not take.
		nameFace, nameW = shrinkToFit(heavy, name, float64(numBox.Min.X)-edge-S*0.03-2*padX, int(S*0.07), int(S*0.045))
	}
	nameBox := image.Rect(int(edge), int(bottom-capHeight(nameFace)-2*padY), int(edge+nameW+2*padX), int(bottom))
	fillRect(dst, nameBox, tag)
	text(dst, nameFace, ellipsize(nameFace, name, nameW, 0), edge+padX, bottom-padY, 0, ink, 0)
	brandDot(dst)
}

// shrinkToFit returns the largest face from start down to floor at which s
// is no wider than maxW, and the width at that size.
func shrinkToFit(w weight, s string, maxW float64, start, floor int) (fontFace, float64) {
	for size := start; size > floor; size -= 2 {
		f := face(w, size)
		if width := measure(f, s, 0); width <= maxW {
			return f, width
		}
	}
	f := face(w, floor)
	return f, math.Min(measure(f, s, 0), maxW)
}

func renderRadio(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	bg := tone(accent, 0.66, 0.62)
	fill(dst, bg)
	fg := textOn(bg)

	// Packed tight: the side circles tuck behind the middle one and run off
	// the edges, so the three read as one group rather than three tiles.
	cy := S * 0.40
	side := int(S * 0.46)
	mid := int(S * 0.56)
	circle := func(cx float64, d int, img image.Image, ring bool) {
		r := image.Rect(int(cx)-d/2, int(cy)-d/2, int(cx)-d/2+d, int(cy)-d/2+d)
		if ring {
			// A ring of the background colour, so the middle circle cuts
			// cleanly into the two behind it.
			g := int(S * 0.014)
			rr := r.Inset(-g)
			fillMasked(dst, rr, bg, circleMask(rr.Dx()))
		}
		m := circleMask(d)
		if img != nil {
			placeMasked(dst, r, img, m)
		} else {
			fillMasked(dst, r, tone(accent, 0.8, 0.5), m)
		}
	}
	// The middle is the artist; the sides are two of their albums, or the
	// next two albums when there is no photo.
	left, right := s.album(0), s.album(1)
	if s.Artist == nil {
		left, right = s.album(1), s.album(2)
	}
	circle(S*0.13, side, left, false)
	circle(S*0.87, side, right, false)
	circle(S*0.5, mid, s.photo(), true)

	if s.Tag != "" {
		f := face(heavy, int(S*0.04))
		tr := S * 0.004
		w := measure(f, s.Tag, tr)
		text(dst, f, s.Tag, S-S*0.055-w, S*0.045+S*0.052/2+capHeight(f)/2, tr, fg, 0)
	}
	m := S * 0.06
	titleBlock(dst, s.Title, m, S-m, S-2*m, 2, fg, 0, heavy, 0.095, 0.085, 0.075, 0.065, 0.055)
	brandDot(dst)
}

func renderBand(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	full := dst.Bounds()
	if p := s.photo(); p != nil {
		place(dst, full, p)
		// A warm grade in the photo's own colour, the look of a mix cover
		// rather than of a press shot.
		duotone(dst, full, tone(accent, 0.1, 0.45), tone(shiftHue(accent, 10), 0.86, 0.6), 0.35)
	} else {
		gradient(dst, full, tone(accent, 0.5, 0.5), tone(shiftHue(accent, 24), 0.22, 0.5), 60)
	}
	veil(dst, image.Rect(0, 0, s.Size, int(S*0.2)), black, 0.3, 0)
	veil(dst, image.Rect(0, int(S*0.55), s.Size, s.Size), black, 0, 0.3)

	band := tone(shiftHue(accent, 160), 0.7, 0.75)
	if s.Fixed {
		band = tone(accent, 0.7, 0.75)
	}
	top, bottom := int(S*0.775), int(S*0.905)
	// The narrow upright under the band's left end, the one detail that
	// makes it read as a label stuck on the photo rather than a stripe.
	fillRect(dst, image.Rect(int(S*0.042), top, int(S*0.068), s.Size), tone(band, 0.42, 0.6))
	fillRect(dst, image.Rect(0, top, s.Size, bottom), band)
	f, w := shrinkToFit(heavy, s.Title, S*0.84, int(S*0.072), int(S*0.045))
	label := ellipsize(f, s.Title, w, 0)
	text(dst, f, label, S*0.088, float64(top+bottom)/2+capHeight(f)/2, 0, textOn(band), 0)
	brandDot(dst)
}

func renderPoster(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	ground := tone(accent, 0.07, 0.35)
	fill(dst, ground)
	a := tone(accent, 0.56, 0.85)
	b := tone(shiftHue(accent, -55), 0.6, 0.8)

	// Four slanted blocks running off the corners, placed by a generator
	// seeded with the variant so the same playlist always gets the same
	// arrangement. Only the corners: the middle belongs to the type.
	rng := newRand(uint32(s.Variant)*2654435761 + 17)
	type area struct{ x0, y0, x1, y1, w0, w1, h0, h1 float64 }
	areas := []area{
		{0.08, -0.1, 0.3, -0.04, 0.3, 0.42, 0.13, 0.18},  // top left, clear of the dot
		{0.62, -0.1, 0.8, 0.0, 0.3, 0.42, 0.12, 0.18},    // top right
		{-0.14, 0.86, 0.1, 0.94, 0.3, 0.42, 0.12, 0.18},  // bottom left
		{0.64, 0.88, 0.84, 0.96, 0.3, 0.42, 0.12, 0.18},  // bottom right
	}
	for i, e := range areas {
		x := (e.x0 + (e.x1-e.x0)*rng.float()) * S
		y := (e.y0 + (e.y1-e.y0)*rng.float()) * S
		w := (e.w0 + (e.w1-e.w0)*rng.float()) * S
		h := (e.h0 + (e.h1-e.h0)*rng.float()) * S
		from, to := a, b
		if i%2 == 1 {
			from, to = b, a
		}
		slab(dst, x, y, w, h, h*0.55, from, to)
	}

	displayTitle(dst, strings.ToUpper(s.Title), S*0.5, S*0.5, S*0.84, S*0.62, white, 0.55, true)
	brandDot(dst)
}

func renderEditorial(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	full := dst.Bounds()
	pop := tone(shiftHue(accent, 180), 0.64, 0.95)
	if p := s.photo(); p != nil {
		place(dst, full, p)
		duotone(dst, full, tone(accent, 0.1, 0.6), tone(shiftHue(accent, 25), 0.82, 0.7), 1)
	} else {
		gradient(dst, full, tone(accent, 0.5, 0.6), tone(shiftHue(accent, 30), 0.15, 0.6), 70)
	}
	veil(dst, image.Rect(0, int(S*0.4), s.Size, s.Size), black, 0, 0.72)
	veil(dst, image.Rect(0, 0, s.Size, int(S*0.2)), black, 0.25, 0)

	// Two sparkles and no more: one high on the right, one beside the title.
	rng := newRand(uint32(s.Variant)*2246822519 + 3)
	star(dst, (0.82+0.06*rng.float())*S, (0.12+0.06*rng.float())*S, S*0.045, 4, 0, pop)
	star(dst, (0.1+0.05*rng.float())*S, (0.5+0.06*rng.float())*S, S*0.028, 4, 0, pop)

	m := S * 0.06
	centredBlock(dst, s.Title, S-m*1.3, S-2*m, 2, pop, 0.75, retro, 0.17, 0.15, 0.135, 0.12, 0.105, 0.09)
	brandDot(dst)
}

func renderHorizon(dst *image.RGBA, s Spec, accent color.RGBA) {
	S := float64(s.Size)
	top, bottom := s.Sky[0], s.Sky[1]
	if top == (color.RGBA{}) || bottom == (color.RGBA{}) {
		top, bottom = tone(shiftHue(accent, -14), 0.66, 0.6), tone(shiftHue(accent, 16), 0.38, 0.6)
	}
	sun := s.Sun
	if sun == ([3]float64{}) {
		sun = [3]float64{0.5, 0.5, 0.62}
	}
	gradient(dst, dst.Bounds(), top, bottom, 90)

	// The sun is the mix: its albums packed edge to edge inside one disc,
	// graded towards the sky so they read as light and not as four sleeves.
	d := int(sun[2] * S)
	cx, cy := sun[0]*S, sun[1]*S
	r := image.Rect(int(cx)-d/2, int(cy)-d/2, int(cx)-d/2+d, int(cy)-d/2+d)
	disc := circleMask(d)
	glowAround(dst, r, disc, mix(white, top, 0.3), int(S*0.06), 0.55)
	mosaic := image.NewRGBA(image.Rect(0, 0, d, d))
	h := d / 2
	switch n := len(s.Albums); {
	case n >= 4:
		place(mosaic, image.Rect(0, 0, h, h), s.Albums[0])
		place(mosaic, image.Rect(h, 0, d, h), s.Albums[1])
		place(mosaic, image.Rect(0, h, h, d), s.Albums[2])
		place(mosaic, image.Rect(h, h, d, d), s.Albums[3])
	case n == 3:
		place(mosaic, image.Rect(0, 0, h, d), s.Albums[0])
		place(mosaic, image.Rect(h, 0, d, h), s.Albums[1])
		place(mosaic, image.Rect(h, h, d, d), s.Albums[2])
	case n == 2:
		place(mosaic, image.Rect(0, 0, h, d), s.Albums[0])
		place(mosaic, image.Rect(h, 0, d, d), s.Albums[1])
	case n == 1:
		place(mosaic, mosaic.Bounds(), s.Albums[0])
	default:
		gradient(mosaic, mosaic.Bounds(), mix(white, top, 0.4), mix(white, bottom, 0.5), 90)
	}
	if len(s.Albums) > 0 {
		duotone(mosaic, mosaic.Bounds(), tone(bottom, 0.18, 0.5), mix(white, top, 0.15), 0.4)
	}
	xdraw.DrawMask(dst, r, mosaic, image.Point{}, disc, image.Point{}, xdraw.Over)

	// The ground: a wide shallow arc, so the horizon curves like a hill and
	// the sun sinks behind it rather than behind a ruled line.
	hill(dst, S*0.76, S*1.6, tone(bottom, 0.13, 0.45))

	m := S * 0.06
	f, w := shrinkToFit(serif, s.Title, S-2*m, int(S*0.15), int(S*0.08))
	text(dst, f, ellipsize(f, s.Title, w, 0), m, S-m*1.15, 0, mix(white, top, 0.35), 0)
	brandDot(dst)
}

// centredBlock sets s centred on the canvas, wrapped into maxLines at the
// largest of sizes (fractions of the side) that fits, the last baseline at
// `bottom`.
func centredBlock(dst *image.RGBA, s string, bottom, maxW float64, maxLines int, c color.RGBA, shadowStrength float64, w weight, sizes ...float64) {
	S := float64(dst.Bounds().Dx())
	px := make([]int, len(sizes))
	for i, f := range sizes {
		px[i] = int(math.Round(f * S))
	}
	f, lines := fit(w, s, maxW, maxLines, px...)
	lh := lineHeight(f)
	if w == retro {
		// Shrikhand's line box is built for its tall swashes; stacked display
		// lines look loose at it.
		lh *= 0.84
	}
	for i, line := range lines {
		y := bottom - float64(len(lines)-1-i)*lh
		text(dst, f, line, (S-measure(f, line, 0))/2, y, 0, c, shadowStrength)
	}
}

// titleBlock sets s left aligned with the last baseline at `bottom`, at the
// largest of sizes (fractions of the side) that fits in maxLines.
func titleBlock(dst *image.RGBA, s string, x, bottom, maxW float64, maxLines int, c color.RGBA, shadowStrength float64, w weight, sizes ...float64) {
	S := float64(dst.Bounds().Dx())
	px := make([]int, len(sizes))
	for i, f := range sizes {
		px[i] = int(math.Round(f * S))
	}
	f, lines := fit(w, s, maxW, maxLines, px...)
	for i, line := range lines {
		y := bottom - float64(len(lines)-1-i)*lineHeight(f)
		text(dst, f, line, x, y, 0, c, shadowStrength)
	}
}

// displayTitle sets s in the condensed display face, every line scaled to
// fill maxW on its own, the way a poster is set: "DISCOVER" and "WEEKLY"
// are the same width and different sizes. The block is scaled down as a
// whole when it would be taller than maxH. With middle true, (cx, y) is the
// centre of the block; otherwise y is the last baseline.
func displayTitle(dst *image.RGBA, s string, cx, y, maxW, maxH float64, c color.RGBA, shadowStrength float64, middle bool) {
	S := float64(dst.Bounds().Dx())
	lines := splitLines(s)
	if len(lines) == 0 {
		return
	}
	const probe = 100
	pf := face(display, probe)
	sizes := make([]float64, len(lines))
	for i, l := range lines {
		w := measure(pf, l, 0)
		if w <= 0 {
			sizes[i] = probe
			continue
		}
		sizes[i] = math.Min(probe*maxW/w, S*0.42)
	}
	capRatio := capHeight(pf) / probe
	gap := S * 0.022
	height := func() float64 {
		h := gap * float64(len(lines)-1)
		for _, sz := range sizes {
			h += sz * capRatio
		}
		return h
	}
	if h := height(); h > maxH {
		k := (maxH - gap*float64(len(lines)-1)) / (h - gap*float64(len(lines)-1))
		for i := range sizes {
			sizes[i] *= k
		}
	}
	top := y - height()
	if middle {
		top = y - height()/2
	}
	for i, l := range lines {
		f := face(display, int(sizes[i]))
		top += capHeight(f)
		text(dst, f, l, cx-measure(f, l, 0)/2, top, 0, c, shadowStrength)
		top += gap
	}
}

// splitLines breaks a title into the lines a poster sets it on: one word a
// line for two or three words, a balanced pair of lines beyond that.
func splitLines(s string) []string {
	words := strings.Fields(s)
	switch {
	case len(words) == 0:
		return nil
	case len(words) <= 3:
		return words
	}
	best, bestDiff := 1, math.MaxInt
	for i := 1; i < len(words); i++ {
		a, b := len(strings.Join(words[:i], " ")), len(strings.Join(words[i:], " "))
		if d := abs(a - b); d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return []string{strings.Join(words[:best], " "), strings.Join(words[best:], " ")}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pmod(v, n int) int {
	return ((v % n) + n) % n
}

// EncodeJPEG is the upload format: a fraction of a PNG's size for a
// photographic image, and every client can show it.
func EncodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode reads whatever getCoverArt returned: JPEG, PNG, GIF or WebP.
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// placeholderHashes are the SHA-256 of Navidrome's own placeholder images
// (resources/artist-placeholder.webp and album-placeholder.webp), identical
// from 0.63.1 through 0.64.2 and on master as of 2026-10-13. Navidrome serves
// them byte for byte, never resized, when an entity has no artwork YET, which
// for an artist is common: 0.64 resolves artist photos in the background, so
// the first getCoverArt after a scan often answers with the grey silhouette.
// A cover built on that silhouette would look broken, so it is recognised
// and the album art is used instead until the photo arrives.
var placeholderHashes = map[string]bool{
	"521259c652af6d43cb124876106258d93b6b6f7fbe7ad2c73e8345875afe4dcf": true,
	"273a4dbd61dfbb0a12d5d8ffe780eb7a3d4d000bc9771c5411cc70ae4dfa8a1f": true,
}

// IsPlaceholder reports whether data is one of Navidrome's placeholders.
func IsPlaceholder(data []byte) bool {
	sum := sha256.Sum256(data)
	return placeholderHashes[hex.EncodeToString(sum[:])]
}
