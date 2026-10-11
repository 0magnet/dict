//go:build !js && !wasip1

package cmd

import (
	"log"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var serveAddr, serveDir string

// serveCmd hosts a directory over HTTP so the wasm demo can be opened in a
// browser. Go serves .wasm with the right MIME type, and this needs only the
// Go toolchain this project already uses.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve the wasm demo directory over HTTP",
	Long:  "Host a directory over HTTP so the wasm demo can be opened in a browser.",
	Args:  cobra.NoArgs,
	RunE: func(*cobra.Command, []string) error {
		log.Printf("serving %s on http://localhost%s", serveDir, serveAddr)
		// Timeouts, where http.ListenAndServe would have none. The write
		// timeout has to be generous: what it serves includes a 22 MB .wasm.
		srv := &http.Server{
			Addr:              serveAddr,
			Handler:           http.FileServer(http.Dir(serveDir)),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      5 * time.Minute,
			IdleTimeout:       2 * time.Minute,
		}
		return srv.ListenAndServe()
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&serveDir, "dir", ".", "directory to serve")
	RootCmd.AddCommand(serveCmd)
}
