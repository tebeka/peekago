package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// normalizeTrace rewrites every heap ID to a stable #N label so fixtures
// are deterministic across runs (heap addresses vary due to ASLR). Labels
// are assigned in traversal order: per step, frames (top-first, locals
// sorted) → globals (sorted) → heap (sorted keys). The same address gets
// the same label across all steps in the trace.
func normalizeTrace(t *Trace) *Trace {
	if t == nil {
		return nil
	}

	mapping := map[string]string{}
	assign := func(id string) string {
		if n, ok := mapping[id]; ok {
			return n
		}
		n := fmt.Sprintf("#%d", len(mapping)+1)
		mapping[id] = n
		return n
	}

	var fixValue func(Value) Value
	fixValue = func(v Value) Value {
		if v.Ref != "" {
			v.Ref = assign(v.Ref)
		}
		for i := range v.Fields {
			v.Fields[i].Value = fixValue(v.Fields[i].Value)
		}
		for i := range v.Elems {
			v.Elems[i] = fixValue(v.Elems[i])
		}
		for i := range v.Pairs {
			v.Pairs[i].Key = fixValue(v.Pairs[i].Key)
			v.Pairs[i].Value = fixValue(v.Pairs[i].Value)
		}
		return v
	}

	fixMap := func(m map[string]Value) map[string]Value {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]Value, len(m))
		for _, k := range keys {
			out[k] = fixValue(m[k])
		}
		return out
	}

	out := *t
	out.Steps = make([]Step, len(t.Steps))
	for i, s := range t.Steps {
		ns := s
		ns.Frames = make([]Frame, len(s.Frames))
		for j, f := range s.Frames {
			ns.Frames[j] = Frame{Func: f.Func, Locals: fixMap(f.Locals)}
		}
		ns.Globals = fixMap(s.Globals)

		hkeys := make([]string, 0, len(s.Heap))
		for k := range s.Heap {
			hkeys = append(hkeys, k)
		}
		sort.Strings(hkeys)
		nh := make(map[string]Value, len(s.Heap))
		for _, k := range hkeys {
			v := fixValue(s.Heap[k])
			nh[assign(k)] = v
		}
		ns.Heap = nh
		out.Steps[i] = ns
	}
	return &out
}

func TestTraceSourceFixtures(t *testing.T) {
	ensureDelve(t)

	entries, err := os.ReadDir("testdata/progs")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			dir := filepath.Join("testdata/progs", entry.Name())
			src := readFixtureFile(t, dir, "*.go")
			got, err := TraceSource(context.Background(), src)
			if errorKind, ok := readOptionalText(t, filepath.Join(dir, "error.txt")); ok {
				checkFixtureError(t, err, errorKind)
				return
			}

			if err != nil {
				t.Fatalf("TraceSource() error = %v", err)
			}

			gotNorm := normalizeTrace(got)

			traceFile := filepath.Join(dir, "trace.json")
			if os.Getenv("PKGO_REGEN") != "" {
				data := mustJSON(t, gotNorm)
				if err := os.WriteFile(traceFile, append([]byte(data), '\n'), 0o644); err != nil {
					t.Fatalf("WriteFile(%s) error = %v", traceFile, err)
				}
				return
			}

			var want Trace
			readFixtureJSON(t, traceFile, &want)
			wantNorm := normalizeTrace(&want)
			if !reflect.DeepEqual(gotNorm, wantNorm) {
				gotJSON := mustJSON(t, gotNorm)
				wantJSON := mustJSON(t, wantNorm)
				t.Fatalf("TraceSource() mismatch\nwant:\n%s\ngot:\n%s", wantJSON, gotJSON)
			}
		})
	}
}

func TestTraceSourceExamples(t *testing.T) {
	ensureDelve(t)

	files, err := filepath.Glob("static/_examples/*.go")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if len(files) == 0 {
		t.Fatal("no examples found")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", file, err)
			}

			if _, err := TraceSource(context.Background(), string(src)); err != nil {
				t.Fatalf("TraceSource() error = %v", err)
			}
		})
	}
}

func ensureDelve(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv not available")
	}
}

func readFixtureFile(t *testing.T, dir string, pattern string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 %s file in %s, got %d", pattern, dir, len(matches))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", matches[0], err)
	}

	return string(data)
}

func readFixtureJSON(t *testing.T, name string, out any) {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}

	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", name, err)
	}
}

func readOptionalText(t *testing.T, name string) (string, bool) {
	t.Helper()

	data, err := os.ReadFile(name)
	if err == nil {
		return strings.TrimSpace(string(data)), true
	}

	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}

	t.Fatalf("ReadFile(%s) error = %v", name, err)
	return "", false
}

func checkFixtureError(t *testing.T, err error, kind string) {
	t.Helper()

	switch kind {
	case "non_nil":
		if err == nil {
			t.Fatal("TraceSource() error = nil, want non-nil")
		}
	case "step_limit":
		if !errors.Is(err, errStepLimit) {
			t.Fatalf("TraceSource() error = %v, want %v", err, errStepLimit)
		}
	case "timeout":
		if !errors.Is(err, errTraceTimeout) {
			t.Fatalf("TraceSource() error = %v, want %v", err, errTraceTimeout)
		}
	default:
		t.Fatalf("unknown error fixture kind %q", kind)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}

	return string(data)
}

func TestTraceSourceHidesEnv(t *testing.T) {
	ensureDelve(t)
	t.Setenv("PKGO_SECRET", "s3cret")

	src := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("PKGO_SECRET"))
}
`
	trace, err := TraceSource(context.Background(), src)
	if err != nil {
		t.Fatalf("TraceSource() error = %v", err)
	}

	if strings.Contains(trace.Output, "s3cret") {
		t.Fatalf("traced program read the server environment: %q", trace.Output)
	}
}

func TestTraceSourceKillsChildren(t *testing.T) {
	ensureDelve(t)

	src := `package main

import "os/exec"

func main() {
	exec.Command("sleep", "31337").Start()
}
`
	if _, err := TraceSource(context.Background(), src); err != nil {
		t.Fatalf("TraceSource() error = %v", err)
	}

	// pgrep exits with 1 when no process matches.
	if out, err := exec.Command("pgrep", "-f", "sleep 31337").Output(); err == nil {
		exec.Command("pkill", "-f", "sleep 31337").Run()
		t.Fatalf("child process survived the trace: %s", out)
	}
}
