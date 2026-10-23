# Peek-A-Go Visualizer Plan

Goal: a Python-Tutor-like step-through visualization of a Go program's
execution, driven by the existing `dlv`-based tracer.

## Reference: what Python Tutor shows

- Code panel with a highlighted "just executed" line and a "next line" arrow.
- Step controls: First / Prev / Next / Last, a slider, and "Step N of M".
- Print output panel with stdout accumulated up to the current step.
- Frames column: global frame plus one box per active call, each listing
  locals.
- Objects column: heap objects (functions, lists, dicts, instances) with SVG
  arrows from frame slots to objects so aliasing is visible.

## What pkgo has today

- CLI only. `TraceFile` runs `dlv debug` headless, steps the main goroutine,
  and emits `[]Step` as JSON.
- `Step` is flat: `File`, `Line`, `Function`, `Locals map[string]string`,
  `Globals map[string]string` (`trace.go`).
- Only the top frame is captured — the stacktrace request uses `Depth: 1`
  (`dlv_rpc.go`).
- Variables are stringified eagerly in `formatVariable`
  (`trace_snapshot.go`). No identity, no sharing, no pointer graph.
- No stdout capture, no static assets, no HTTP server.

## Gap analysis

| Need                    | Today                | Change                                  |
| ----------------------- | -------------------- | --------------------------------------- |
| Call stack              | top frame only       | fetch full stacktrace each step         |
| Heap objects w/ id      | strings              | emit `{id, kind, fields}`, key on `Addr` |
| Pointer edges           | flattened `&{...}`   | frame slot → heap id refs               |
| Stdout per step         | not captured         | tee debuggee stdout, record byte offset |
| Line + next-line arrow  | `Line` only          | also record previous line               |
| Viewer                  | none                 | static HTML/JS fed by the trace         |

## Phased plan

### v1 — UI on top of the existing trace (this branch)

Ship something demoable without touching the tracer:

1. Embed a single-file HTML viewer via `go:embed`.
2. Add a `-html <out>` flag to `main.go` that renders the viewer with the
   trace and the source inlined as JSON.
3. Viewer layout:
   - Left: source with current line highlighted.
   - Middle: locals for the current step.
   - Right: globals for the current step.
   - Bottom: First / Prev / Next / Last + slider + "Step N of M".
4. Keep the default CLI behavior (JSON to stdout) unchanged.

### v2 — richer trace model

Once v1 is usable, replace stringified variables with a structured model:

- New types: `Trace { Source, Steps, Output }`,
  `Step { Line, PrevLine, Frames, Globals, Heap, StdoutLen }`,
  `Frame { Func, Locals map[string]Value }`,
  `Value { Kind, Prim, Ref }`,
  `HeapObject { ID, Kind, Type, Fields/Elems []Value }`.
- Walk each Delve `variable` recursively. Assign IDs from `Addr` (fallback
  synthetic for non-addressable). Dedupe across frames so a shared object
  appears once in the heap.

### v3 — full stack + stdout

- Bump stacktrace `Depth` (e.g. 32) and filter to frames inside `main.go`,
  populating `Frames` in call order.
- Capture debuggee stdout and store cumulative length per step.

### v4 — pointer arrows in the viewer

- Render the heap column and draw SVG arrows from frame slots to heap
  objects (positions measured with `getBoundingClientRect`).

### v5 — tests and fixtures

- Add fixtures under `testdata/progs` that exercise pointer sharing, slices,
  maps, and recursion. Snapshot-test the richer JSON.

## Tradeoffs

- v1 reuses the current trace, so aliasing and pointer arrows are not
  possible — locals just show the stringified values already produced. This
  is fine for a demo and defers the biggest chunk of work (heap identity) to
  v2.
- Keeping the viewer as a single self-contained HTML file (no dev server, no
  bundler) trades polish for simplicity. Good enough until the trace model
  catches up.
