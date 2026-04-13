package main

import (
	"context"
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
	addr string
}

func validateAddr(addr string) error {
	_, err := net.ResolveTCPAddr("tcp", addr)
	return err
}

func main() {
	flag.StringVar(&options.addr, "http", ":8080", "HTTP listen address")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", path.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Start the Peek-A-Go web server.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 0 {
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
