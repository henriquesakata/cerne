package lint

import (
	"reflect"
	"testing"
)

func TestEntityOf(t *testing.T) {
	tests := []struct {
		name string
		f    Finding
		want string
	}{
		{"entity set wins", Finding{Entity: "F-0001", File: "content/features/F-0002/spec.md"}, "F-0001"},
		{"feature file", Finding{File: "content/features/F-0003/spec.md"}, "F-0003"},
		{"feature file nested", Finding{File: "content/features/F-0003/sub/x.md"}, "F-0003"},
		{"errata file", Finding{File: "content/errata/F-0004.md"}, "F-0004"},
		{"errata decision", Finding{File: "content/errata/D-0001.md"}, "D-0001"},
		{"wiki file", Finding{File: "content/wiki/page.md"}, ""},
		{"feature dir without file", Finding{File: "content/features/F-0003"}, ""},
		{"neither", Finding{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EntityOf(tc.f); got != tc.want {
				t.Fatalf("EntityOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScope(t *testing.T) {
	a := Finding{Rule: "r", Entity: "F-0001"}
	b := Finding{Rule: "r", File: "content/features/F-0002/spec.md"}
	c := Finding{Rule: "r", File: "content/errata/F-0001.md"}
	d := Finding{Rule: "r"}
	in, outside := Scope([]Finding{a, b, c, d}, map[string]bool{"F-0001": true})
	if !reflect.DeepEqual(in, []Finding{a, c}) || outside != 2 {
		t.Fatalf("Scope = %v, %d", in, outside)
	}
	in, outside = Scope(nil, map[string]bool{"F-0001": true})
	if len(in) != 0 || outside != 0 {
		t.Fatalf("empty Scope = %v, %d", in, outside)
	}
}
