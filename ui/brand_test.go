package ui

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// F-0461: cerne's mark is the cross-section of a tree trunk, drawn in one ink.

var (
	pathTagRE  = regexp.MustCompile(`<path\b[^>]*>`)
	pathDataRE = regexp.MustCompile(`\bd="([^"]+)"`)
	numberRE   = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	inkRE      = regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
	strokeRE   = regexp.MustCompile(`stroke-width="([0-9.]+)"`)
	// The favicon's stylesheet: the rules for a light tab, then the ones a
	// dark colour scheme swaps in.
	// Every .nav-mark rule of the dashboard's stylesheet, body captured.
	navMarkRE = regexp.MustCompile(`\.nav-mark\s*\{([^}]*)\}`)
	schemeRE  = regexp.MustCompile(`<style>(.*)@media\s*\(prefers-color-scheme:\s*dark\)\s*\{(.*)\}</style>`)
)

const (
	markForLightPage = "docs/assets/logo/cerne-mark-light.svg"
	markForDarkPage  = "docs/assets/logo/cerne-mark-dark.svg"
	favicon          = "ui/public/favicon.svg"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// inksOf lists the distinct colours a piece of a drawing uses.
func inksOf(svg string) []string {
	seen := map[string]bool{}
	for _, ink := range inkRE.FindAllString(svg, -1) {
		seen[strings.ToLower(ink)] = true
	}
	inks := make([]string, 0, len(seen))
	for ink := range seen {
		inks = append(inks, ink)
	}
	sort.Strings(inks)
	return inks
}

// lightness is 0 for black and 1 for white.
func lightness(t *testing.T, ink string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(ink, "#"), 16, 32)
	if err != nil {
		t.Fatalf("ink %q: %v", ink, err)
	}
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	return (0.2126*r + 0.7152*g + 0.0722*b) / 255
}

// wantInk fails unless the piece is drawn in exactly one ink of the wanted
// polarity: dark for a light page, light for a dark page.
func wantInk(t *testing.T, where, piece string, dark bool) {
	t.Helper()
	inks := inksOf(piece)
	if len(inks) != 1 {
		t.Errorf("%s: inks %v, want exactly one", where, inks)
		return
	}
	l := lightness(t, inks[0])
	if dark && l > 0.3 {
		t.Errorf("%s: ink %s is too light to read on a light page", where, inks[0])
	}
	if !dark && l < 0.7 {
		t.Errorf("%s: ink %s is too dark to read on a dark page", where, inks[0])
	}
}

type point struct{ x, y float64 }

// shape is one path of a drawing, as points sampled along its curve.
type shape []point

// shapes returns the paths of a drawing in the order they are painted. Every
// path here is an absolute M followed by cubic C segments, so the numbers are
// a start point and then two control points and an end point per segment.
func shapes(t *testing.T, svg string) []shape {
	t.Helper()
	var all []shape
	for _, tag := range pathTagRE.FindAllString(svg, -1) {
		m := pathDataRE.FindStringSubmatch(tag)
		if m == nil {
			t.Fatalf("path without data: %s", tag)
		}
		nums := numberRE.FindAllString(m[1], -1)
		if len(nums) < 8 || (len(nums)-2)%6 != 0 {
			t.Fatalf("path data is not M x y followed by cubic segments: %d numbers", len(nums))
		}
		at := func(i int) point {
			x, _ := strconv.ParseFloat(nums[i], 64)
			y, _ := strconv.ParseFloat(nums[i+1], 64)
			return point{x, y}
		}
		p0 := at(0)
		s := shape{p0}
		for i := 2; i < len(nums); i += 6 {
			c1, c2, p3 := at(i), at(i+2), at(i+4)
			for step := 1; step <= 10; step++ {
				u := float64(step) / 10
				v := 1 - u
				s = append(s, point{
					v*v*v*p0.x + 3*v*v*u*c1.x + 3*v*u*u*c2.x + u*u*u*p3.x,
					v*v*v*p0.y + 3*v*v*u*c1.y + 3*v*u*u*c2.y + u*u*u*p3.y,
				})
			}
			p0 = p3
		}
		all = append(all, s)
	}
	return all
}

// center is the middle of the shape's extent.
func (s shape) center() point {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range s {
		minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
		minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	return point{(minX + maxX) / 2, (minY + maxY) / 2}
}

// radii returns the shortest, the longest and the mean distance from the
// shape's centre to its curve.
func (s shape) radii() (near, far, mean float64) {
	c := s.center()
	near, far = math.Inf(1), math.Inf(-1)
	for _, p := range s {
		r := math.Hypot(p.x-c.x, p.y-c.y)
		near, far, mean = math.Min(near, r), math.Max(far, r), mean+r
	}
	return near, far, mean / float64(len(s))
}

// gap is the shortest distance between the curves of two shapes.
func gap(a, b shape) float64 {
	d := math.Inf(1)
	for _, p := range a {
		for _, q := range b {
			d = math.Min(d, math.Hypot(p.x-q.x, p.y-q.y))
		}
	}
	return d
}

func TestBrandMarkIsPathOnlyInOneInk(t *testing.T) {
	for file, darkInk := range map[string]bool{markForLightPage: true, markForDarkPage: false} {
		svg := readRepoFile(t, file)
		for _, banned := range []string{"<text", "<image", "<circle", "<ellipse", "<use", "<style", "href", "url("} {
			if strings.Contains(svg, banned) {
				t.Errorf("%s contains %q", file, banned)
			}
		}
		if !strings.Contains(svg, `viewBox="0 0 100 100"`) {
			t.Errorf("%s: the viewBox is not the 100 by 100 square", file)
		}
		if n := len(pathTagRE.FindAllString(svg, -1)); n != 4 {
			t.Errorf("%s: %d paths, want three rings and the heartwood", file, n)
		}
		wantInk(t, file, svg, darkInk)
	}
}

// A trunk is not a set of circles around its middle: every ring is irregular
// and the heartwood sits off centre. It holds for the full mark and for the
// reduced one.
func TestBrandMarkRingsAreIrregularAroundAnOffCenterHeartwood(t *testing.T) {
	for _, file := range []string{markForLightPage, markForDarkPage, favicon} {
		all := shapes(t, readRepoFile(t, file))
		if len(all) < 2 {
			t.Fatalf("%s: %d paths, want at least a ring and the heartwood", file, len(all))
		}
		trunk, heartwood := all[0].center(), all[len(all)-1].center()
		if d := math.Hypot(heartwood.x-trunk.x, heartwood.y-trunk.y); d < 4 {
			t.Errorf("%s: the heartwood sits %.1f from the centre of the trunk, want at least 4 of 100", file, d)
		}
		for i, s := range all {
			near, far, mean := s.radii()
			if (far-near)/mean < 0.04 {
				t.Errorf("%s: path %d is too close to a circle: its radius varies %.1f%%, want at least 4%%", file, i+1, 100*(far-near)/mean)
			}
		}
	}
}

// The favicon is checked in the embedded front-end, because that is what ships.
func TestDistIndexLinksFavicon(t *testing.T) {
	index, err := Dist.ReadFile("dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `<link rel="icon" type="image/svg+xml" href="/favicon.svg"`) {
		t.Error("the embedded index.html does not link the favicon")
	}
	shipped, err := Dist.ReadFile("dist/favicon.svg")
	if err != nil {
		t.Fatalf("the embedded front-end has no favicon: %v", err)
	}
	if string(shipped) != readRepoFile(t, favicon) {
		t.Error("dist/favicon.svg differs from ui/public/favicon.svg: rebuild ui/dist")
	}
}

func TestFaviconSwitchesInkByColorScheme(t *testing.T) {
	svg := readRepoFile(t, favicon)
	for _, banned := range []string{"<text", "<image", "<circle", "<ellipse", "<use", "href", "url("} {
		if strings.Contains(svg, banned) {
			t.Errorf("the favicon contains %q", banned)
		}
	}
	if !strings.Contains(svg, `viewBox="0 0 100 100"`) {
		t.Error("the favicon's viewBox is not the 100 by 100 square")
	}
	// B-0033: two rings, so the reduced mark reads as tree rings and not as a
	// circle with a dot in it.
	tags := pathTagRE.FindAllString(svg, -1)
	if len(tags) != 3 || !strings.Contains(tags[0], `class="r"`) || !strings.Contains(tags[1], `class="r"`) || !strings.Contains(tags[2], `class="h"`) {
		t.Fatalf("want two rings with class r and the heartwood with class h, got %d paths", len(tags))
	}

	m := schemeRE.FindStringSubmatch(svg)
	if m == nil {
		t.Fatal("the favicon has no rules for a dark colour scheme")
	}
	for name, rules := range map[string]string{"light tab": m[1], "dark tab": m[2]} {
		for _, class := range []string{".r{", ".h{"} {
			if !strings.Contains(rules, class) {
				t.Errorf("%s: no rule for %s}", name, class)
			}
		}
	}
	wantInk(t, "the favicon on a light tab", m[1], true)
	wantInk(t, "the favicon on a dark tab", m[2], false)
}

func TestFaviconReadsAt16px(t *testing.T) {
	svg := readRepoFile(t, favicon)
	const px = 16.0 / 100 // one unit of the viewBox on a 16 px tab icon

	all := shapes(t, svg)
	widths := strokeRE.FindAllStringSubmatch(svg, -1)
	if len(all) != 3 || len(widths) != 2 {
		t.Fatalf("%d paths and %d strokes, want two stroked rings and the heartwood", len(all), len(widths))
	}
	outer, _ := strconv.ParseFloat(widths[0][1], 64)
	inner, _ := strconv.ParseFloat(widths[1][1], 64)
	for name, w := range map[string]float64{"outer": outer, "inner": inner} {
		if w*px < 1 {
			t.Errorf("the %s ring is %.2f px wide at 16 px, want at least 1", name, w*px)
		}
	}

	// A ring is stroked on its centre line, so half of its stroke eats into
	// each gap beside it. A gap of just 1 px between two curved strokes comes
	// out grey, and the rings smear into one blot (B-0033): the floor leaves a
	// pixel that is mostly background.
	const minGap = 1.15
	if clear := gap(all[0], all[1]) - outer/2 - inner/2; clear*px < minGap {
		t.Errorf("the gap between the two rings is %.2f px at 16 px, want at least %.2f", clear*px, minGap)
	}
	if clear := gap(all[1], all[2]) - inner/2; clear*px < minGap {
		t.Errorf("the gap between the inner ring and the heartwood is %.2f px at 16 px, want at least %.2f", clear*px, minGap)
	}
}

func TestReadmeShowsMarkInBothThemes(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	picture, heading := strings.Index(readme, "<picture>"), strings.Index(readme, "<h1")
	if picture < 0 || heading < 0 || picture > heading {
		t.Fatalf("the README does not show the mark above the name (picture at %d, h1 at %d)", picture, heading)
	}
	top := readme[:heading]
	for theme, want := range map[string]string{
		"dark":  `<source media="(prefers-color-scheme: dark)" srcset="` + markForDarkPage + `">`,
		"light": `src="` + markForLightPage + `"`,
	} {
		if !strings.Contains(top, want) {
			t.Errorf("no mark for the %s theme above the name: missing %s", theme, want)
		}
	}
}

// F-0462: the top nav shows the mark as a CSS mask over the favicon, painted
// in the ink of the name. One drawing, and it follows the dashboard's theme
// instead of the system's.
func TestNavMarkIsAMaskOfTheFavicon(t *testing.T) {
	rules := navMarkRE.FindAllStringSubmatch(readRepoFile(t, "ui/src/app.css"), -1)
	if len(rules) == 0 {
		t.Fatal("app.css has no .nav-mark rule")
	}
	painted := false
	for _, r := range rules {
		rule := r[1]
		if inks := inksOf(rule); len(inks) != 0 {
			t.Errorf("a .nav-mark rule fixes the colour %v, want only currentColor", inks)
		}
		if strings.Contains(rule, "background: currentColor") &&
			strings.Contains(rule, "-webkit-mask: url(/favicon.svg)") &&
			strings.Contains(rule, " mask: url(/favicon.svg)") {
			painted = true
		}
	}
	if !painted {
		t.Error("no .nav-mark rule paints currentColor through a mask of /favicon.svg")
	}

	nav := readRepoFile(t, "ui/src/Nav.tsx")
	for _, banned := range []string{"<svg", "<path"} {
		if strings.Contains(nav, banned) {
			t.Errorf("Nav.tsx carries its own drawing (%q): the mark must come from the favicon", banned)
		}
	}
}
