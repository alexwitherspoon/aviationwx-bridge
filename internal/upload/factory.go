package upload

import (
	"fmt"
	"strings"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

// NewClientFromConfig creates an SFTP upload client from the config package's Upload type.
// Protocol "ftps" and "ftp" are migrated to SFTP (port 2222) for backward compatibility.
// knownHostsPath is the SSH known_hosts file used for TOFU host key verification.
func NewClientFromConfig(cfg config.Upload, knownHostsPath string) (Client, error) {
	// Normalize protocol: migrate deprecated FTPS/FTP to SFTP
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" || protocol == "ftps" || protocol == "ftp" {
		protocol = "sftp"
	}
	if protocol != "sftp" {
		return nil, fmt.Errorf("unsupported upload protocol: %s (SFTP only)", protocol)
	}

	port := cfg.Port
	if port == 0 {
		port = 2222 // aviationwx.org SFTP port
	}

	basePath := cfg.BasePath
	if basePath == "" {
		basePath = "/files" // Default SFTP upload directory
	}

	uploadConfig := Config{
		Host:                  cfg.Host,
		Port:                  port,
		Username:              cfg.Username,
		Password:              cfg.Password,
		TimeoutConnectSeconds: cfg.TimeoutConnectSeconds,
		TimeoutUploadSeconds:  cfg.TimeoutUploadSeconds,
		BasePath:              basePath,
		KnownHostsPath:        knownHostsPath,
	}

	return NewSFTPClient(uploadConfig)
}
