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
go run . -listen :9090
```

Show help:

```bash
go run . -h
```

Then open `http://localhost:8080`.

Build a binary:

```bash
go build -o peek-a-go .
./peek-a-go -listen :8080
```

## Web UI

The browser UI lets you:

- paste or edit Go source
- run the tracer
- move backward and forward through the recorded steps
- inspect the current line, locals, and globals

## JSON API

The server exposes one JSON endpoint:

- `POST /trace`

Request body:

```json
{
  "code": "package main\n\nfunc main() {}\n"
}
```

Response body:

```json
{
  "steps": [
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
