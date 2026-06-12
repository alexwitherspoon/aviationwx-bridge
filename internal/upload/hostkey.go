package upload

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// hostKeyStore records SSH host keys on first connect (TOFU) and rejects changes.
type hostKeyStore struct {
	path string
	mu   sync.Mutex
}

func newHostKeyStore(path string) (*hostKeyStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("known hosts path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create known hosts directory: %w", err)
	}
	return &hostKeyStore{path: path}, nil
}

func (s *hostKeyStore) callback(expectedHost string, expectedPort int) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		_ = hostname
		label := knownHostLabel(expectedHost, expectedPort)
		return s.verify(label, key)
	}
}

func knownHostLabel(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port <= 0 || port == 22 {
		return host
	}
	return fmt.Sprintf("[%s]:%d", host, port)
}

func (s *hostKeyStore) verify(label string, key ssh.PublicKey) error {
	if label == "" {
		return fmt.Errorf("ssh host key verification: empty host label")
	}
	if key == nil {
		return fmt.Errorf("ssh host key verification: missing server key")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, found, err := s.lookup(label)
	if err != nil {
		return err
	}
	if !found {
		return s.append(label, key)
	}
	if keysEqual(stored, key) {
		return nil
	}
	return fmt.Errorf("ssh host key mismatch for %s (got %s, want %s)",
		label, ssh.FingerprintSHA256(key), ssh.FingerprintSHA256(stored))
}

func (s *hostKeyStore) lookup(label string) (ssh.PublicKey, bool, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read known hosts: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !hostFieldMatches(fields[0], label) {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fields[1] + " " + fields[2]))
		if err != nil {
			return nil, false, fmt.Errorf("parse known hosts entry for %s: %w", label, err)
		}
		return pub, true, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("read known hosts: %w", err)
	}
	return nil, false, nil
}

func hostFieldMatches(field, label string) bool {
	for _, part := range strings.Split(field, ",") {
		if strings.TrimSpace(part) == label {
			return true
		}
	}
	return false
}

func (s *hostKeyStore) append(label string, key ssh.PublicKey) error {
	line := fmt.Sprintf("%s %s\n", label, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))))
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("write known hosts: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write known hosts: %w", err)
	}
	return nil
}

func keysEqual(a, b ssh.PublicKey) bool {
	return bytes.Equal(a.Marshal(), b.Marshal())
}
