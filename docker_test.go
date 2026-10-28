package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Build the image first with: docker build -t peek-a-go . (or podman with DOCKER=podman)
const testImage = "peek-a-go"

// testRuntime is the container runtime for the tests, set PEEKAGO_RUNTIME=runsc to test under gVisor.
var testRuntime = os.Getenv("PEEKAGO_RUNTIME")

func TestHasRuntime(t *testing.T) {
	tests := []struct {
		data string
		want bool
	}{
		{`{"io.containerd.runc.v2":{"path":"runc"},"runc":{"path":"runc"},"runsc":{"path":"/usr/local/bin/runsc"}}`, true},
		{`{"io.containerd.runc.v2":{"path":"runc"},"runc":{"path":"runc"}}`, false},
		{`not json`, false},
	}

	for _, tc := range tests {
		if got := hasRuntime([]byte(tc.data), "runsc"); got != tc.want {
			t.Errorf("hasRuntime(%s) = %v, want %v", tc.data, got, tc.want)
		}
	}
}

func TestDockerArgsRuntime(t *testing.T) {
	if args := strings.Join(dockerArgs("n", "img", ""), " "); strings.Contains(args, "--runtime") {
		t.Fatalf("dockerArgs() = %q, want no --runtime", args)
	}

	args := dockerArgs("n", "img", "runsc")
	if !slices.Contains(args, "--runtime=runsc") {
		t.Fatalf("dockerArgs() = %q, want --runtime=runsc", args)
	}

	// Flags after the image are passed to the container's entrypoint.
	if i := slices.Index(args, "img"); i < slices.Index(args, "--runtime=runsc") {
		t.Fatalf("dockerArgs() = %q, --runtime after image", args)
	}
}

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

			trace, err := TraceDocker(context.Background(), testImage, testRuntime, string(src))
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

	_, err = TraceDocker(context.Background(), testImage, testRuntime, string(src))
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
	if _, err := TraceDocker(context.Background(), testImage, testRuntime, src); err != nil {
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
	if _, err := TraceDocker(ctx, testImage, testRuntime, src); !errors.Is(err, errTraceTimeout) {
		t.Fatalf("TraceDocker() error = %v, want %v", err, errTraceTimeout)
	}

	// Without killing the container, TraceDocker waits for the in-container trace timeout.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("TraceDocker() took %v after cancel", elapsed)
	}

	out, err := exec.Command(dockerCmd, "ps", "-q", "--filter", "name=peek-a-go-").Output()
	if err != nil {
		t.Fatalf("docker ps error = %v", err)
	}

	if len(out) > 0 {
		t.Fatalf("containers still running: %s", out)
	}
}

func ensureImage(t *testing.T) {
	t.Helper()

	if err := exec.Command(dockerCmd, "image", "inspect", testImage).Run(); err != nil {
		t.Skipf("docker image %q not available", testImage)
	}
}
