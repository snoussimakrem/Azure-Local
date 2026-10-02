package kernel

import (
	"os"
	"path/filepath"
)

type Config struct {
	BindAddress string
	Port        int
	DataDir     string
	LogLevel    string
	NetworkMode string // "offline" | "online"
}

func DefaultConfig() Config {
	return Config{
		BindAddress: "127.0.0.1",
		Port:        4577,
		DataDir:     defaultDataDir(),
		LogLevel:    "info",
		NetworkMode: "offline",
	}
}

func defaultDataDir() string {
	if v := os.Getenv("AZLOCAL_DATA_DIR"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".azlocal", "data")
	}
	return "/data"
}
