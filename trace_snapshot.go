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

func snapshot(ctx context.Context, client *rpcClient, state *debuggerState, identities *traceIdentities) (Step, bool, error) {
	location := currentLocation(state)
	if filepath.Base(location.File) != traceFileName || location.Line == 0 {
		return Step{}, false, nil
	}

	goroutineID := currentGoroutineID(state)
	stack, err := client.stacktrace(ctx, goroutineID, 1)
	if err != nil {
		return Step{}, false, fmt.Errorf("stacktrace: %w", err)
	}

	builder := newGraphBuilder(identities)
	frames := make([]Frame, 0, len(stack)+1)

	globalsList, err := client.packageVars(ctx)
	if err != nil {
		return Step{}, false, fmt.Errorf("package vars: %w", err)
	}

	globalBindings := buildBindings(builder, globalsList, "global")
	if len(globalBindings) > 0 {
		frames = append(frames, Frame{
			ID:       "frame:global",
			Name:     "Global frame",
			Kind:     "global",
			Bindings: globalBindings,
		})
	}

	function := functionName(location.Function)
	if len(stack) > 0 {
		frame := stack[0]
		function = functionName(frame.Location.Function)
		bindings := buildFrameBindings(builder, frame, "frame:0")
		frames = append(frames, Frame{
			ID:        "frame:0",
			Name:      frameName(frame, 0),
			Kind:      "stack",
			Bindings:  bindings,
			IsCurrent: true,
		})
	}

	return Step{
		File:     traceFileName,
		Line:     location.Line,
		Function: function,
		Frames:   frames,
		Objects:  builder.snapshotObjects(),
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
	data, err := json.Marshal(step)
	if err != nil {
		return fmt.Sprintf("%s:%d:%s", step.File, step.Line, step.Function)
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

func frameName(frame stackFrame, index int) string {
	if name := functionName(frame.Location.Function); name != "" {
		return name
	}

	return fmt.Sprintf("frame %d", index)
}

func buildFrameBindings(builder *graphBuilder, frame stackFrame, scopeID string) []Binding {
	bindings := make([]Binding, 0, len(frame.Arguments)+len(frame.Locals))
	seen := map[string]bool{}

	for _, variable := range frame.Arguments {
		name := cleanVarName(variable.Name)
		if name == "" || seen[name] {
			continue
		}

		seen[name] = true
		bindings = append(bindings, Binding{
			Name:  name,
			Value: builder.valueForVariable(variable, scopeID+"."+name, true),
		})
	}

	for _, variable := range frame.Locals {
		name := cleanVarName(variable.Name)
		if name == "" || seen[name] {
			continue
		}

		seen[name] = true
		bindings = append(bindings, Binding{
			Name:  name,
			Value: builder.valueForVariable(variable, scopeID+"."+name, true),
		})
	}

	return bindings
}

func buildBindings(builder *graphBuilder, variables []variable, scopeID string) []Binding {
	bindings := make([]Binding, 0, len(variables))
	for _, variable := range variables {
		name := cleanVarName(variable.Name)
		if name == "" {
			continue
		}

		bindings = append(bindings, Binding{
			Name:  name,
			Value: builder.valueForVariable(variable, scopeID+"."+name, true),
		})
	}

	return bindings
}

type graphBuilder struct {
	identities *traceIdentities
	objects    map[string]Object
	order      []string
}

func newGraphBuilder(identities *traceIdentities) *graphBuilder {
	return &graphBuilder{
		identities: identities,
		objects:    map[string]Object{},
	}
}

func (g *graphBuilder) snapshotObjects() []Object {
	objects := make([]Object, 0, len(g.order))
	for _, id := range g.order {
		objects = append(objects, g.objects[id])
	}

	return objects
}

func (g *graphBuilder) addObject(obj Object) {
	if _, exists := g.objects[obj.ID]; !exists {
		g.order = append(g.order, obj.ID)
	}

	g.objects[obj.ID] = obj
}

func (g *graphBuilder) valueForVariable(variable variable, path string, preferPathID bool) Value {
	if variable.Unreadable != "" {
		return Value{Kind: "scalar", Display: "<unreadable>"}
	}

	if isNilVariable(variable) {
		return Value{Kind: "nil", Display: "nil"}
	}

	switch variable.Kind {
	case reflect.Slice:
		return g.sliceValue(variable, path)
	case reflect.Array:
		return g.arrayValue(variable, path, preferPathID)
	case reflect.Struct:
		return g.structValue(variable, path, preferPathID)
	case reflect.Map:
		return g.mapValue(variable, path, preferPathID)
	case reflect.Pointer:
		return g.pointerValue(variable, path, preferPathID)
	case reflect.Interface:
		return g.interfaceValue(variable, path, preferPathID)
	}

	return Value{
		Kind:    "scalar",
		Display: formatVariable(variable),
	}
}

func (g *graphBuilder) sliceValue(variable variable, path string) Value {
	objectID := "slice:" + path
	backingID := ""
	if variable.Base != 0 {
		backingID = g.identities.idForAddress("array", variable.Base)
	} else if len(variable.Children) > 0 {
		backingID = objectID + ":backing"
	}

	if backingID != "" {
		g.addObject(Object{
			ID:       backingID,
			Kind:     "array",
			Type:     "backing array",
			Label:    backingLabel(variable),
			Elements: g.elementValues(variable.Children, path+".backing"),
		})
	}

	g.addObject(Object{
		ID:              objectID,
		Kind:            "slice",
		Type:            variable.Type,
		Label:           sliceLabel(variable),
		Len:             variable.Len,
		Cap:             sliceCap(variable),
		BackingObjectID: backingID,
	})

	return Value{
		Kind:     "object",
		Display:  sliceLabel(variable),
		ObjectID: objectID,
	}
}

func (g *graphBuilder) arrayValue(variable variable, path string, preferPathID bool) Value {
	objectID := g.objectIDForVariable("array", variable, path, preferPathID)
	g.addObject(Object{
		ID:       objectID,
		Kind:     "array",
		Type:     variable.Type,
		Label:    variable.Type,
		Elements: g.elementValues(variable.Children, path+".array"),
	})

	return Value{
		Kind:     "object",
		Display:  variable.Type,
		ObjectID: objectID,
	}
}

func (g *graphBuilder) structValue(variable variable, path string, preferPathID bool) Value {
	objectID := g.objectIDForVariable("struct", variable, path, preferPathID)
	fields := make([]Binding, 0, len(variable.Children))
	for index, child := range variable.Children {
		name := strings.TrimSpace(child.Name)
		if name == "" {
			name = fmt.Sprintf("field%d", index)
		}

		fields = append(fields, Binding{
			Name:  name,
			Value: g.valueForVariable(child, path+"."+name, false),
		})
	}

	g.addObject(Object{
		ID:     objectID,
		Kind:   "struct",
		Type:   variable.Type,
		Label:  variable.Type,
		Fields: fields,
	})

	return Value{
		Kind:     "object",
		Display:  variable.Type,
		ObjectID: objectID,
	}
}

func (g *graphBuilder) mapValue(variable variable, path string, preferPathID bool) Value {
	objectID := g.objectIDForVariable("map", variable, path, preferPathID)
	entries := make([]MapEntry, 0, len(variable.Children)/2)
	for i := 0; i+1 < len(variable.Children); i += 2 {
		entries = append(entries, MapEntry{
			Key:   g.valueForVariable(variable.Children[i], fmt.Sprintf("%s.key%d", path, i/2), false),
			Value: g.valueForVariable(variable.Children[i+1], fmt.Sprintf("%s.value%d", path, i/2), false),
		})
	}

	g.addObject(Object{
		ID:      objectID,
		Kind:    "map",
		Type:    variable.Type,
		Label:   variable.Type,
		Entries: entries,
	})

	return Value{
		Kind:     "object",
		Display:  variable.Type,
		ObjectID: objectID,
	}
}

func (g *graphBuilder) pointerValue(variable variable, path string, preferPathID bool) Value {
	objectID := g.objectIDForVariable("pointer", variable, path, preferPathID)
	var target *Value
	if len(variable.Children) > 0 {
		value := g.valueForVariable(variable.Children[0], path+".target", false)
		target = &value
	}

	g.addObject(Object{
		ID:     objectID,
		Kind:   "pointer",
		Type:   variable.Type,
		Label:  variable.Type,
		Target: target,
	})

	return Value{
		Kind:     "object",
		Display:  variable.Type,
		ObjectID: objectID,
	}
}

func (g *graphBuilder) interfaceValue(variable variable, path string, preferPathID bool) Value {
	objectID := g.objectIDForVariable("interface", variable, path, preferPathID)
	var target *Value
	if len(variable.Children) > 0 {
		value := g.valueForVariable(variable.Children[0], path+".value", false)
		target = &value
	}

	g.addObject(Object{
		ID:     objectID,
		Kind:   "interface",
		Type:   variable.Type,
		Label:  variable.Type,
		Target: target,
	})

	return Value{
		Kind:     "object",
		Display:  variable.Type,
		ObjectID: objectID,
	}
}

func (g *graphBuilder) elementValues(children []variable, path string) []Value {
	values := make([]Value, 0, len(children))
	for index, child := range children {
		values = append(values, g.valueForVariable(child, fmt.Sprintf("%s[%d]", path, index), false))
	}

	return values
}

func (g *graphBuilder) objectIDForVariable(kind string, variable variable, path string, preferPathID bool) string {
	if variable.Addr != 0 && !preferPathID {
		return g.identities.idForAddress(kind, variable.Addr)
	}

	return kind + ":" + path
}

func isNilVariable(variable variable) bool {
	switch variable.Kind {
	case reflect.Pointer, reflect.Interface:
		return len(variable.Children) == 0
	case reflect.Slice:
		return variable.Base == 0 && variable.Len == 0 && variable.Cap == 0 && len(variable.Children) == 0
	case reflect.Map:
		return variable.Len == 0 && len(variable.Children) == 0 && variable.Addr == 0
	}

	return false
}

func sliceCap(variable variable) int64 {
	if variable.Cap > 0 {
		return variable.Cap
	}

	if variable.Len > 0 {
		return variable.Len
	}

	return int64(len(variable.Children))
}

func sliceLabel(variable variable) string {
	return fmt.Sprintf("%s len=%d cap=%d", variable.Type, variable.Len, sliceCap(variable))
}

func backingLabel(variable variable) string {
	return fmt.Sprintf("backing array (%d values)", len(variable.Children))
}

type traceIdentities struct {
	nextID    map[string]int
	addresses map[string]map[uint64]string
}

func newTraceIdentities() *traceIdentities {
	return &traceIdentities{
		nextID:    map[string]int{},
		addresses: map[string]map[uint64]string{},
	}
}

func (t *traceIdentities) idForAddress(kind string, addr uint64) string {
	if addr == 0 {
		t.nextID[kind]++
		return fmt.Sprintf("%s:%d", kind, t.nextID[kind])
	}

	if t.addresses[kind] == nil {
		t.addresses[kind] = map[uint64]string{}
	}

	if id, exists := t.addresses[kind][addr]; exists {
		return id
	}

	t.nextID[kind]++
	id := fmt.Sprintf("%s:%d", kind, t.nextID[kind])
	t.addresses[kind][addr] = id
	return id
}
