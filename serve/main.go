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
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()
	log.Printf("serving %s on http://localhost%s", *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))))
}
