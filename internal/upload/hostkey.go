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
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/update"
	"golang.org/x/crypto/ssh"
)

const trustedHostKeyFetchCooldown = time.Hour

// hostKeyStore records SSH host keys on first connect (TOFU) and rejects unexpected changes.
// Keys listed in upload_ssh_trusted_keys.json (from GitHub release metadata) rotate automatically.
type hostKeyStore struct {
	path      string
	configDir string
	mu        sync.Mutex

	lastTrustedFetch time.Time
}

func newHostKeyStore(path, configDir string) (*hostKeyStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("known hosts path is required")
	}
	if strings.TrimSpace(configDir) == "" {
		configDir = filepath.Dir(path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create known hosts directory: %w", err)
	}
	return &hostKeyStore{path: path, configDir: configDir}, nil
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

	fp := ssh.FingerprintSHA256(key)
	if s.isTrustedFingerprint(fp) {
		return s.replace(label, key)
	}
	if s.refreshTrustedFingerprints() && s.isTrustedFingerprint(fp) {
		return s.replace(label, key)
	}

	return fmt.Errorf("ssh host key mismatch for %s (got %s, want %s)",
		label, fp, ssh.FingerprintSHA256(stored))
}

func (s *hostKeyStore) isTrustedFingerprint(fp string) bool {
	trusted, err := update.LoadTrustedUploadHostKeys(s.configDir)
	if err != nil || len(trusted) == 0 {
		return false
	}
	fp = strings.TrimSpace(fp)
	for _, t := range trusted {
		if strings.EqualFold(strings.TrimSpace(t), fp) {
			return true
		}
	}
	return false
}

func (s *hostKeyStore) refreshTrustedFingerprints() bool {
	if time.Since(s.lastTrustedFetch) < trustedHostKeyFetchCooldown {
		return false
	}
	s.lastTrustedFetch = time.Now()
	if err := update.SyncTrustedUploadHostKeys(s.configDir); err != nil {
		return false
	}
	return true
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
	line := formatKnownHostsLine(label, key)
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

func (s *hostKeyStore) replace(label string, key ssh.PublicKey) error {
	lines, err := s.readLines()
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 1 && hostFieldMatches(fields[0], label) {
			continue
		}
		out = append(out, line)
	}
	out = append(out, strings.TrimRight(formatKnownHostsLine(label, key), "\n"))
	return s.writeLines(out)
}

func (s *hostKeyStore) readLines() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read known hosts: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n"), nil
}

func (s *hostKeyStore) writeLines(lines []string) error {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	b.WriteByte('\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0600); err != nil {
		return fmt.Errorf("write known hosts: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("install known hosts: %w", err)
	}
	return nil
}

func formatKnownHostsLine(label string, key ssh.PublicKey) string {
	return fmt.Sprintf("%s %s\n", label, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))))
}

func keysEqual(a, b ssh.PublicKey) bool {
	return bytes.Equal(a.Marshal(), b.Marshal())
}
