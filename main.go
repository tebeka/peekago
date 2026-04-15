package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
)

func main() {
	htmlOut := flag.String("html", "", "write an interactive HTML viewer to the given path instead of JSON")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <go-source-file>\n\n", path.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Trace a Go source file with headless Delve and print the execution trace as JSON.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "error: wrong number of arguments")
		os.Exit(1)
	}

	sourcePath := flag.Arg(0)

	trace, err := TraceFile(context.Background(), sourcePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *htmlOut != "" {
		out, err := os.Create(*htmlOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer out.Close()

		if err := RenderHTML(out, sourcePath, trace); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(trace); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
