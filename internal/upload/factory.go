package upload

import (
	"fmt"
	"strings"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

// NewClientFromConfig creates an upload client from the config package's Upload type.
// Converts the config.Upload to upload.Config and creates the appropriate client based on protocol.
// Supports "sftp" (default, recommended) and "ftps" (legacy) protocols.
func NewClientFromConfig(cfg config.Upload) (Client, error) {
	// Normalize protocol (default to SFTP, case-insensitive)
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = "sftp" // Default to SFTP
	}

	// Default DisableEPSV to true (use standard PASV) if not specified (FTPS only)
	disableEPSV := true
	if cfg.DisableEPSV != nil {
		disableEPSV = *cfg.DisableEPSV
	}

	// Set default ports based on protocol
	port := cfg.Port
	if port == 0 {
		switch protocol {
		case "sftp":
			port = 22
		case "ftps":
			port = 2121
		default:
			port = 22 // Default to SFTP port
		}
	}

	uploadConfig := Config{
		Host:                  cfg.Host,
		Port:                  port,
		Username:              cfg.Username,
		Password:              cfg.Password,
		TLS:                   cfg.TLS,
		TLSVerify:             cfg.TLSVerify,
		CABundlePath:          cfg.CABundlePath,
		TimeoutConnectSeconds: cfg.TimeoutConnectSeconds,
		TimeoutUploadSeconds:  cfg.TimeoutUploadSeconds,
		DisableEPSV:           disableEPSV,
	}

	// Create appropriate client based on protocol
	switch protocol {
	case "sftp":
		return NewSFTPClient(uploadConfig)
	case "ftps", "ftp":
		return NewFTPSClient(uploadConfig)
	default:
		return nil, fmt.Errorf("unsupported upload protocol: %s (supported: sftp, ftps)", protocol)
	}
}
