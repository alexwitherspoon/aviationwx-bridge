// Local mock of api.aviationwx.org/v1/bridge/* for weather wire capture.
// Serves HTTPS with an ephemeral self-signed cert (pair with AVIATIONWX_API_TLS_INSECURE=1).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const maxCaptured = 40

type captureStore struct {
	mu       sync.Mutex
	items    []json.RawMessage
	failNext int // 0 = accept; >0 = remaining 503s; <0 = 503 until cleared
}

func (s *captureStore) push(raw json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	s.items = append([]json.RawMessage{cp}, s.items...)
	if len(s.items) > maxCaptured {
		s.items = s.items[:maxCaptured]
	}
}

func (s *captureStore) list() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]json.RawMessage, len(s.items))
	copy(out, s.items)
	return out
}

func (s *captureStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
}

func (s *captureStore) setFail(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = n
}

// shouldFail reports whether this POST returns 503 and remaining fails after it.
func (s *captureStore) shouldFail() (fail bool, remaining int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext == 0 {
		return false, 0
	}
	if s.failNext < 0 {
		return true, -1
	}
	s.failNext--
	return true, s.failNext
}

func (s *captureStore) failRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failNext
}

func main() {
	addr := flag.String("addr", ":8443", "HTTPS listen address")
	flag.Parse()

	cert, err := selfSignedCert([]string{"localhost", "bridge-api-mock", "127.0.0.1"})
	if err != nil {
		log.Fatalf("tls cert: %v", err)
	}

	store := &captureStore{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bridge/bootstrap", handleBootstrap)
	mux.HandleFunc("/v1/bridge/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/bridge/weather", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer func() { _ = r.Body.Close() }()
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if fail, rem := store.shouldFail(); fail {
			log.Printf("rejecting weather POST (fail remaining=%d)", rem)
			http.Error(w, "simulated outage", http.StatusServiceUnavailable)
			return
		}
		store.push(raw)
		log.Printf("captured weather POST (%d bytes)", len(raw))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/bridge/weather/captured", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count": len(store.list()),
			"posts": store.list(),
		})
	})
	// Local-only test hooks for outage / catch-up drills.
	mux.HandleFunc("/v1/bridge/weather/fail", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Count int `json:"count"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Count <= 0 {
				body.Count = -1 // fail until DELETE
			}
			store.setFail(body.Count)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"fail_remaining": store.failRemaining()})
		case http.MethodDelete:
			store.setFail(0)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/bridge/weather/captured/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		store.clear()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:      *addr,
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
	log.Printf("bridge-api-mock HTTPS on %s", *addr)
	log.Printf("  GET  /v1/bridge/bootstrap")
	log.Printf("  POST /v1/bridge/weather  (captured)")
	log.Printf("  GET  /v1/bridge/weather/captured")
	log.Printf("  POST /v1/bridge/weather/fail  (test outage)")
	log.Printf("  DELETE /v1/bridge/weather/fail  (clear outage)")
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		log.Printf("listen: %v", err)
		os.Exit(1)
	}
}

func handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"bridge_id":                  "br_local_mock",
			"airport":                    map[string]interface{}{"id": "KXX", "name": "Mock Field"},
			"declination_deg":            -15.2,
			"declination_source":         "mock",
			"heartbeat_interval_seconds": 60,
			"enabled_sources":            []interface{}{},
		},
	})
}

func selfSignedCert(hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "aviationwx-bridge-api-mock"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
