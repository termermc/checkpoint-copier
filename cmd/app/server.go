package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// Server entrypoint.
// inputPath is presumed to exist and be a directory.
func serverMain(inputPath string) {
	var addr string
	if addr = os.Getenv("SERVER_ADDR"); addr == "" {
		addr = "0.0.0.0"
		fmt.Printf("Using default address \"%s\". Use env var SERVER_ADDR to override.\n", addr)
	}

	var portStr string
	if portStr = os.Getenv("SERVER_PORT"); portStr == "" {
		portStr = "6655"
		fmt.Printf("Using default port \"%s\". Use env var SERVER_PORT to override.\n", portStr)
	}

	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		panic(err)
	}

	snap, err := ReadOrCreateSnapshot(inputPath)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/snapshot.jsonl", func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		for _, simple := range snap {
			err := enc.Encode(simple)
			if err != nil {
				panic(err)
			}
		}
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		notFound := func() {
			w.WriteHeader(404)
			_, _ = w.Write([]byte("not found"))
		}

		pathRel := r.URL.Query().Get("path")
		if pathRel == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("Missing \"path\" query param"))
			return
		}

		_, has := snap[pathRel]
		if !has {
			notFound()
			return
		}

		pathResolved := filepath.Join(inputPath, pathRel)

		var f *os.File
		f, err = os.Open(pathResolved)
		defer func() {
			_ = f.Close()
		}()

		http.ServeFile(w, r, pathResolved)
	})

	fmt.Printf("Listening on %s:%d\n", addr, port)
	err = http.ListenAndServe(fmt.Sprintf("%s:%d", addr, port), mux)
	if err != nil {
		panic(err)
	}
}
