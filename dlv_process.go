package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func writeTempProgram(dir string, src string) error {
	mainPath := filepath.Join(dir, traceFileName)
	if err := os.WriteFile(mainPath, []byte(src), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", traceFileName, err)
	}

	goMod := fmt.Sprintf("module %s\n\ngo %s\n", traceModuleName, goDirective())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	return nil
}

func reserveLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve delve listen address: %w", err)
	}
	defer listener.Close()

	return listener.Addr().String(), nil
}

type delveProcess struct {
	cmd     *exec.Cmd
	output  *bytes.Buffer
	waitErr chan error
}

func startDelve(ctx context.Context, workDir, addr, stdoutPath string) (*delveProcess, error) {
	args := []string{
		"debug",
		"--headless",
		"--listen=" + addr,
		"--api-version=2",
		"--accept-multiclient",
	}
	if stdoutPath != "" {
		args = append(args, "-r", "stdout:"+stdoutPath)
	}
	cmd := exec.CommandContext(ctx, "dlv", args...)
	cmd.Dir = workDir
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start delve: %w", err)
	}

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	proc := &delveProcess{
		cmd:     cmd,
		output:  output,
		waitErr: waitErr,
	}

	deadline := time.NewTimer(time.Duration(debugServerWait) * time.Second)
	defer deadline.Stop()

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case err := <-waitErr:
			if err == nil {
				err = errors.New("delve exited before accepting connections")
			}

			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("delve did not start listening: %s", strings.TrimSpace(output.String()))
		case <-tick.C:
			conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return proc, nil
			}
		}
	}
}

func (p *delveProcess) close() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}

	_ = p.cmd.Process.Kill()
	select {
	case <-p.waitErr:
	case <-time.After(250 * time.Millisecond):
	}
}

func (p *delveProcess) String() string {
	if p == nil || p.output == nil {
		return ""
	}

	return strings.TrimSpace(p.output.String())
}

func goDirective() string {
	version := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return "1.26"
	}

	minor := strings.Builder{}
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			break
		}
		minor.WriteRune(r)
	}

	if minor.Len() == 0 {
		return "1.26"
	}

	return parts[0] + "." + minor.String()
}
