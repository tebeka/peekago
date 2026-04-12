package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

const (
	traceFileName    = "main.go"
	traceModuleName  = "pkgo-trace"
	defaultStepLimit = 50
	debugServerWait  = 5
	commandTimeout   = 5
	maxStringLen     = 128
	maxArrayValues   = 16
	maxStructFields  = 16
	maxVariableDepth = 2
)

var errStepLimit = errors.New("trace step limit reached")

type Step struct {
	File     string
	Line     int
	Function string
	Frames   []Frame
	Objects  []Object
}

type Frame struct {
	ID        string
	Name      string
	Kind      string
	Bindings  []Binding
	IsCurrent bool
}

type Binding struct {
	Name  string
	Value Value
}

type Value struct {
	Kind     string
	Display  string
	ObjectID string `json:",omitempty"`
}

type Object struct {
	ID              string
	Kind            string
	Type            string     `json:",omitempty"`
	Label           string     `json:",omitempty"`
	Len             int64      `json:",omitempty"`
	Cap             int64      `json:",omitempty"`
	BackingObjectID string     `json:",omitempty"`
	Elements        []Value    `json:",omitempty"`
	Fields          []Binding  `json:",omitempty"`
	Entries         []MapEntry `json:",omitempty"`
	Target          *Value     `json:",omitempty"`
}

type MapEntry struct {
	Key   Value
	Value Value
}

func TraceFile(ctx context.Context, name string) ([]Step, error) {
	src, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}

	return TraceSource(ctx, string(src))
}

func TraceSource(ctx context.Context, src string) ([]Step, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tmpDir, err := os.MkdirTemp("", "pkgo-trace-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := writeTempProgram(tmpDir, src); err != nil {
		return nil, err
	}

	addr, err := reserveLoopbackAddress()
	if err != nil {
		return nil, err
	}

	dlv, err := startDelve(ctx, tmpDir, addr)
	if err != nil {
		return nil, err
	}
	defer dlv.close()

	client, err := dialRPC(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("connect to delve: %w", err)
	}
	defer client.close()

	if err := client.setAPIVersion(2); err != nil {
		return nil, fmt.Errorf("set delve api version: %w", err)
	}

	if err := client.createBreakpoint("main.main"); err != nil {
		return nil, fmt.Errorf("create main breakpoint: %w", err)
	}

	state, err := client.command(ctx, debuggerCommand{Name: "continue"})
	if err != nil {
		return nil, fmt.Errorf("continue to main: %w", err)
	}

	if state == nil {
		return nil, errors.New("delve returned no debugger state")
	}

	if state.Exited {
		return nil, exitedError(state)
	}

	mainGoroutineID := currentGoroutineID(state)
	if mainGoroutineID == 0 {
		return nil, errors.New("delve did not report the selected goroutine")
	}

	var (
		steps      []Step
		seenState  string
		identities = newTraceIdentities()
	)

	for range defaultStepLimit {
		state, err = advanceToUserFrame(ctx, client, state, mainGoroutineID)
		if err != nil {
			return steps, err
		}

		if state.Exited {
			if len(steps) > 0 && state.ExitStatus == 0 {
				return steps, nil
			}

			return steps, exitedError(state)
		}

		step, ok, err := snapshot(ctx, client, state, identities)
		if err != nil {
			return steps, err
		}

		if ok {
			fingerprint := stepFingerprint(step)
			if fingerprint != seenState {
				steps = append(steps, step)
				seenState = fingerprint
			}
		}

		state, err = client.command(ctx, debuggerCommand{Name: "step"})
		if err != nil {
			return steps, fmt.Errorf("step: %w", err)
		}
	}

	return steps, errStepLimit
}
