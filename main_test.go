package main

import (
	"reflect"
	"testing"
)

func TestSplitCommaList(t *testing.T) {
	got := splitCommaList(" 日本, 香港 ,美国,, ")
	want := []string{"日本", "香港", "美国"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCommaList()=%#v，期望 %#v", got, want)
	}
}
