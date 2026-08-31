package inventory

import (
	"reflect"
	"testing"
)

func TestParseAndFormatCPUList(t *testing.T) {
	ids, err := ParseCPUList("0-3,8,10-11")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, 2, 3, 8, 10, 11}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	if got := FormatCPUList(ids); got != "0-3,8,10-11" {
		t.Fatalf("got %q", got)
	}
}
