package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azure-local/azure-local/internal/kernel"
	"github.com/azure-local/azure-local/internal/server"
)

const (
	defaultPort = 4577
	daemonEnv   = "AZLOCAL_DAEMON_CHILD"

	devAccount    = "devstoreaccount1"
	devAccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "start":
		cmdStart(os.Args[2:])
	case "stop":
		cmdStop()
	case "status":
		cmdStatus()
	case "env":
		cmdEnv()
	case "reset":
		cmdReset()
	case "version", "--version", "-v":
		fmt.Println("azlocal 0.1.0-milestone1")
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `azlocal — Azure Local emulator

Usage:
  azlocal start [--daemon] [--port N] [--data DIR] [--log-level LEVEL]
  azlocal stop
  azlocal status
  azlocal env
  azlocal reset
  azlocal version
`)
}

// ---------- start ----------------------------------------------------------

func cmdStart(args []string) {
	daemon := false
	port := defaultPort
	dataDir := ""
	logLevel := "info"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--daemon", "-d":
			daemon = true
		case "--port", "-p":
			if i+1 >= len(args) {
				fatal("--port requires a value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fatal("invalid --port: " + err.Error())
			}
			port = n
		case "--data":
			if i+1 >= len(args) {
				fatal("--data requires a value")
			}
			i++
			dataDir = args[i]
		case "--log-level":
			if i+1 >= len(args) {
				fatal("--log-level requires a value")
			}
			i++
			logLevel = args[i]
		default:
			fatal("unknown flag: " + args[i])
		}
	}

	if daemon && os.Getenv(daemonEnv) != "1" {
		spawnDaemon(port, dataDir, logLevel)
		return
	}

	cfg := kernel.DefaultConfig()
	cfg.Port = port
	cfg.LogLevel = logLevel
	if dataDir != "" {
		cfg.DataDir = dataDir
	}

	logger := kernel.NewLogger(cfg.LogLevel)

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to initialise server", "err", err)
		os.Exit(1)
	}

	pidFile := pidFilePath()
	_ = os.MkdirAll(filepath.Dir(pidFile), 0o755)
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidFile)

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

func spawnDaemon(port int, dataDir, logLevel string) {
	exe, err := os.Executable()
	if err != nil {
		fatal("cannot locate executable: " + err.Error())
	}
	args := []string{"start",
		"--port", strconv.Itoa(port),
		"--log-level", logLevel,
	}
	if dataDir != "" {
		args = append(args, "--data", dataDir)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonEnv+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fatal("failed to spawn daemon: " + err.Error())
	}
	fmt.Printf("Azure Local started (pid %d)\n", cmd.Process.Pid)
}

// ---------- stop -----------------------------------------------------------

func cmdStop() {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Azure Local is not running (no PID file)")
		os.Exit(1)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fatal("invalid PID file")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fatal("process not found: " + err.Error())
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fatal("failed to signal: " + err.Error())
	}
	fmt.Printf("Azure Local stopped (pid %d)\n", pid)
}

// ---------- status ---------------------------------------------------------

func cmdStatus() {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", defaultPort)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("Azure Local")
		fmt.Println()
		fmt.Println("  status   STOPPED")
		fmt.Printf("  endpoint http://localhost:%d\n", defaultPort)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var h struct {
		Status   string `json:"status"`
		Endpoint string `json:"endpoint"`
		Uptime   string `json:"uptime"`
		Services []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Status  string `json:"status"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		fatal("invalid health response: " + err.Error())
	}

	fmt.Println("Azure Local")
	fmt.Println()
	fmt.Printf("  status   %s\n", strings.ToUpper(h.Status))
	fmt.Printf("  uptime   %s\n", h.Uptime)
	fmt.Println()
	fmt.Println("  services")
	for _, s := range h.Services {
		fmt.Printf("    %-12s %-10s (%s)\n",
			s.Name, strings.ToUpper(s.Status), s.Version)
	}
	fmt.Println()
	fmt.Printf("  endpoint http://localhost:%d\n", defaultPort)
}

// ---------- env ------------------------------------------------------------

func cmdEnv() {
	connStr := fmt.Sprintf(
		"DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=%s;"+
			"BlobEndpoint=http://localhost:%d/%s;"+
			"QueueEndpoint=http://localhost:%d/%s-queue;"+
			"TableEndpoint=http://localhost:%d/%s-table;",
		devAccount, devAccountKey,
		defaultPort, devAccount,
		defaultPort, devAccount,
		defaultPort, devAccount,
	)
	fmt.Printf("export AZURE_TENANT_ID=local\n")
	fmt.Printf("export AZURE_SUBSCRIPTION_ID=local-sub\n")
	fmt.Printf("export AZURE_CLIENT_ID=local-client\n")
	fmt.Printf("export AZURE_CLIENT_SECRET=local-secret\n")
	fmt.Printf("export AZURE_STORAGE_ACCOUNT=%s\n", devAccount)
	fmt.Printf("export AZURE_STORAGE_KEY='%s'\n", devAccountKey)
	fmt.Printf("export AZURE_STORAGE_CONNECTION_STRING='%s'\n", connStr)
	fmt.Printf("export AZURE_AUTHORITY_HOST=http://localhost:%d\n", defaultPort)
	fmt.Printf("export ARM_TENANT_ID=local\n")
	fmt.Printf("export ARM_SUBSCRIPTION_ID=local-sub\n")
	fmt.Printf("export ARM_CLIENT_ID=local-client\n")
	fmt.Printf("export ARM_CLIENT_SECRET=local-secret\n")
	fmt.Printf("export ARM_ENDPOINT=http://localhost:%d\n", defaultPort)
	fmt.Printf("export AZURERM_METADATA_HOST=localhost:%d\n", defaultPort)
}

// ---------- reset ----------------------------------------------------------

func cmdReset() {
	cfg := kernel.DefaultConfig()
	fmt.Printf("This will permanently delete all local Azure state under:\n  %s\n\n", cfg.DataDir)
	fmt.Print("Continue? [y/N] ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Aborted.")
		return
	}
	if err := os.RemoveAll(cfg.DataDir); err != nil {
		fatal("reset failed: " + err.Error())
	}
	fmt.Println("Local Azure state deleted.")
}

// ---------- helpers --------------------------------------------------------

func pidFilePath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".azlocal", "azlocal.pid")
	}
	return "/tmp/azlocal.pid"
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "error: "+msg)
	os.Exit(1)
}

// silence unused import
var _ = io.Discard
