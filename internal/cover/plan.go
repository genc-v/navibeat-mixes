package cover

import (
	"hash/fnv"
	"image"
	"image/color"
	"strconv"
	"strings"
)

// RenderVersion goes into every signature. Bump it whenever a layout changes
// how it looks, and every cover is redrawn on the next run; leave it alone
// and an unchanged mix is never re-uploaded.
const RenderVersion = "1"

// Mix is what the generator knows about one playlist when it asks for a
// cover, in the words its plan already uses.
type Mix struct {
	// Kind is the NaviBeat mix kind: "dailymix", "artistradio", "timeofday"...
	Kind string
	// Slot is the machine slot: "dailymix-2", "morning", "decade-1990".
	Slot string
	// Name is the playlist's name without the prefix.
	Name string
	// Subject is the artist, genre or decade the mix is about, "" when none.
	Subject string
	// Accent is a colour the owner set for this family, "" for none.
	Accent string
	// Base is the family's built-in colour, the fallback when the artwork
	// has no colour of its own.
	Base string
}

// Track is one entry of the written playlist: the part a cover is chosen
// from.
type Track struct {
	Artist   string
	ArtistID string
	AlbumID  string
}

// Plan is a cover decided but not yet drawn: the words, the colour and which
// artwork to fetch. Deciding first is what lets an unchanged cover be skipped
// without fetching a single image.
type Plan struct {
	Layout  Layout
	Title   string
	Tag     string
	Accent  color.RGBA
	Fixed   bool
	Sky     [2]color.RGBA
	Sun     [3]float64
	Variant int
	// Artist is the artwork id of the photo, "" for none.
	Artist string
	// Albums are artwork ids, most important first.
	Albums []string
}

// skies are the time of day gradients, top then bottom.
var skies = map[string][2]string{
	"morning":   {"FFD08A", "E8606E"},
	"afternoon": {"9AD8FF", "2C74C9"},
	"evening":   {"FF9C66", "4E2483"},
	"night":     {"3C4C93", "0A0E29"},
}

// suns are where each time of day puts its sun (x, y, diameter, as
// fractions of the side): rising half hidden in the morning, high and whole
// in the afternoon, big and sinking in the evening, a small moon at night.
var suns = map[string][3]float64{
	"morning":   {0.5, 0.7, 0.62},
	"afternoon": {0.55, 0.4, 0.56},
	"evening":   {0.45, 0.76, 0.72},
	"night":     {0.62, 0.38, 0.46},
}

// PlanFor decides the cover of one mix from the tracks it was written with.
func PlanFor(m Mix, tracks []Track) Plan {
	p := Plan{Variant: variantOf(m.Slot)}
	if c, ok := Hex(m.Accent); ok {
		p.Accent, p.Fixed = c, true
	} else if c, ok := Hex(m.Base); ok {
		p.Accent = c
	} else {
		p.Accent = Brand
	}
	n := number(m.Slot)
	name := strings.TrimSpace(m.Name)
	subject := strings.TrimSpace(m.Subject)
	top := topArtist(tracks, "")

	switch m.Kind {
	case "dailymix":
		p.Layout, p.Title = LayoutDaily, "Daily Mix"
		if n > 0 {
			p.Tag = twoDigits(n)
			p.Variant = n - 1
		}
		p.Artist = artistArt(topArtist(tracks, subject))
		p.Albums = albumArts(tracks, subject, 1)
	case "artistradio":
		p.Layout, p.Title, p.Tag = LayoutRadio, or(subject, name), "RADIO"
		p.Artist = artistArt(topArtist(tracks, subject))
		p.Albums = albumArts(tracks, subject, 3)
	case "genreradio":
		p.Layout, p.Title = LayoutBand, or(subject, name)+" Mix"
		if subject == "" {
			p.Title = name
		}
		p.Artist = artistArt(top)
		p.Albums = albumArts(tracks, "", 1)
	case "decade":
		p.Layout, p.Title = LayoutBand, name+" Mix"
		p.Artist = artistArt(top)
		p.Albums = albumArts(tracks, "", 1)
	case "onrepeat", "essentials", "loved", "wrapped":
		p.Layout, p.Title = LayoutEditorial, name
		p.Artist = artistArt(top)
		p.Albums = albumArts(tracks, "", 1)
	case "timeofday":
		p.Layout, p.Title = LayoutHorizon, name
		p.Albums = albumArts(tracks, "", 4)
		p.Sun = suns[m.Slot]
		if sky, ok := skies[m.Slot]; ok && !p.Fixed {
			a, _ := Hex(sky[0])
			b, _ := Hex(sky[1])
			p.Sky = [2]color.RGBA{a, b}
		}
	default:
		// newmusic, discovery, rediscover, and anything added later.
		p.Layout, p.Title = LayoutPoster, name
		p.Albums = albumArts(tracks, "", 1)
	}
	if p.Title == "" {
		p.Title = name
	}
	return p
}

// Signature identifies what the cover would look like, so an unchanged one
// is never drawn or uploaded twice.
func (p Plan) Signature() string {
	accent := "auto:" + hexOf(p.Accent)
	if p.Fixed {
		accent = "fixed:" + hexOf(p.Accent)
	}
	parts := []string{
		"v" + RenderVersion, string(p.Layout), p.Title, p.Tag, accent,
		hexOf(p.Sky[0]) + hexOf(p.Sky[1]), sunOf(p.Sun), strconv.Itoa(p.Variant), p.Artist,
	}
	parts = append(parts, p.Albums...)
	return strings.Join(parts, "|")
}

// Spec turns the plan into something to draw, once the artwork is fetched.
// artist may be nil, and albums holds only the covers that could be read.
func (p Plan) Spec(artist image.Image, albums []image.Image) Spec {
	return Spec{
		Layout: p.Layout, Size: DefaultSize, Title: p.Title, Tag: p.Tag,
		Accent: p.Accent, Fixed: p.Fixed, Sky: p.Sky, Sun: p.Sun, Variant: p.Variant,
		Artist: artist, Albums: albums,
	}
}

// topArtist is the id of the artist the mix is about: the subject when it
// is set and present, else whoever has the most tracks in it. Ties go to the
// lowest id, so a reshuffle of the same tracks gives the same answer.
func topArtist(tracks []Track, subject string) string {
	counts := map[string]int{}
	order := []string{}
	for _, t := range tracks {
		if t.ArtistID == "" {
			continue
		}
		if subject != "" && !strings.EqualFold(strings.TrimSpace(t.Artist), subject) {
			continue
		}
		if counts[t.ArtistID] == 0 {
			order = append(order, t.ArtistID)
		}
		counts[t.ArtistID]++
	}
	if len(order) == 0 && subject != "" {
		return topArtist(tracks, "")
	}
	best := ""
	for _, id := range order {
		if best == "" || counts[id] > counts[best] || (counts[id] == counts[best] && id < best) {
			best = id
		}
	}
	return best
}

// albumArts is up to n distinct albums by how many tracks each has in the
// mix, those of `artist` (a name) first when it is set.
func albumArts(tracks []Track, artist string, n int) []string {
	counts := map[string]int{}
	mine := map[string]bool{}
	order := []string{}
	for _, t := range tracks {
		if t.AlbumID == "" {
			continue
		}
		if counts[t.AlbumID] == 0 {
			order = append(order, t.AlbumID)
		}
		counts[t.AlbumID]++
		if artist != "" && strings.EqualFold(strings.TrimSpace(t.Artist), artist) {
			mine[t.AlbumID] = true
		}
	}
	// A stable insertion sort: the lists are a few dozen long at most.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && better(order[j], order[j-1], counts, mine); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	if len(order) > n {
		order = order[:n]
	}
	out := make([]string, len(order))
	for i, id := range order {
		out[i] = "al-" + id
	}
	return out
}

// better orders albums: the subject's first, then by track count, then by
// id. Never by position in the playlist, because the mixes reshuffle daily
// and a cover must not change just because the same albums moved around.
func better(a, b string, counts map[string]int, mine map[string]bool) bool {
	if mine[a] != mine[b] {
		return mine[a]
	}
	if counts[a] != counts[b] {
		return counts[a] > counts[b]
	}
	return a < b
}

func artistArt(id string) string {
	if id == "" {
		return ""
	}
	return "ar-" + id
}

// number is the 1 based position of a numbered mix, read off its slot
// ("dailymix-2" is 2), or 0 for a slot that is not numbered.
func number(slot string) int {
	i := strings.LastIndexByte(slot, '-')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(slot[i+1:])
	if err != nil || n < 1 || n > 99 {
		return 0
	}
	return n
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// variantOf is a small number that is the same for a slot every night and
// different between slots.
func variantOf(slot string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(slot))
	return int(h.Sum32() % 997)
}

func sunOf(v [3]float64) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(f, 'f', 2, 64)
	}
	return strings.Join(parts, ",")
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
