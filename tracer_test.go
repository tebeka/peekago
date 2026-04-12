package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

			var want []Step
			readFixtureJSON(t, filepath.Join(dir, "trace.json"), &want)
			if !reflect.DeepEqual(got, want) {
				gotJSON := mustJSON(t, got)
				wantJSON := mustJSON(t, want)
				t.Fatalf("TraceSource() mismatch\nwant:\n%s\ngot:\n%s", wantJSON, gotJSON)
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
