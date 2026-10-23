# Peek-A-Go

Peek-A-Go traces a Go program step by step with headless Delve and prints a JSON execution trace.

The goal is to make Go execution easier to inspect and eventually support visualizations similar to Python Tutor.

## What It Does

- Accepts a single Go source file containing a runnable `package main`
- Writes the source into a temporary module
- Starts `dlv debug` in headless mode
- Steps through execution and captures:
  - source line
  - local variables
  - package-level variables
- Prints the collected trace as formatted JSON

## Requirements

- Go
- Delve (`dlv`) available on `PATH`

## Usage

Run directly:

```bash
go run . path/to/program.go
```

Show help:

```bash
go run . -h
```

Build a binary:

```bash
go build -o peek-a-go .
./peek-a-go path/to/program.go
```

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

## Output

The CLI prints a JSON array of steps like:

```json
[
  {
    "File": "main.go",
    "Line": 5,
    "Function": "",
    "Locals": {
      "x": "2"
    },
    "Globals": {
      "total": "3"
    }
  }
]
```

## Testing

The test suite uses fixture programs under `testdata/progs`.

Run tests with:

```bash
go test ./...
```

## Notes

- The current implementation traces a single linear execution timeline.
- It is intended for small runnable programs.
- The trace format is designed for downstream visualization tools.
