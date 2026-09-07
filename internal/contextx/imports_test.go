package contextx

import "testing"

func TestRelativeImportPaths_GoSkipsStdlib(t *testing.T) {
	src := `package p

import (
	"fmt"
	"./foo"
	"../bar"
)
`
	got := RelativeImportPaths("internal/pkg/file.go", src)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "internal/pkg/foo" {
		t.Errorf("foo: %s", got[0])
	}
	if got[1] != "internal/bar" {
		t.Errorf("bar: %s", got[1])
	}
}

func TestRelativeImportPaths_JS(t *testing.T) {
	src := `
import x from './util'
const y = require('../lib/helper')
import 'fmt'
`
	got := RelativeImportPaths("src/app.ts", src)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestRelativeImportPaths_Cap(t *testing.T) {
	src := "import (\n"
	for i := 0; i < 10; i++ {
		src += "\t\"./" + string(rune('a'+i)) + "\"\n"
	}
	src += ")\n"
	got := RelativeImportPaths("pkg/x.go", src)
	if len(got) > maxRelativeImports {
		t.Fatalf("cap exceeded: %d", len(got))
	}
}
