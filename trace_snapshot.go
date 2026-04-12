package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
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

func snapshot(ctx context.Context, client *rpcClient, state *debuggerState) (Step, bool, error) {
	location := currentLocation(state)
	if filepath.Base(location.File) != traceFileName || location.Line == 0 {
		return Step{}, false, nil
	}

	goroutineID := currentGoroutineID(state)
	frames, err := client.stacktrace(ctx, goroutineID)
	if err != nil {
		return Step{}, false, fmt.Errorf("stacktrace: %w", err)
	}

	locals := map[string]string{}
	function := functionName(location.Function)
	if len(frames) > 0 {
		function = functionName(frames[0].Location.Function)
		for _, arg := range frames[0].Arguments {
			if name := cleanVarName(arg.Name); name != "" {
				locals[name] = formatVariable(arg)
			}
		}

		for _, local := range frames[0].Locals {
			if name := cleanVarName(local.Name); name != "" {
				locals[name] = formatVariable(local)
			}
		}
	}

	globalsList, err := client.packageVars(ctx)
	if err != nil {
		return Step{}, false, fmt.Errorf("package vars: %w", err)
	}

	globals := map[string]string{}
	for _, global := range globalsList {
		name := cleanVarName(global.Name)
		if name == "" {
			continue
		}

		globals[name] = formatVariable(global)
	}

	return Step{
		File:     traceFileName,
		Line:     location.Line,
		Function: function,
		Locals:   locals,
		Globals:  globals,
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
	var b strings.Builder
	b.WriteString(step.File)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(step.Line))
	b.WriteByte(':')
	b.WriteString(step.Function)
	b.WriteByte('|')
	b.WriteString(stableMap(step.Locals))
	b.WriteByte('|')
	b.WriteString(stableMap(step.Globals))
	return b.String()
}

func stableMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte(';')
	}

	return b.String()
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

func formatVariable(v variable) string {
	if v.Unreadable != "" {
		return "<unreadable>"
	}

	if v.Kind == reflect.String {
		return strconv.Quote(v.Value)
	}

	if v.Value != "" && len(v.Children) == 0 {
		return v.Value
	}

	switch v.Kind {
	case reflect.Pointer:
		if len(v.Children) == 0 {
			return "nil"
		}

		return "&" + formatVariable(v.Children[0])
	case reflect.Struct:
		parts := make([]string, 0, len(v.Children))
		for _, child := range v.Children {
			label := child.Name
			if label == "" {
				label = "?"
			}
			parts = append(parts, label+":"+formatVariable(child))
		}
		return wrapType(v.Type, "{"+strings.Join(parts, ", ")+"}")
	case reflect.Array, reflect.Slice:
		if len(v.Children) == 0 && v.Base == 0 && v.Len == 0 {
			return "nil"
		}

		items := make([]string, 0, len(v.Children))
		for _, child := range v.Children {
			items = append(items, formatVariable(child))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case reflect.Map:
		if len(v.Children) == 0 && v.Len == 0 {
			return "nil"
		}

		items := make([]string, 0, len(v.Children)/2)
		for i := 0; i+1 < len(v.Children); i += 2 {
			items = append(items, formatVariable(v.Children[i])+":"+formatVariable(v.Children[i+1]))
		}
		return "map[" + strings.Join(items, ", ") + "]"
	case reflect.Interface:
		if len(v.Children) == 0 {
			return "nil"
		}

		return formatVariable(v.Children[0])
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		if v.Value != "" {
			return v.Value
		}
	}

	if len(v.Children) > 0 {
		items := make([]string, 0, len(v.Children))
		for _, child := range v.Children {
			items = append(items, formatVariable(child))
		}
		return wrapType(v.Type, "["+strings.Join(items, ", ")+"]")
	}

	if v.OnlyAddr && v.Addr != 0 {
		return fmt.Sprintf("%#x", v.Addr)
	}

	if v.Type != "" {
		return v.Type
	}

	return "nil"
}

func wrapType(typ string, value string) string {
	if typ == "" {
		return value
	}

	return typ + value
}
