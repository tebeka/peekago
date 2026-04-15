package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHTMLHeapDemo(t *testing.T) {
	dest := os.Getenv("PKGO_HEAP_OUT")
	if dest == "" {
		t.Skip("set PKGO_HEAP_OUT to render the heap demo HTML")
	}

	source := "package main\n\n" +
		"type Node struct {\n\tName string\n\tNext *Node\n}\n\n" +
		"func main() {\n" +
		"\tc := &Node{Name: \"c\"}\n" +
		"\tb := &Node{Name: \"b\", Next: c}\n" +
		"\ta := &Node{Name: \"a\", Next: b}\n" +
		"\ttail := a.Next.Next\n" +
		"\t_ = tail\n" +
		"}\n"

	nodeType := "*main.Node"
	nodeVal := func(name, nextRef string) Value {
		next := Value{Kind: "nil", Type: nodeType}
		if nextRef != "" {
			next = Value{Kind: "ref", Type: nodeType, Ref: nextRef}
		}
		return Value{
			Kind: "struct", Type: "main.Node",
			Fields: []Field{
				{Name: "Name", Value: Value{Kind: "prim", Type: "string", Prim: "\"" + name + "\""}},
				{Name: "Next", Value: next},
			},
		}
	}

	heap := map[string]Value{
		"0xa": nodeVal("a", "0xb"),
		"0xb": nodeVal("b", "0xc"),
		"0xc": nodeVal("c", ""),
	}

	ref := func(id string) Value {
		return Value{Kind: "ref", Type: nodeType, Ref: id}
	}

	steps := []Step{
		{
			File: "main.go", Line: 11, PrevLine: 10, Function: "main.main",
			Frames: []Frame{{Func: "main.main", Locals: map[string]Value{
				"a":    ref("0xa"),
				"b":    ref("0xb"),
				"c":    ref("0xc"),
				"tail": ref("0xc"),
			}}},
			Globals: map[string]Value{},
			Heap:    heap,
		},
	}

	var buf bytes.Buffer
	if err := RenderHTML(&buf, "demo.go", source, steps); err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}

	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", dest, err)
	}
}

func TestRenderHTMLFixture(t *testing.T) {
	dir := filepath.Join("testdata/progs", "call_and_globals")
	src, err := os.ReadFile(filepath.Join(dir, "prog.go"))
	if err != nil {
		t.Fatalf("ReadFile(prog.go) error = %v", err)
	}

	var steps []Step
	readFixtureJSON(t, filepath.Join(dir, "trace.json"), &steps)

	var buf bytes.Buffer
	if err := RenderHTML(&buf, filepath.Join(dir, "prog.go"), string(src), steps); err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}

	out := buf.String()
	for _, need := range []string{"<title>Peek-A-Go", "const TRACE =", "const SOURCE =", "package main", "Frames", "Heap"} {
		if !strings.Contains(out, need) {
			t.Errorf("rendered HTML missing %q", need)
		}
	}

	if dest := os.Getenv("PKGO_HTML_OUT"); dest != "" {
		if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", dest, err)
		}
	}
}
