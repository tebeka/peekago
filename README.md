# Peek-A-Go

Peek-A-Go traces a Go program step by step with headless Delve and serves a browser UI for exploring the execution trace.

The goal is to make Go execution easier to inspect and eventually support visualizations similar to Python Tutor.

## What It Does

- Serves a web UI with a code editor and step-by-step trace viewer
- Accepts a runnable `package main` program pasted into the browser
- Writes the source into a temporary module
- Starts `dlv debug` in headless mode
- Steps through execution and captures:
  - source line
  - local variables
  - package-level variables
- Returns the collected trace as JSON to the browser

## Requirements

- Go
- Delve (`dlv`) available on `PATH`

## Usage

Start the server:

```bash
go run .
```

Listen on another address:

```bash
go run . -http :9090
```

Show help:

```bash
go run . -h
```

Then open `http://localhost:8080`.

Build a binary:

```bash
go build -o peek-a-go .
./peek-a-go -http :8080
```

Trace a single file from the command line and print the trace as JSON:

```bash
go run . path/to/program.go
```

## Web UI

The browser UI lets you:

- paste or edit Go source
- run the tracer
- step through the trace in the HTML viewer

## HTML Viewer

Write an interactive step-through viewer instead of JSON:

```bash
go run . -html out.html path/to/program.go
```

The page shows the source, the call stack, the heap with pointer arrows, and stdout. Step with the buttons, the slider, or the arrow keys.

Examples in [`docs`](docs):

- [Linked list](docs/linked-list.html): pointer arrows from locals and struct fields
- [Pointer aliasing](docs/pointer-aliasing.html): two pointers to the same heap object
- [Recursion](docs/recursion.html): a multi-frame call stack
- [Stdout](docs/stdout.html): program output so far, and package-level variables
- [Slice mutation](docs/slice-mutation.html): `append` moving a slice to a new backing array
- [Slice view](docs/slice-view.html): a sub-slice viewing part of another slice's backing array
- [Map grow](docs/map-grow.html): adding a key to a map

## JSON API

The server exposes one JSON endpoint:

- `POST /trace`

Request body:

```json
{
  "code": "package main\n\nfunc main() {\n\ta := 1\n}\n"
}
```

Response body:

```json
{
  "trace": {
    "Source": "package main\n\nfunc main() {\n\ta := 1\n}\n",
    "Steps": [
      {
        "File": "main.go",
        "Line": 5,
        "PrevLine": 4,
        "Function": "main.main",
        "Frames": [
          {
            "Func": "main.main",
            "Locals": {
              "a": {"Kind": "prim", "Type": "int", "Prim": "1"}
            }
          }
        ],
        "Globals": {},
        "Heap": {},
        "StdoutLen": 0
      }
    ],
    "Output": ""
  }
}
```

On errors the endpoint returns:

```json
{
  "error": "message"
}
```

## Testing

The trace engine tests use fixture programs under `testdata/progs`.

Run tests with:

```bash
go test ./...
```

## Notes

- The current implementation traces a single linear execution timeline.
- It is intended for small runnable programs.
- The web UI uses plain JavaScript and no frontend framework.
- The embedded editor is CodeMirror 5, vendored under `static/vendor/codemirror`.
- CodeMirror 5 is MIT-licensed; its license text is included in `static/vendor/codemirror/LICENSE`.
