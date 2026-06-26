//go:build e2e

package harness

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// HTTPPost sends POST with optional body and returns response body on 2xx.
func HTTPPost(url string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s: status %d body=%s", url, resp.StatusCode, out)
	}
	return out, nil
}
