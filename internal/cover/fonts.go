package cover

import (
	_ "embed"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// Four display faces, all under the SIL Open Font License 1.1 (the
// fonts/OFL-*.txt files). Embedded so a cover renders the same on every
// server, with no font installed anywhere and nothing fetched at run time.
// Static fonts only, because x/image's opentype reader draws static outlines.
//
// Each layout has its own voice rather than one sans everywhere:
//
//   - Archivo Black: the heavy grotesque of the tags and radio titles, the
//     nearest free cut to the black weight streaming covers are set in.
//   - Anton: tall and condensed, for the poster covers.
//   - Shrikhand: a fat retro italic, for the editorial "best of" covers.
//   - DM Serif Display Italic: for the times of day, softer than the rest.
var (
	//go:embed fonts/ArchivoBlack-Regular.ttf
	archivoBlack []byte
	//go:embed fonts/Anton-Regular.ttf
	antonRegular []byte
	//go:embed fonts/Shrikhand-Regular.ttf
	shrikhand []byte
	//go:embed fonts/DMSerifDisplay-Italic.ttf
	dmSerifItalic []byte
)

type weight int

const (
	heavy weight = iota
	display
	retro
	serif
)

var (
	parsed = map[weight]*opentype.Font{}
	faces  = map[faceKey]font.Face{}
)

type faceKey struct {
	w    weight
	size int
}

// face returns a cached face. Sizes are whole pixels: a cover is drawn at a
// handful of sizes, and caching per fractional size would cache nothing.
//
// The plugin is one WASM instance serving one call at a time, so the maps
// need no lock, the same reasoning main.go gives for runningScheduleID.
func face(w weight, size int) font.Face {
	if size < 6 {
		size = 6
	}
	k := faceKey{w, size}
	if f, ok := faces[k]; ok {
		return f
	}
	f, ok := parsed[w]
	if !ok {
		data := archivoBlack
		switch w {
		case display:
			data = antonRegular
		case retro:
			data = shrikhand
		case serif:
			data = dmSerifItalic
		}
		var err error
		if f, err = opentype.Parse(data); err != nil {
			// The bytes are embedded at build time, so this is a broken build,
			// not a runtime condition worth carrying an error for.
			panic("cover: embedded font does not parse: " + err.Error())
		}
		parsed[w] = f
	}
	ff, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic("cover: embedded font has no face: " + err.Error())
	}
	faces[k] = ff
	return ff
}
