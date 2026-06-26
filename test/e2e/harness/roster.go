//go:build e2e

package harness

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// UploadHTTPSAddr returns host:port for roster HTTPS from the host (published compose port).
func UploadHTTPSAddr() string {
	if v := os.Getenv("E2E_UPLOAD_HTTPS_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:18443"
}

// UploadSFTPAddr returns host:port for SFTP from the host.
func UploadSFTPAddr() string {
	if v := os.Getenv("E2E_UPLOAD_SFTP_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:12223"
}

// RosterHTTPClient returns an HTTP client that fetches the upload roster over harness TLS.
func RosterHTTPClient() (*http.Client, error) {
	caPEM, err := os.ReadFile(UploadRosterCAFile())
	if err != nil {
		return nil, fmt.Errorf("read roster CA: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse roster CA PEM")
	}

	host, port, err := net.SplitHostPort(UploadHTTPSAddr())
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: UploadHost,
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}, nil
}
