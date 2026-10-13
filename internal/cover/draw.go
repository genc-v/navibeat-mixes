package cover

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// The drawing primitives a cover is built from. Everything works straight on
// the RGBA pixel slice where it can: this runs inside WASM, and image.At/Set
// per pixel on a 640 by 640 canvas costs a visible share of a host call.

type fontFace = font.Face

func newCanvas(size int) *image.RGBA {
	return image.NewRGBA(image.Rect(0, 0, size, size))
}

// fillRect paints r with one opaque colour.
func fillRect(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(dst.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst.Pix[dst.PixOffset(r.Min.X, y):dst.PixOffset(r.Max.X, y)]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, 255
		}
	}
}

// duotone maps r's brightness onto the ramp from dark to light, blended with
// the original by amount. The treatment that turns any press photo into
// something that looks designed for this cover.
func duotone(dst *image.RGBA, r image.Rectangle, dark, light color.RGBA, amount float64) {
	r = r.Intersect(dst.Bounds())
	var lut [256][3]uint8
	for i := range 256 {
		// A gentle S curve first, so the duotone has some punch.
		t := float64(i) / 255
		t = t * t * (3 - 2*t)
		c := mix(dark, light, t)
		lut[i] = [3]uint8{c.R, c.G, c.B}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		off := dst.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			p := dst.Pix[off : off+3 : off+3]
			l := (299*int(p[0]) + 587*int(p[1]) + 114*int(p[2])) / 1000
			d := lut[l]
			p[0] = blend(p[0], d[0], amount)
			p[1] = blend(p[1], d[1], amount)
			p[2] = blend(p[2], d[2], amount)
			off += 4
		}
	}
}

// polyMask rasterises a closed polygon (points relative to the w by h box).
func polyMask(w, h int, pts [][2]float64) *image.Alpha {
	z := vector.NewRasterizer(w, h)
	z.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
	for _, p := range pts[1:] {
		z.LineTo(float32(p[0]), float32(p[1]))
	}
	z.ClosePath()
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(m, m.Bounds(), image.Opaque, image.Point{})
	return m
}

// slab is a parallelogram leaning right by skew, filled with a vertical
// gradient: the blocks of the poster layout.
func slab(dst *image.RGBA, x, y, w, h, skew float64, from, to color.RGBA) {
	bw, bh := int(math.Ceil(w+skew)), int(math.Ceil(h))
	if bw <= 0 || bh <= 0 {
		return
	}
	m := polyMask(bw, bh, [][2]float64{{skew, 0}, {w + skew, 0}, {w, h}, {0, h}})
	g := image.NewRGBA(image.Rect(0, 0, bw, bh))
	gradient(g, g.Bounds(), from, to, 90)
	at := image.Rect(int(x), int(y), int(x)+bw, int(y)+bh)
	xdraw.DrawMask(dst, at, g, image.Point{}, m, image.Point{}, xdraw.Over)
}

// star draws a filled star of `points` points and outer radius r centred on
// (cx, cy), turned by rot radians. Four points make a sparkle.
func star(dst *image.RGBA, cx, cy, r float64, points int, rot float64, c color.RGBA) {
	inner := 0.45
	if points == 4 {
		inner = 0.3
	}
	d := int(math.Ceil(2 * r))
	pts := make([][2]float64, 0, 2*points)
	for i := range 2 * points {
		rad := r
		if i%2 == 1 {
			rad = r * inner
		}
		a := rot + float64(i)*math.Pi/float64(points) - math.Pi/2
		pts = append(pts, [2]float64{r + rad*math.Cos(a), r + rad*math.Sin(a)})
	}
	m := polyMask(d, d, pts)
	fillMasked(dst, image.Rect(int(cx-r), int(cy-r), int(cx-r)+d, int(cy-r)+d), c, m)
}

// glowAround is shadow in a colour: a blurred copy of mask drawn under r.
func glowAround(dst *image.RGBA, r image.Rectangle, mask *image.Alpha, c color.RGBA, blur int, strength float64) {
	pad := blur * 3
	soft := image.NewAlpha(image.Rect(0, 0, r.Dx()+2*pad, r.Dy()+2*pad))
	for y := 0; y < r.Dy(); y++ {
		copy(soft.Pix[(y+pad)*soft.Stride+pad:], mask.Pix[y*mask.Stride:y*mask.Stride+r.Dx()])
	}
	for range 3 {
		boxBlur(soft, blur)
	}
	fillMasked(dst, r.Inset(-pad), c, scaleAlpha(soft, strength))
}

// scaleAlpha returns a copy of m with every value multiplied by f.
func scaleAlpha(m *image.Alpha, f float64) *image.Alpha {
	out := image.NewAlpha(m.Rect)
	for i, v := range m.Pix {
		out.Pix[i] = uint8(float64(v) * f)
	}
	return out
}

// rnd is a tiny deterministic generator (xorshift32). Layout details are
// "random" only in the sense of looking unplanned: the same playlist must
// get the same cover every night, or every run would upload a new image.
type rnd struct{ s uint32 }

func newRand(seed uint32) *rnd {
	if seed == 0 {
		seed = 0x9E3779B9
	}
	return &rnd{seed}
}

// float returns a number in [0, 1).
func (r *rnd) float() float64 {
	r.s ^= r.s << 13
	r.s ^= r.s >> 17
	r.s ^= r.s << 5
	return float64(r.s) / (1 << 32)
}

func fill(dst *image.RGBA, c color.RGBA) {
	r := dst.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst.Pix[dst.PixOffset(r.Min.X, y):dst.PixOffset(r.Max.X, y)]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2], row[i+3] = c.R, c.G, c.B, 255
		}
	}
}

// gradient paints r from `from` to `to` along angle degrees (0 is left to
// right, 90 is top to bottom).
func gradient(dst *image.RGBA, r image.Rectangle, from, to color.RGBA, angle float64) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	rad := angle * math.Pi / 180
	dx, dy := math.Cos(rad), math.Sin(rad)
	// Project the four corners to find the span the gradient has to cover.
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range [][2]float64{
		{float64(r.Min.X), float64(r.Min.Y)}, {float64(r.Max.X), float64(r.Min.Y)},
		{float64(r.Min.X), float64(r.Max.Y)}, {float64(r.Max.X), float64(r.Max.Y)},
	} {
		v := p[0]*dx + p[1]*dy
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	span := hi - lo
	for y := r.Min.Y; y < r.Max.Y; y++ {
		off := dst.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			t := (float64(x)*dx + float64(y)*dy - lo) / span
			c := mix(from, to, t)
			dst.Pix[off], dst.Pix[off+1], dst.Pix[off+2], dst.Pix[off+3] = c.R, c.G, c.B, 255
			off += 4
		}
	}
}

// veil lays c over r with an alpha that runs from a0 at the top of r to a1
// at the bottom. A veil is how text stays readable over a photograph.
func veil(dst *image.RGBA, r image.Rectangle, c color.RGBA, a0, a1 float64) {
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	h := float64(r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		t := (float64(y-r.Min.Y) + 0.5) / h
		// Eased, so the veil has no visible edge where it starts.
		t = t * t * (3 - 2*t)
		a := clamp01(a0 + (a1-a0)*t)
		off := dst.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.Pix[off] = blend(dst.Pix[off], c.R, a)
			dst.Pix[off+1] = blend(dst.Pix[off+1], c.G, a)
			dst.Pix[off+2] = blend(dst.Pix[off+2], c.B, a)
			off += 4
		}
	}
}

func blend(under, over uint8, a float64) uint8 {
	return uint8(math.Round(float64(under)*(1-a) + float64(over)*a))
}

// tint lays one flat colour over r at alpha a.
func tint(dst *image.RGBA, r image.Rectangle, c color.RGBA, a float64) {
	veil(dst, r, c, a, a)
}

// cropFill scales src to cover a w by h box completely, cropping the overflow
// from the centre, the way every album grid in every client does it.
func cropFill(src image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := trimFrame(src)
	if sb.Empty() || w <= 0 || h <= 0 {
		return out
	}
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	scale := math.Max(float64(w)/sw, float64(h)/sh)
	cw, ch := float64(w)/scale, float64(h)/scale
	x0 := float64(sb.Min.X) + (sw-cw)/2
	// Faces sit in the upper half of nearly every press photo, so a crop of a
	// portrait image keeps the top rather than the middle.
	y0 := float64(sb.Min.Y) + (sh-ch)*0.3
	crop := image.Rect(int(x0), int(y0), int(math.Round(x0+cw)), int(math.Round(y0+ch))).Intersect(sb)
	xdraw.BiLinear.Scale(out, out.Bounds(), src, crop, xdraw.Src, nil)
	return out
}

// trimFrame returns src's bounds without a light frame round the picture,
// which some artist photos carry (a press shot on white card): drawn full
// bleed, the frame shows up as a thin white box inside the cover. A side
// only counts when its rows are uniformly light, and only a frame on at
// least three sides is trimmed, so a photo with a pale sky is left alone.
func trimFrame(src image.Image) image.Rectangle {
	b := src.Bounds()
	limitX, limitY := b.Dx()*8/100, b.Dy()*8/100
	light := func(x, y int) bool {
		c := color.RGBAModel.Convert(src.At(x, y)).(color.RGBA)
		return c.R > 215 && c.G > 215 && c.B > 215 &&
			max(c.R, c.G, c.B)-min(c.R, c.G, c.B) < 18
	}
	row := func(y int) bool {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			if !light(x, y) {
				return false
			}
		}
		return true
	}
	col := func(x int) bool {
		for y := b.Min.Y; y < b.Max.Y; y += 3 {
			if !light(x, y) {
				return false
			}
		}
		return true
	}
	top, bottom, left, right := 0, 0, 0, 0
	for top < limitY && row(b.Min.Y+top) {
		top++
	}
	for bottom < limitY && row(b.Max.Y-1-bottom) {
		bottom++
	}
	for left < limitX && col(b.Min.X+left) {
		left++
	}
	for right < limitX && col(b.Max.X-1-right) {
		right++
	}
	sides := 0
	for _, n := range []int{top, bottom, left, right} {
		if n > 0 {
			sides++
		}
	}
	if sides < 3 {
		return b
	}
	// A pixel more than the frame itself, for the anti-aliased edge between
	// frame and picture.
	return image.Rect(b.Min.X+left+1, b.Min.Y+top+1, b.Max.X-right-1, b.Max.Y-bottom-1)
}

// place draws src cropped to fill r.
func place(dst *image.RGBA, r image.Rectangle, src image.Image) {
	img := cropFill(src, r.Dx(), r.Dy())
	xdraw.Draw(dst, r, img, image.Point{}, xdraw.Src)
}

// placeMasked draws src cropped to fill r, through mask (whose bounds start
// at 0,0 and match r's size).
func placeMasked(dst *image.RGBA, r image.Rectangle, src image.Image, mask *image.Alpha) {
	img := cropFill(src, r.Dx(), r.Dy())
	xdraw.DrawMask(dst, r, img, image.Point{}, mask, image.Point{}, xdraw.Over)
}

// fillMasked paints a flat colour through mask at r.
func fillMasked(dst *image.RGBA, r image.Rectangle, c color.RGBA, mask *image.Alpha) {
	xdraw.DrawMask(dst, r, image.NewUniform(c), image.Point{}, mask, image.Point{}, xdraw.Over)
}

// circleMask is an anti-aliased disc filling a w by h box.
func circleMask(d int) *image.Alpha {
	return roundedMask(d, d, float64(d)/2)
}

// roundedMask is an anti-aliased rounded rectangle filling a w by h box.
func roundedMask(w, h int, radius float64) *image.Alpha {
	z := vector.NewRasterizer(w, h)
	fw, fh := float32(w), float32(h)
	r := float32(math.Min(radius, math.Min(float64(w), float64(h))/2))
	// 0.5523 places a cubic's control points so four of them trace a circle.
	k := r * (1 - 0.5523)
	z.MoveTo(r, 0)
	z.LineTo(fw-r, 0)
	z.CubeTo(fw-k, 0, fw, k, fw, r)
	z.LineTo(fw, fh-r)
	z.CubeTo(fw, fh-k, fw-k, fh, fw-r, fh)
	z.LineTo(r, fh)
	z.CubeTo(k, fh, 0, fh-k, 0, fh-r)
	z.LineTo(0, r)
	z.CubeTo(0, k, k, 0, r, 0)
	z.ClosePath()
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(m, m.Bounds(), image.Opaque, image.Point{})
	return m
}

// shadow drops a soft shadow of mask, placed at r and shifted by offset, onto
// dst. blur is the box radius; three box passes approximate a gaussian.
func shadow(dst *image.RGBA, r image.Rectangle, mask *image.Alpha, offset image.Point, blur int, strength float64) {
	pad := blur * 3
	w, h := r.Dx()+2*pad, r.Dy()+2*pad
	soft := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < r.Dy(); y++ {
		copy(soft.Pix[(y+pad)*soft.Stride+pad:], mask.Pix[y*mask.Stride:y*mask.Stride+r.Dx()])
	}
	for range 3 {
		boxBlur(soft, blur)
	}
	if strength < 1 {
		for i, v := range soft.Pix {
			soft.Pix[i] = uint8(float64(v) * strength)
		}
	}
	at := image.Rect(r.Min.X-pad, r.Min.Y-pad, r.Max.X+pad, r.Max.Y+pad).Add(offset)
	xdraw.DrawMask(dst, at, image.NewUniform(color.Black), image.Point{}, soft, image.Point{}, xdraw.Over)
}

// boxBlur blurs an alpha image in place, horizontally then vertically, with a
// running sum so the cost does not grow with the radius.
func boxBlur(m *image.Alpha, radius int) {
	if radius < 1 {
		return
	}
	w, h := m.Rect.Dx(), m.Rect.Dy()
	n := 2*radius + 1
	line := make([]int, max(w, h))
	pass := func(get func(i int) int, set func(i, v int), length int) {
		for i := range length {
			line[i] = get(i)
		}
		sum := 0
		for i := -radius; i <= radius; i++ {
			sum += line[clampIndex(i, length)]
		}
		for i := range length {
			set(i, sum/n)
			sum += line[clampIndex(i+radius+1, length)] - line[clampIndex(i-radius, length)]
		}
	}
	for y := range h {
		row := m.Pix[y*m.Stride:]
		pass(func(i int) int { return int(row[i]) }, func(i, v int) { row[i] = uint8(v) }, w)
	}
	for x := range w {
		pass(func(i int) int { return int(m.Pix[i*m.Stride+x]) },
			func(i, v int) { m.Pix[i*m.Stride+x] = uint8(v) }, h)
	}
}

func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// Text.

func px(v fixed.Int26_6) float64 { return float64(v) / 64 }

// measure is the drawn width of s, with `tracking` extra pixels between
// letters (the wide spacing of the small caps label).
func measure(f font.Face, s string, tracking float64) float64 {
	if tracking == 0 {
		return px(font.MeasureString(f, s))
	}
	w := 0.0
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			w += px(f.Kern(prev, r)) + tracking
		}
		adv, _ := f.GlyphAdvance(r)
		w += px(adv)
		prev = r
	}
	return w
}

// textMask renders s into an alpha mask the size of the canvas, baseline at
// (x, y). A mask rather than a direct draw, so the same pixels give both the
// soft shadow and the text itself.
func textMask(bounds image.Rectangle, f font.Face, s string, x, y, tracking float64) *image.Alpha {
	m := image.NewAlpha(bounds)
	d := font.Drawer{Dst: m, Src: image.Opaque, Face: f}
	if tracking == 0 {
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
		d.DrawString(s)
		return m
	}
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			x += px(f.Kern(prev, r)) + tracking
		}
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
		d.DrawString(string(r))
		adv, _ := f.GlyphAdvance(r)
		x += px(adv)
		prev = r
	}
	return m
}

// text draws s with its baseline at (x, y). shadowStrength 0 draws no shadow.
func text(dst *image.RGBA, f font.Face, s string, x, y, tracking float64, c color.RGBA, shadowStrength float64) {
	m := textMask(dst.Bounds(), f, s, x, y, tracking)
	if shadowStrength > 0 {
		lineH := px(f.Metrics().Height)
		top := int(y - lineH)
		box := image.Rect(int(x)-2, top, int(x+measure(f, s, tracking))+4, int(y+lineH*0.4)).Intersect(dst.Bounds())
		if !box.Empty() {
			crop := image.NewAlpha(image.Rect(0, 0, box.Dx(), box.Dy()))
			for row := 0; row < box.Dy(); row++ {
				copy(crop.Pix[row*crop.Stride:], m.Pix[m.PixOffset(box.Min.X, box.Min.Y+row):m.PixOffset(box.Max.X, box.Min.Y+row)])
			}
			blur := max(2, int(lineH/14))
			shadow(dst, box, crop, image.Pt(0, max(1, blur/2)), blur, shadowStrength)
		}
	}
	xdraw.DrawMask(dst, dst.Bounds(), image.NewUniform(c), image.Point{}, m, dst.Bounds().Min, xdraw.Over)
}

// wrap breaks s into at most maxLines lines no wider than maxW. ok is false
// when it cannot, in which case the caller tries a smaller size.
func wrap(f font.Face, s string, maxW float64, maxLines int) ([]string, bool) {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil, true
	}
	var lines []string
	cur := ""
	for _, w := range words {
		try := w
		if cur != "" {
			try = cur + " " + w
		}
		if measure(f, try, 0) <= maxW {
			cur = try
			continue
		}
		if cur == "" {
			// One word wider than the line on its own.
			return nil, false
		}
		lines = append(lines, cur)
		cur = w
		if measure(f, cur, 0) > maxW {
			return nil, false
		}
	}
	lines = append(lines, cur)
	return lines, len(lines) <= maxLines
}

// fit finds the largest size from `sizes` (largest first) at which s wraps
// into maxLines. When even the smallest does not fit, the last line is cut
// with an ellipsis, so a long genre name shrinks before it is ever truncated.
func fit(w weight, s string, maxW float64, maxLines int, sizes ...int) (font.Face, []string) {
	for _, size := range sizes {
		f := face(w, size)
		if lines, ok := wrap(f, s, maxW, maxLines); ok {
			return f, lines
		}
	}
	f := face(w, sizes[len(sizes)-1])
	lines, _ := wrap(f, s, maxW, 1000)
	if len(lines) == 0 {
		// A single word wider than the line: cut it.
		return f, []string{ellipsize(f, s, maxW, 0)}
	}
	if len(lines) > maxLines {
		rest := strings.Join(lines[maxLines-1:], " ")
		lines = append(lines[:maxLines-1], ellipsize(f, rest, maxW, 0))
	}
	for i, l := range lines {
		lines[i] = ellipsize(f, l, maxW, 0)
	}
	return f, lines
}

// ellipsize cuts s to fit maxW, ending in an ellipsis when anything was cut.
func ellipsize(f font.Face, s string, maxW, tracking float64) string {
	if measure(f, s, tracking) <= maxW {
		return s
	}
	const dots = "…"
	for len(s) > 0 {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
		trimmed := strings.TrimRight(s, " ,")
		if measure(f, trimmed+dots, tracking) <= maxW {
			return trimmed + dots
		}
	}
	return dots
}

// lineHeight is the distance between two baselines of a block of text set at
// face f, tighter than the font's own because display titles look loose at
// the default.
func lineHeight(f font.Face) float64 {
	return px(f.Metrics().Height) * 0.92
}

// capHeight approximates the height of a capital, for centring text
// vertically against something that is not text, like the brand dot.
func capHeight(f font.Face) float64 {
	return px(f.Metrics().CapHeight)
}

// hill fills everything below an arc whose top touches y = crest in the
// middle of the canvas and which curves down towards the sides: the top of
// a circle of the given radius. Filled column by column with an
// anti-aliased top pixel, which is far cheaper than a mask the size of the
// whole circle.
func hill(dst *image.RGBA, crest, radius float64, c color.RGBA) {
	b := dst.Bounds()
	cx := float64(b.Min.X+b.Max.X) / 2
	for x := b.Min.X; x < b.Max.X; x++ {
		dx := float64(x) + 0.5 - cx
		top := crest
		if dx < radius {
			top += radius - math.Sqrt(radius*radius-dx*dx)
		} else {
			top = float64(b.Max.Y)
		}
		y0 := int(math.Floor(top))
		for y := max(y0, b.Min.Y); y < b.Max.Y; y++ {
			a := 1.0
			if y == y0 {
				a = 1 - (top - float64(y0))
			}
			off := dst.PixOffset(x, y)
			dst.Pix[off] = blend(dst.Pix[off], c.R, a)
			dst.Pix[off+1] = blend(dst.Pix[off+1], c.G, a)
			dst.Pix[off+2] = blend(dst.Pix[off+2], c.B, a)
		}
	}
}
