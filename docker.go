package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// dockerOverhead is the time allowed for starting and removing the container on top of the trace.
const dockerOverhead = 10

// TraceDocker traces src inside a throwaway, network-less container running the image's peek-a-go CLI.
func TraceDocker(ctx context.Context, image, src string) (*Trace, error) {
	ctx, cancel := context.WithTimeout(ctx, (debugServerWait+traceTimeout+dockerOverhead)*time.Second)
	defer cancel()

	name := "peek-a-go-" + strings.ToLower(rand.Text())
	cmd := exec.CommandContext(ctx, "docker", dockerArgs(name, image)...)
	// Killing the docker client doesn't stop the container.
	cmd.Cancel = func() error {
		return exec.Command("docker", "kill", name).Run()
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
			return nil, fmt.Errorf("docker: %w", err)
		}

		return nil, errors.New(msg)
	}

	var trace Trace
	if err := json.Unmarshal(stdout.Bytes(), &trace); err != nil {
		return nil, fmt.Errorf("decode trace: %w", err)
	}

	return &trace, nil
}

func dockerArgs(name, image string) []string {
	return []string{
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
		image,
		"/dev/stdin",
	}
}
