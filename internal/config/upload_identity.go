package config

import (
	"errors"
	"fmt"
	"strings"
)

// ErrDuplicateUploadCredentials is returned when two cameras would share the same
// SFTP identity (host, port, username). Each camera must use a distinct account.
var ErrDuplicateUploadCredentials = errors.New("duplicate SFTP upload credentials")

// NormalizeUploadPort returns a positive port, defaulting SFTP to 2222 when unset.
func NormalizeUploadPort(port int) int {
	if port <= 0 {
		return 2222
	}
	return port
}

// UploadIdentityKey returns a normalized key for comparing SFTP accounts across cameras.
// Empty host or username yields an empty key (callers may skip duplicate checks for incomplete configs).
func UploadIdentityKey(host string, port int, username string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	port = NormalizeUploadPort(port)
	username = strings.TrimSpace(username)
	if host == "" || username == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d:%s", host, port, username)
}

func uploadIdentityKey(u *Upload) string {
	if u == nil {
		return ""
	}
	return UploadIdentityKey(u.Host, u.Port, u.Username)
}

// checkDuplicateUpload returns an error if another camera (excluding excludeCameraID) uses the same SFTP identity.
func (s *Service) checkDuplicateUpload(excludeCameraID string, u *Upload) error {
	key := uploadIdentityKey(u)
	if key == "" {
		return nil
	}
	for id, other := range s.cameras {
		if id == excludeCameraID {
			continue
		}
		if other.Upload == nil {
			continue
		}
		if uploadIdentityKey(other.Upload) == key {
			return fmt.Errorf("camera %q already uses this SFTP account (same host, port, and username): %w",
				id, ErrDuplicateUploadCredentials)
		}
	}
	return nil
}
