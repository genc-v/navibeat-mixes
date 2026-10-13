package cover

import (
	"image"
	"image/color"
	"math"
	"strings"
)

// Hex parses six hex digits, with or without a leading '#'. ok is false for
// anything else, so a typo in a settings field falls back to a default rather
// than painting a cover an arbitrary shade.
func Hex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		hi, ok1 := hexDigit(s[2*i])
		lo, ok2 := hexDigit(s[2*i+1])
		if !ok1 || !ok2 {
			return color.RGBA{}, false
		}
		v[i] = hi<<4 | lo
	}
	return color.RGBA{v[0], v[1], v[2], 255}, true
}

func hexDigit(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// hexOf is the inverse of Hex, used only to put a colour into a signature.
func hexOf(c color.RGBA) string {
	const digits = "0123456789ABCDEF"
	b := []byte{0, 0, 0, 0, 0, 0}
	for i, v := range []uint8{c.R, c.G, c.B} {
		b[2*i] = digits[v>>4]
		b[2*i+1] = digits[v&0x0F]
	}
	return string(b)
}

// hsl is hue in degrees [0,360), saturation and lightness in [0,1].
type hsl struct{ h, s, l float64 }

func toHSL(c color.RGBA) hsl {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	l := (mx + mn) / 2
	if mx == mn {
		return hsl{0, 0, l}
	}
	d := mx - mn
	s := d / (1 - math.Abs(2*l-1))
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return hsl{h, s, l}
}

func fromHSL(v hsl) color.RGBA {
	h := math.Mod(v.h, 360)
	if h < 0 {
		h += 360
	}
	s, l := clamp01(v.s), clamp01(v.l)
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{to8(r + m), to8(g + m), to8(b + m), 255}
}

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }

func to8(f float64) uint8 { return uint8(math.Round(clamp01(f) * 255)) }

// tone keeps the hue of c and moves it to the given lightness, with at least
// the given saturation. Every background on a cover goes through here, so a
// muddy dominant colour still produces a background worth looking at.
func tone(c color.RGBA, lightness, minSat float64) color.RGBA {
	v := toHSL(c)
	v.l = lightness
	if v.s < minSat {
		v.s = minSat
	}
	return fromHSL(v)
}

// shiftHue turns c around the colour wheel, for the second stop of a gradient.
func shiftHue(c color.RGBA, degrees float64) color.RGBA {
	v := toHSL(c)
	v.h += degrees
	return fromHSL(v)
}

// mix blends a towards b by t.
func mix(a, b color.RGBA, t float64) color.RGBA {
	t = clamp01(t)
	f := func(x, y uint8) uint8 { return to8((float64(x)*(1-t) + float64(y)*t) / 255) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// luminance is WCAG relative luminance, the number that decides whether text
// on a colour is drawn white or dark.
func luminance(c color.RGBA) float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// textOn picks white or near black for text drawn over bg.
func textOn(bg color.RGBA) color.RGBA {
	if luminance(bg) > 0.42 {
		return color.RGBA{18, 18, 18, 255}
	}
	return color.RGBA{255, 255, 255, 255}
}

// Dominant finds the most prominent VIVID colour in img: the one a person
// would name if asked what colour the artwork is. Grey, near black and near
// white pixels are ignored, because a black and white press photo should not
// give a cover a grey background when the family colour would do better.
// ok is false when too little of the image has any colour at all.
func Dominant(img image.Image) (color.RGBA, bool) {
	if img == nil {
		return color.RGBA{}, false
	}
	b := img.Bounds()
	if b.Empty() {
		return color.RGBA{}, false
	}
	const grid = 48
	const buckets = 24
	var weight [buckets]float64
	var sumR, sumG, sumB [buckets]float64
	samples := 0
	for gy := 0; gy < grid; gy++ {
		y := b.Min.Y + (2*gy+1)*b.Dy()/(2*grid)
		for gx := 0; gx < grid; gx++ {
			x := b.Min.X + (2*gx+1)*b.Dx()/(2*grid)
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			samples++
			v := toHSL(c)
			if v.l < 0.12 || v.l > 0.92 || v.s < 0.18 {
				continue
			}
			w := v.s * (1 - math.Abs(v.l-0.5)*1.2)
			i := int(v.h/(360.0/buckets)) % buckets
			weight[i] += w
			sumR[i] += w * float64(c.R)
			sumG[i] += w * float64(c.G)
			sumB[i] += w * float64(c.B)
		}
	}
	best, bestScore, total := -1, 0.0, 0.0
	for i := 0; i < buckets; i++ {
		total += weight[i]
		// A hue straddling two buckets should not lose to a narrower one, so
		// each bucket is judged with half of its neighbours.
		score := weight[i] + 0.5*(weight[(i+buckets-1)%buckets]+weight[(i+1)%buckets])
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 || total < 0.04*float64(samples) || weight[best] == 0 {
		return color.RGBA{}, false
	}
	w := weight[best]
	return color.RGBA{
		uint8(math.Round(sumR[best] / w)),
		uint8(math.Round(sumG[best] / w)),
		uint8(math.Round(sumB[best] / w)),
		255,
	}, true
}
