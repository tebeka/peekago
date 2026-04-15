package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	for _, need := range []string{"<title>Peek-A-Go", "const TRACE =", "const SOURCE =", "package main"} {
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
