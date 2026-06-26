package update

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncUploadSSHHostKeysHTTPS_trustsRosterCAFile(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	serverCert, serverKey := generateServerCert(t, caKey, caCert, "upload.test")

	body := `{"version":1,"hostname":"upload.test","port":2222,"sha256":["SHA256:fromhttps"],"updated_at":"2026-06-26T00:00:00Z"}`
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}},
	}
	server.StartTLS()
	defer server.Close()

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	writePEM(t, caPath, "CERTIFICATE", caCert.Raw)

	t.Setenv(uploadRosterCAFileEnv, caPath)
	resetUploadRosterCAForTest()
	t.Cleanup(func() {
		t.Setenv(uploadRosterCAFileEnv, "")
		resetUploadRosterCAForTest()
	})

	prod := NewUploadHostKeysTLSHTTPClient("upload.test", requestTimeout)
	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Timeout:       prod.Timeout,
		CheckRedirect: prod.CheckRedirect,
		Transport:     &rewriteHostTransport{base: server.URL, inner: prod.Transport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	dir := t.TempDir()
	if err := SyncUploadSSHHostKeysHTTPS(dir, "upload.test", 2222); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func TestSyncUploadSSHHostKeysHTTPS_rejectsUntrustedRosterCA(t *testing.T) {
	caKey, caCert := generateTestCA(t)
	serverCert, serverKey := generateServerCert(t, caKey, caCert, "upload.test")

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":1,"hostname":"upload.test","port":2222,"sha256":["SHA256:x"],"updated_at":"t"}`))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}},
	}
	server.StartTLS()
	defer server.Close()

	otherCAPath := filepath.Join(t.TempDir(), "other-ca.pem")
	_, otherCert := generateTestCA(t)
	writePEM(t, otherCAPath, "CERTIFICATE", otherCert.Raw)

	t.Setenv(uploadRosterCAFileEnv, otherCAPath)
	resetUploadRosterCAForTest()
	t.Cleanup(func() {
		t.Setenv(uploadRosterCAFileEnv, "")
		resetUploadRosterCAForTest()
	})

	prod := NewUploadHostKeysTLSHTTPClient("upload.test", requestTimeout)
	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Timeout:       prod.Timeout,
		CheckRedirect: prod.CheckRedirect,
		Transport:     &rewriteHostTransport{base: server.URL, inner: prod.Transport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	if err := SyncUploadSSHHostKeysHTTPS(t.TempDir(), "upload.test", 2222); err == nil {
		t.Fatal("expected TLS error with wrong CA")
	}
}

func TestSyncUploadSSHHostKeysHTTPS_invalidCAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(uploadRosterCAFileEnv, path)
	resetUploadRosterCAForTest()
	t.Cleanup(func() {
		t.Setenv(uploadRosterCAFileEnv, "")
		resetUploadRosterCAForTest()
	})

	if err := SyncUploadSSHHostKeysHTTPS(t.TempDir(), "upload.test", 2222); err == nil {
		t.Fatal("expected invalid CA error")
	}
}

func generateTestCA(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, cert
}

func generateServerCert(t *testing.T, caKey *rsa.PrivateKey, caCert *x509.Certificate, host string) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{host},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
		t.Fatal(err)
	}
}
