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
}

func validateAddr(addr string) error {
	_, err := net.ResolveTCPAddr("tcp", options.addr)
	return err
}

func main() {
	flag.StringVar(&options.addr, "http", ":8080", "HTTP listen address")
	flag.StringVar(&options.htmlOut, "html", "", "write an interactive HTML viewer to the given path instead of JSON")
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

	if _, err := exec.LookPath("dlv"); err != nil {
		fmt.Fprintf(os.Stderr, "error: `dlv` executable not found.\n")
		os.Exit(1)
	}

	if flag.NArg() == 1 {
		if err := traceToFile(flag.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	server := newServer(TraceSource)
	httpServer := &http.Server{
		Addr:    options.addr,
		Handler: server.routes(),
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

	slog.Info("server starting", "address", options.addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
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
