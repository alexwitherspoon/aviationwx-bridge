package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const defaultImageDir = "/images"

type state struct {
	mu    sync.Mutex
	index map[string]int
}

func main() {
	imageDir := os.Getenv("SIM_IMAGE_DIR")
	if imageDir == "" {
		imageDir = defaultImageDir
	}
	st := &state{index: map[string]int{"cam-a": 0, "cam-b": 0}}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/control/state", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.index)
	})
	mux.HandleFunc("/control/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st.mu.Lock()
		for k := range st.index {
			st.index[k] = 0
		}
		st.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/control/advance", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st.mu.Lock()
		for k := range st.index {
			st.index[k]++
		}
		st.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/http/", func(w http.ResponseWriter, r *http.Request) {
		// /http/{cam}/snapshot.jpg
		cam := filepath.Base(filepath.Dir(r.URL.Path))
		if cam == "" || cam == "." {
			http.NotFound(w, r)
			return
		}
		st.mu.Lock()
		idx, ok := st.index[cam]
		st.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(imageDir, fmt.Sprintf("seq-%03d.jpg", idx+1))
		data, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, "image not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(data)
	})

	addr := ":8080"
	log.Printf("camera-simulator listening on %s images=%s", addr, imageDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
