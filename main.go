package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "HTTP listen address")
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

	server := newServer(TraceSource)
	log.Printf("listening on %s", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, server.routes()); err != nil {
		log.Fatal(err)
	}
}
