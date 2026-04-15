package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

func advanceToUserFrame(ctx context.Context, client *rpcClient, state *debuggerState, mainGoroutineID int64) (*debuggerState, error) {
	if currentGoroutineID(state) != mainGoroutineID {
		var err error
		state, err = client.command(ctx, debuggerCommand{
			Name:        "switchGoroutine",
			GoroutineID: mainGoroutineID,
		})
		if err != nil {
			return nil, fmt.Errorf("switch goroutine: %w", err)
		}
	}

	for !state.Exited && !isUserFrame(state) {
		var err error
		state, err = client.command(ctx, debuggerCommand{Name: "stepOut"})
		if err != nil {
			return nil, fmt.Errorf("step out: %w", err)
		}
	}

	return state, nil
}

func snapshot(ctx context.Context, client *rpcClient, state *debuggerState, prevLine int) (Step, bool, error) {
	location := currentLocation(state)
	if filepath.Base(location.File) != traceFileName || location.Line == 0 {
		return Step{}, false, nil
	}

	goroutineID := currentGoroutineID(state)
	frames, err := client.stacktrace(ctx, goroutineID)
	if err != nil {
		return Step{}, false, fmt.Errorf("stacktrace: %w", err)
	}

	heap := map[string]Value{}

	outFrames := []Frame{}
	for _, frame := range frames {
		if filepath.Base(frame.File) != traceFileName {
			continue
		}

		locals := map[string]Value{}
		for _, arg := range frame.Arguments {
			if name := cleanVarName(arg.Name); name != "" {
				locals[name] = walkValue(arg, heap)
			}
		}

		for _, local := range frame.Locals {
			if name := cleanVarName(local.Name); name != "" {
				locals[name] = walkValue(local, heap)
			}
		}

		outFrames = append(outFrames, Frame{
			Func:   functionName(frame.Function),
			Locals: locals,
		})
	}

	function := functionName(location.Function)
	if len(outFrames) > 0 {
		function = outFrames[0].Func
	}

	globalsList, err := client.packageVars(ctx)
	if err != nil {
		return Step{}, false, fmt.Errorf("package vars: %w", err)
	}

	globals := map[string]Value{}
	for _, global := range globalsList {
		name := cleanVarName(global.Name)
		if name == "" {
			continue
		}

		globals[name] = walkValue(global, heap)
	}

	return Step{
		File:     traceFileName,
		Line:     location.Line,
		PrevLine: prevLine,
		Function: function,
		Frames:   outFrames,
		Globals:  globals,
		Heap:     heap,
	}, true, nil
}

func currentLocation(state *debuggerState) location {
	if state == nil {
		return location{}
	}

	if state.CurrentThread != nil {
		return location{
			File:     state.CurrentThread.File,
			Line:     state.CurrentThread.Line,
			Function: state.CurrentThread.Function,
		}
	}

	if state.SelectedGoroutine != nil {
		return state.SelectedGoroutine.UserCurrentLoc
	}

	return location{}
}

func currentGoroutineID(state *debuggerState) int64 {
	if state == nil {
		return 0
	}

	if state.SelectedGoroutine != nil && state.SelectedGoroutine.ID != 0 {
		return state.SelectedGoroutine.ID
	}

	if state.CurrentThread != nil {
		return state.CurrentThread.GoroutineID
	}

	return 0
}

func isUserFrame(state *debuggerState) bool {
	return filepath.Base(currentLocation(state).File) == traceFileName
}

func cleanVarName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, traceModuleName+".")
	name = strings.TrimPrefix(name, "main.")
	if name == "" || strings.Contains(name, ".") || strings.HasPrefix(name, "~") || strings.HasPrefix(name, "_cgo") {
		return ""
	}

	return name
}

func functionName(fn *function) string {
	if fn == nil {
		return ""
	}

	return fn.Name_
}

func exitedError(state *debuggerState) error {
	if state == nil {
		return errors.New("debuggee exited")
	}

	if state.ExitStatus == 0 {
		return nil
	}

	return fmt.Errorf("debuggee exited with status %d", state.ExitStatus)
}

func stepFingerprint(step Step) string {
	tmp := step
	tmp.PrevLine = 0
	data, err := json.Marshal(tmp)
	if err != nil {
		return ""
	}
	return string(data)
}

func defaultLoadConfig() *loadConfig {
	return &loadConfig{
		FollowPointers:     true,
		MaxVariableRecurse: maxVariableDepth,
		MaxStringLen:       maxStringLen,
		MaxArrayValues:     maxArrayValues,
		MaxStructFields:    maxStructFields,
	}
}

func walkValue(v variable, heap map[string]Value) Value {
	if v.Unreadable != "" {
		return Value{Kind: "prim", Type: v.Type, Prim: "<unreadable>"}
	}

	switch v.Kind {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return Value{Kind: "prim", Type: v.Type, Prim: v.Value}

	case reflect.String:
		return Value{Kind: "prim", Type: v.Type, Prim: strconv.Quote(v.Value)}

	case reflect.Pointer:
		if len(v.Children) == 0 {
			return Value{Kind: "nil", Type: v.Type}
		}
		child := v.Children[0]
		id := heapID(child)
		if id == "" {
			return walkValue(child, heap)
		}
		if _, ok := heap[id]; !ok {
			heap[id] = Value{}
			heap[id] = walkValue(child, heap)
		}
		return Value{Kind: "ref", Type: v.Type, Ref: id}

	case reflect.Struct:
		fields := make([]Field, 0, len(v.Children))
		for _, child := range v.Children {
			name := child.Name
			if name == "" {
				name = "?"
			}
			fields = append(fields, Field{Name: name, Value: walkValue(child, heap)})
		}
		return Value{Kind: "struct", Type: v.Type, Fields: fields}

	case reflect.Slice, reflect.Array:
		if len(v.Children) == 0 && v.Base == 0 && v.Len == 0 {
			return Value{Kind: "nil", Type: v.Type}
		}
		elems := make([]Value, 0, len(v.Children))
		for _, child := range v.Children {
			elems = append(elems, walkValue(child, heap))
		}
		kind := "slice"
		if v.Kind == reflect.Array {
			kind = "array"
		}
		return Value{Kind: kind, Type: v.Type, Elems: elems}

	case reflect.Map:
		if len(v.Children) == 0 && v.Len == 0 {
			return Value{Kind: "nil", Type: v.Type}
		}
		pairs := make([]Pair, 0, len(v.Children)/2)
		for i := 0; i+1 < len(v.Children); i += 2 {
			pairs = append(pairs, Pair{
				Key:   walkValue(v.Children[i], heap),
				Value: walkValue(v.Children[i+1], heap),
			})
		}
		return Value{Kind: "map", Type: v.Type, Pairs: pairs}

	case reflect.Interface:
		if len(v.Children) == 0 {
			return Value{Kind: "nil", Type: v.Type}
		}
		return walkValue(v.Children[0], heap)
	}

	if v.Value != "" {
		return Value{Kind: "prim", Type: v.Type, Prim: v.Value}
	}

	if v.OnlyAddr && v.Addr != 0 {
		return Value{Kind: "prim", Type: v.Type, Prim: fmt.Sprintf("%#x", v.Addr)}
	}

	return Value{Kind: "nil", Type: v.Type}
}

func heapID(v variable) string {
	if v.Addr == 0 {
		return ""
	}
	return fmt.Sprintf("0x%x", v.Addr)
}
