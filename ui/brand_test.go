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
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// inksOf lists the distinct colours a drawing uses.
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

// outline is the coordinates of one path, control points included: every
// command in these drawings is absolute, so the numbers alternate x and y.
type outline struct{ xs, ys []float64 }

func (o outline) box() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for i := range o.xs {
		minX, maxX = math.Min(minX, o.xs[i]), math.Max(maxX, o.xs[i])
		minY, maxY = math.Min(minY, o.ys[i]), math.Max(maxY, o.ys[i])
	}
	return minX, minY, maxX, maxY
}

func (o outline) center() (x, y float64) {
	minX, minY, maxX, maxY := o.box()
	return (minX + maxX) / 2, (minY + maxY) / 2
}

// outlines returns the paths of a drawing in the order they are painted.
func outlines(t *testing.T, svg string) []outline {
	t.Helper()
	var all []outline
	for _, tag := range pathTagRE.FindAllString(svg, -1) {
		m := pathDataRE.FindStringSubmatch(tag)
		if m == nil {
			t.Fatalf("path without data: %s", tag)
		}
		nums := numberRE.FindAllString(m[1], -1)
		if len(nums) == 0 || len(nums)%2 != 0 {
			t.Fatalf("path data is not a list of x y pairs: %d numbers", len(nums))
		}
		var o outline
		for i := 0; i < len(nums); i += 2 {
			x, _ := strconv.ParseFloat(nums[i], 64)
			y, _ := strconv.ParseFloat(nums[i+1], 64)
			o.xs, o.ys = append(o.xs, x), append(o.ys, y)
		}
		all = append(all, o)
	}
	return all
}

func TestBrandMarkIsPathOnlyInOneInk(t *testing.T) {
	ink := map[string]string{}
	for _, name := range []string{"cerne-mark-light.svg", "cerne-mark-dark.svg"} {
		svg := readRepoFile(t, "docs/assets/logo/"+name)
		for _, banned := range []string{"<text", "<image", "<circle", "<ellipse", "<use", "<style", "href", "url("} {
			if strings.Contains(svg, banned) {
				t.Errorf("%s contains %q", name, banned)
			}
		}
		if !strings.Contains(svg, `viewBox="0 0 100 100"`) {
			t.Errorf("%s: the viewBox is not the 100 by 100 square", name)
		}
		if n := len(pathTagRE.FindAllString(svg, -1)); n != 4 {
			t.Errorf("%s: %d paths, want three rings and the heartwood", name, n)
		}
		inks := inksOf(svg)
		if len(inks) != 1 {
			t.Fatalf("%s: inks %v, want exactly one", name, inks)
		}
		ink[name] = inks[0]
	}
	if ink["cerne-mark-light.svg"] == ink["cerne-mark-dark.svg"] {
		t.Errorf("both files use the ink %s: one of them disappears on its page", ink["cerne-mark-light.svg"])
	}
}

func TestBrandMarkHeartwoodIsOffCenter(t *testing.T) {
	rings := outlines(t, readRepoFile(t, "docs/assets/logo/cerne-mark-light.svg"))
	trunk, heartwood := rings[0], rings[len(rings)-1]

	tx, ty := trunk.center()
	hx, hy := heartwood.center()
	if d := math.Hypot(hx-tx, hy-ty); d < 3 {
		t.Errorf("the heartwood sits %.1f from the centre of the trunk, want at least 3 of 100", d)
	}

	// A trunk is not a circle: its radius changes around the contour.
	near, far := math.Inf(1), math.Inf(-1)
	for i := range trunk.xs {
		r := math.Hypot(trunk.xs[i]-tx, trunk.ys[i]-ty)
		near, far = math.Min(near, r), math.Max(far, r)
	}
	if far-near < 2 {
		t.Errorf("the trunk's radius only varies by %.1f, want at least 2 of 100", far-near)
	}
}
