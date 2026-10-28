package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"syscall"
	"time"
)

var options struct {
	addr    string
	htmlOut string
	runner  string
	image   string
	runtime string
}

func validateAddr(addr string) error {
	_, err := net.ResolveTCPAddr("tcp", options.addr)
	return err
}

func main() {
	flag.StringVar(&options.addr, "http", ":8080", "HTTP listen address")
	flag.StringVar(&options.htmlOut, "html", "", "write an interactive HTML viewer to the given path instead of JSON")
	flag.StringVar(&options.runner, "runner", "exec", "how the web server runs programs: exec (directly, unsandboxed) or docker (in a sandboxed container, set $DOCKER to use another command such as podman)")
	flag.StringVar(&options.image, "image", "peek-a-go", "docker image for the docker runner")
	flag.StringVar(&options.runtime, "runtime", "auto", "container runtime for the docker runner: auto (gVisor's runsc if available), a runtime name such as runsc, or empty for the docker default")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [go-source-file]\n\n", path.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Without a source file, start the Peek-A-Go web server.")
		fmt.Fprintln(os.Stderr, "With a source file, trace it with headless Delve and print the execution trace as JSON (or HTML with -html).")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := validateAddr(options.addr); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid -http address %q: %s\n", options.addr, err)
		os.Exit(2)
	}

	if flag.NArg() == 1 {
		requireExecutable("dlv")
		if err := traceToFile(flag.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var trace traceFunc
	switch options.runner {
	case "exec":
		requireExecutable("dlv")
		trace = TraceSource
	case "docker":
		requireExecutable(dockerCmd)
		if options.runtime == "auto" {
			options.runtime = detectRuntime()
		}
		trace = func(ctx context.Context, src string) (*Trace, error) {
			return TraceDocker(ctx, options.image, options.runtime, src)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown -runner %q (want exec or docker)\n", options.runner)
		os.Exit(2)
	}

	server := newServer(trace)
	httpServer := &http.Server{
		Addr:              options.addr,
		Handler:           server.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      (debugServerWait + traceTimeout + dockerOverhead + 5) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "error", err)
		}
	}()

	slog.Info("server starting", "address", options.addr, "runner", options.runner, "runtime", options.runtime)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func requireExecutable(name string) {
	if _, err := exec.LookPath(name); err != nil {
		fmt.Fprintf(os.Stderr, "error: `%s` executable not found.\n", name)
		os.Exit(1)
	}
}

func traceToFile(sourcePath string) error {
	trace, err := TraceFile(context.Background(), sourcePath)
	if err != nil {
		return err
	}

	if options.htmlOut == "" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(trace)
	}

	out, err := os.Create(options.htmlOut)
	if err != nil {
		return err
	}
	defer out.Close()

	return RenderHTML(out, sourcePath, trace)
}
