package transport

import (
	"os"
	"path/filepath"
	"strings"
)

func ParseEndpoint(endpoint string) (string, string) {
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(strings.ToLower(endpoint), "tcp://") {
		return "tcp", strings.TrimPrefix(endpoint, "tcp://")
	}
	return "unix", endpoint
}

func PrepareEndpoint(network string, address string) error {
	if network != "unix" || address == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(address), 0o777); err != nil {
		return err
	}
	_ = os.Remove(address)
	return nil
}

func CleanupEndpoint(network string, address string) {
	if network == "unix" && address != "" {
		_ = os.Remove(address)
	}
}
