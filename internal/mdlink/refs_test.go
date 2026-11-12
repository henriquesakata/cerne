package mdlink

import (
	"reflect"
	"testing"
)

// F-0352: Refs keeps every occurrence with its 1-based file line, fenced and
// inline code excluded but still counted.
func TestRefs(t *testing.T) {
	content := "a [[F-0001]] b\n```\n[[F-0002]]\n```\n`[[F-0003]]` [[F-0001]] [x](./a.md#s) [e](https://x.y) [h](#top)\n"
	if got, want := Refs(content, true), []Ref{{"F-0001", 1}, {"F-0001", 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wiki refs = %v, want %v", got, want)
	}
	if got, want := Refs(content, false), []Ref{{"./a.md#s", 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("link refs = %v, want %v", got, want)
	}
}
