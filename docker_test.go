package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Build the image first with: docker build -t peek-a-go .
const testImage = "peek-a-go"

func TestTraceDockerExamples(t *testing.T) {
	ensureImage(t)

	files, err := filepath.Glob("static/_examples/*.go")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", file, err)
			}

			trace, err := TraceDocker(context.Background(), testImage, string(src))
			if err != nil {
				t.Fatalf("TraceDocker() error = %v", err)
			}

			if len(trace.Steps) == 0 {
				t.Fatal("no steps")
			}
		})
	}
}

func TestTraceDockerError(t *testing.T) {
	ensureImage(t)

	src, err := os.ReadFile("testdata/progs/broken_program/prog.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	_, err = TraceDocker(context.Background(), testImage, string(src))
	if err == nil {
		t.Fatal("TraceDocker() error = nil, want error")
	}

	if strings.HasPrefix(err.Error(), "error: ") {
		t.Fatalf("error %q has CLI prefix", err)
	}
}

func TestTraceDockerNetHTTP(t *testing.T) {
	ensureImage(t)

	// The image caches net/http, without the cache building it takes ~20s.
	start := time.Now()
	src := `package main

import "net/http"

func main() {
	_ = http.StatusOK
}
`
	if _, err := TraceDocker(context.Background(), testImage, src); err != nil {
		t.Fatalf("TraceDocker() error = %v", err)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("TraceDocker() took %v, is net/http cached in the image?", elapsed)
	}
}

func TestTraceDockerCancelKillsContainer(t *testing.T) {
	ensureImage(t)

	src := `package main

import "time"

func main() {
	time.Sleep(time.Hour)
}
`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := TraceDocker(ctx, testImage, src); !errors.Is(err, errTraceTimeout) {
		t.Fatalf("TraceDocker() error = %v, want %v", err, errTraceTimeout)
	}

	// Without killing the container, TraceDocker waits for the in-container trace timeout.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("TraceDocker() took %v after cancel", elapsed)
	}

	out, err := exec.Command("docker", "ps", "-q", "--filter", "name=peek-a-go-").Output()
	if err != nil {
		t.Fatalf("docker ps error = %v", err)
	}

	if len(out) > 0 {
		t.Fatalf("containers still running: %s", out)
	}
}

func ensureImage(t *testing.T) {
	t.Helper()

	if err := exec.Command("docker", "image", "inspect", testImage).Run(); err != nil {
		t.Skipf("docker image %q not available", testImage)
	}
}
