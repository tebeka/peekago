package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	traceFileName    = "main.go"
	traceModuleName  = "pkgo-trace"
	defaultStepLimit = 50
	debugServerWait  = 5
	detachTimeout    = 2
	traceTimeout     = 10
	maxStringLen     = 128
	maxArrayValues   = 16
	maxStructFields  = 16
	maxVariableDepth = 2
	stacktraceDepth  = 32
)

var (
	errStepLimit    = errors.New("trace step limit reached")
	errTraceTimeout = errors.New("trace timeout reached")
)

type Trace struct {
	Source string
	Steps  []Step
	Output string
}

type Step struct {
	File      string
	Line      int
	PrevLine  int
	Function  string
	Frames    []Frame
	Globals   map[string]Value
	Heap      map[string]Value
	StdoutLen int
}

type Frame struct {
	Func   string
	Locals map[string]Value
}

type Value struct {
	Kind   string  `json:"Kind,omitempty"`
	Type   string  `json:"Type,omitempty"`
	Prim   string  `json:"Prim,omitempty"`
	Ref    string  `json:"Ref,omitempty"`
	Fields []Field `json:"Fields,omitempty"`
	Elems  []Value `json:"Elems,omitempty"`
	Pairs  []Pair  `json:"Pairs,omitempty"`
	Len    int64   `json:"Len,omitempty"`
	Cap    int64   `json:"Cap,omitempty"`
	Offset int64   `json:"Offset,omitempty"`

	// elemSize is set on slice backing arrays only while a step is built, see shareBackingArrays.
	elemSize uint64
}

type Field struct {
	Name  string
	Value Value
}

type Pair struct {
	Key   Value
	Value Value
}

func TraceFile(ctx context.Context, name string) (*Trace, error) {
	src, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}

	return TraceSource(ctx, string(src))
}

func TraceSource(ctx context.Context, src string) (*Trace, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithTimeout(ctx, traceTimeout*time.Second)
	defer cancel()

	trace, err := traceSource(ctx, src)
	// The delve connection deadline can fire a moment before ctx is marked done.
	if err != nil && (ctx.Err() != nil || errors.Is(err, os.ErrDeadlineExceeded)) {
		return trace, errTraceTimeout
	}

	return trace, err
}

func traceSource(ctx context.Context, src string) (*Trace, error) {
	trace := &Trace{Source: src}

	tmpDir, err := os.MkdirTemp("", "pkgo-trace-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := writeTempProgram(tmpDir, src); err != nil {
		return nil, err
	}

	stdoutPath := filepath.Join(tmpDir, "stdout.log")
	if f, err := os.Create(stdoutPath); err != nil {
		return nil, fmt.Errorf("create stdout log: %w", err)
	} else {
		f.Close()
	}

	addr, err := reserveLoopbackAddress()
	if err != nil {
		return nil, err
	}

	dlv, err := startDelve(ctx, tmpDir, addr, stdoutPath)
	if err != nil {
		return nil, err
	}
	defer dlv.close()

	client, err := dialRPC(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("connect to delve: %w", err)
	}
	defer client.close()

	if err := client.setAPIVersion(ctx, 2); err != nil {
		return nil, fmt.Errorf("set delve api version: %w", err)
	}

	if err := client.createBreakpoint(ctx, "main.main"); err != nil {
		return nil, fmt.Errorf("create main breakpoint: %w", err)
	}

	state, err := client.command(ctx, debuggerCommand{Name: "continue"})
	if err != nil {
		return nil, fmt.Errorf("continue to main: %w", err)
	}

	if state == nil {
		return nil, errors.New("delve returned no debugger state")
	}

	finalize := func() *Trace {
		if data, err := os.ReadFile(stdoutPath); err == nil {
			trace.Output = string(data)
		}
		return trace
	}

	if state.Exited {
		return finalize(), exitedError(state)
	}

	mainGoroutineID := currentGoroutineID(state)
	if mainGoroutineID == 0 {
		return finalize(), errors.New("delve did not report the selected goroutine")
	}

	stdoutLen := func() int {
		info, err := os.Stat(stdoutPath)
		if err != nil {
			return 0
		}
		return int(info.Size())
	}

	var (
		seenState string
		prevLine  int
		arrays    = map[string]Value{} // slice backing arrays seen so far
	)

	for range defaultStepLimit {
		state, err = advanceToUserFrame(ctx, client, state, mainGoroutineID)
		if err != nil {
			return finalize(), err
		}

		if state.Exited {
			if len(trace.Steps) > 0 && state.ExitStatus == 0 {
				return finalize(), nil
			}

			return finalize(), exitedError(state)
		}

		step, ok, err := snapshot(ctx, client, state, prevLine, arrays)
		if err != nil {
			return finalize(), err
		}

		if ok {
			step.StdoutLen = stdoutLen()
			fingerprint := stepFingerprint(step)
			if fingerprint != seenState {
				trace.Steps = append(trace.Steps, step)
				seenState = fingerprint
				prevLine = step.Line
			}
		}

		state, err = client.command(ctx, debuggerCommand{Name: "step"})
		if err != nil {
			return finalize(), fmt.Errorf("step: %w", err)
		}
	}

	return finalize(), errStepLimit
}
