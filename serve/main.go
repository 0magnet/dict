// Command serve hosts a directory over HTTP so the wasm demo can be opened in
// a browser — Go serves .wasm with the right MIME type, and this needs only
// the Go toolchain this project already uses.
//
//	go run ./serve                     # http://localhost:8080, current dir
//	go run ./serve -addr :9000 -dir x  # a different port and directory
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()
	log.Printf("serving %s on http://localhost%s", *dir, *addr)

	// Timeouts, where http.ListenAndServe would have none. This serves a
	// directory to a browser on the same machine and nothing here is
	// load-bearing, but the write timeout still has to be generous: what it
	// serves includes a 22 MB .wasm.
	srv := &http.Server{
		Addr:              *addr,
		Handler:           http.FileServer(http.Dir(*dir)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(srv.ListenAndServe())
}
