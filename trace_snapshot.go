package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
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

func snapshot(ctx context.Context, client *rpcClient, state *debuggerState, prevLine int, arrays map[string]Value) (Step, bool, error) {
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

	shareBackingArrays(outFrames, globals, heap, arrays)

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
		if v.Kind == reflect.Array {
			return Value{Kind: "array", Type: v.Type, Elems: elems}
		}

		if v.Base == 0 {
			return Value{Kind: "slice", Type: v.Type, Elems: elems}
		}

		var elemSize uint64
		if len(v.Children) >= 2 {
			elemSize = v.Children[1].Addr - v.Children[0].Addr
		}

		// The backing array lives on the heap so slices sharing it point to the same object.
		id := fmt.Sprintf("0x%x", v.Base)
		if existing, ok := heap[id]; !ok || len(existing.Elems) < len(elems) {
			heap[id] = Value{Kind: "array", Type: v.Type, Elems: elems, Cap: v.Cap, elemSize: elemSize}
		}

		return Value{Kind: "slice", Type: v.Type, Ref: id, Len: v.Len, Cap: v.Cap}

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

type sliceRegion struct {
	id    string
	start uint64
	end   uint64
	array Value
	live  bool // false for arrays remembered from earlier steps
}

type sliceView struct {
	id     string
	offset int64
}

// shareBackingArrays merges slice backing arrays whose memory overlaps, so a
// sub-slice such as xs[1:3] points into the same heap array as xs.
// known holds the backing arrays seen in earlier steps, so ys keeps pointing
// into xs's array after xs moves to a new one. It's updated with this step's arrays.
func shareBackingArrays(frames []Frame, globals, heap, known map[string]Value) {
	// Element size is known only for slices with 2+ elements, share it between slices of the same type.
	sizes := map[string]uint64{}
	for _, m := range []map[string]Value{known, heap} {
		for _, v := range m {
			if v.elemSize != 0 {
				sizes[v.Type] = v.elemSize
			}
		}
	}

	var regions []sliceRegion
	for id, v := range known {
		if _, ok := heap[id]; ok {
			continue
		}

		if r, ok := newSliceRegion(id, v, sizes[v.Type]); ok {
			regions = append(regions, r)
		}
	}
	for id, v := range heap {
		r, ok := newSliceRegion(id, v, sizes[v.Type])
		if !ok {
			continue
		}

		r.live = true
		regions = append(regions, r)
		delete(heap, id)
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].start < regions[j].start })

	views := map[string]sliceView{}
	for i := 0; i < len(regions); {
		first := regions[i]
		end := first.end
		live := first.live
		j := i + 1
		for j < len(regions) && regions[j].start < end && regions[j].array.Type == first.array.Type {
			end = max(end, regions[j].end)
			live = live || regions[j].live
			j++
		}

		if !live {
			i = j
			continue
		}

		size := sizes[first.array.Type]
		capacity := first.array.Cap
		if size != 0 {
			capacity = int64((end - first.start) / size)
		}

		var elems []Value
		for _, r := range regions[i:j] {
			var offset int64
			if size != 0 {
				offset = int64((r.start - first.start) / size)
			}
			for k, e := range r.array.Elems {
				for int64(len(elems)) <= offset+int64(k) {
					elems = append(elems, Value{})
				}
				elems[offset+int64(k)] = e
			}
			views[r.id] = sliceView{id: first.id, offset: offset}
		}

		heap[first.id] = Value{Kind: "array", Type: first.array.Type, Elems: elems, Cap: capacity}
		known[first.id] = Value{Kind: "array", Type: first.array.Type, Cap: capacity, elemSize: size}
		i = j
	}

	for _, frame := range frames {
		for name, v := range frame.Locals {
			frame.Locals[name] = linkSliceViews(v, views)
		}
	}
	for name, v := range globals {
		globals[name] = linkSliceViews(v, views)
	}
	for id, v := range heap {
		heap[id] = linkSliceViews(v, views)
	}
}

func newSliceRegion(id string, v Value, size uint64) (sliceRegion, bool) {
	if v.Kind != "array" || v.Cap == 0 {
		return sliceRegion{}, false
	}

	start, err := strconv.ParseUint(strings.TrimPrefix(id, "0x"), 16, 64)
	if err != nil {
		return sliceRegion{}, false
	}

	end := start + 1 // unknown element size: never overlaps another region
	if size != 0 {
		end = start + uint64(v.Cap)*size
	}

	return sliceRegion{id: id, start: start, end: end, array: v}, true
}

func linkSliceViews(v Value, views map[string]sliceView) Value {
	if view, ok := views[v.Ref]; ok && v.Kind == "slice" {
		v.Ref = view.id
		v.Offset = view.offset
	}

	for i := range v.Fields {
		v.Fields[i].Value = linkSliceViews(v.Fields[i].Value, views)
	}
	for i := range v.Elems {
		v.Elems[i] = linkSliceViews(v.Elems[i], views)
	}
	for i := range v.Pairs {
		v.Pairs[i].Key = linkSliceViews(v.Pairs[i].Key, views)
		v.Pairs[i].Value = linkSliceViews(v.Pairs[i].Value, views)
	}

	return v
}

func heapID(v variable) string {
	if v.Addr == 0 {
		return ""
	}
	return fmt.Sprintf("0x%x", v.Addr)
}
