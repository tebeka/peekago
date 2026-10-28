package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// dockerOverhead is the time allowed for starting and removing the container on top of the trace.
const dockerOverhead = 10

// dockerCmd is the container command, set DOCKER=podman to use podman.
var dockerCmd = cmp.Or(os.Getenv("DOCKER"), "docker")

// TraceDocker traces src inside a throwaway, network-less container running the image's peek-a-go CLI.
// A non-empty runtime selects the container runtime, e.g. runsc for gVisor.
func TraceDocker(ctx context.Context, image, runtime, src string) (*Trace, error) {
	ctx, cancel := context.WithTimeout(ctx, (debugServerWait+traceTimeout+dockerOverhead)*time.Second)
	defer cancel()

	name := "peek-a-go-" + strings.ToLower(rand.Text())
	cmd := exec.CommandContext(ctx, dockerCmd, dockerArgs(name, image, runtime)...)
	// Killing the docker client doesn't stop the container.
	cmd.Cancel = func() error {
		return exec.Command(dockerCmd, "kill", name).Run()
	}
	cmd.Stdin = strings.NewReader(src)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errTraceTimeout
		}

		msg := strings.TrimPrefix(strings.TrimSpace(stderr.String()), "error: ")
		if msg == "" {
			return nil, fmt.Errorf("%s: %w", dockerCmd, err)
		}

		return nil, errors.New(msg)
	}

	var trace Trace
	if err := json.Unmarshal(stdout.Bytes(), &trace); err != nil {
		return nil, fmt.Errorf("decode trace: %w", err)
	}

	return &trace, nil
}

func dockerArgs(name, image, runtime string) []string {
	args := []string{
		"run", "--rm", "-i",
		"--name", name,
		"--network=none",
		"--read-only",
		"--tmpfs", "/tmp:exec,size=128m",
		"--memory=512m",
		"--cpus=1",
		"--pids-limit=64",
		"--cap-drop=ALL",
		"--cap-add=SYS_PTRACE",
		"--security-opt=no-new-privileges",
	}
	if runtime != "" {
		args = append(args, "--runtime="+runtime)
	}

	return append(args, image, "/dev/stdin")
}

// gvisorRuntime is the gVisor container runtime.
const gvisorRuntime = "runsc"

// detectRuntime returns gvisorRuntime if the container command can use it, otherwise "" for the default runtime.
func detectRuntime() string {
	// Docker lists the runtimes registered with the daemon.
	out, err := exec.Command(dockerCmd, "info", "--format", "{{json .Runtimes}}").Output()
	if err == nil {
		if hasRuntime(out, gvisorRuntime) {
			return gvisorRuntime
		}
		return ""
	}

	// Podman doesn't list runtimes, but finds runsc in $PATH.
	if _, err := exec.LookPath(gvisorRuntime); err == nil {
		return gvisorRuntime
	}

	return ""
}

// hasRuntime reports whether the JSON output of docker info's .Runtimes contains name.
func hasRuntime(data []byte, name string) bool {
	var runtimes map[string]json.RawMessage
	if err := json.Unmarshal(data, &runtimes); err != nil {
		return false
	}

	_, ok := runtimes[name]
	return ok
}
