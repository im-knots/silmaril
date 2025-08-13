package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/silmaril/silmaril/internal/api"
	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/daemon"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the Silmaril daemon",
	Long: `Control the Silmaril background daemon that handles P2P operations.

The daemon maintains persistent connections to the BitTorrent network,
manages DHT operations, and provides an API for CLI commands.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Silmaril daemon",
	Long: `Start the Silmaril daemon in the background.

The daemon will:
- Maintain persistent DHT connections
- Continue seeding downloaded models
- Handle download/upload operations
- Provide an HTTP API on port 8737 (configurable)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if daemon is already running
		if isDaemonRunning() {
			fmt.Println("Daemon is already running")
			return nil
		}

		foreground, _ := cmd.Flags().GetBool("foreground")
		port, _ := cmd.Flags().GetInt("port")
		
		if port == 0 {
			port = viper.GetInt("daemon.port")
			if port == 0 {
				port = 8737 // Default port
			}
		}

		if foreground {
			// Run in foreground
			return runDaemonForeground(port)
		}

		// Start daemon in background
		return startDaemonBackground(port)
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Silmaril daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isDaemonRunning() {
			fmt.Println("Daemon is not running")
			return nil
		}

		// Try API shutdown first
		apiClient := client.NewClient(getDaemonURL())
		if err := apiClient.Shutdown(); err == nil {
			fmt.Println("Daemon shutdown initiated via API")
			time.Sleep(2 * time.Second)
			if !isDaemonRunning() {
				fmt.Println("Daemon stopped successfully")
				return nil
			}
		}

		// Fall back to PID-based shutdown
		pidFile := getPIDFile()
		pidData, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("failed to read PID file: %w", err)
		}

		pid, err := strconv.Atoi(string(pidData))
		if err != nil {
			return fmt.Errorf("invalid PID in file: %w", err)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("failed to find process: %w", err)
		}

		// Send SIGTERM for graceful shutdown
		if err := process.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}

		fmt.Println("Sent shutdown signal to daemon")
		
		// Wait for daemon to stop
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)
			if !isDaemonRunning() {
				fmt.Println("Daemon stopped successfully")
				return nil
			}
		}

		return fmt.Errorf("daemon did not stop within timeout")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isDaemonRunning() {
			fmt.Println("Daemon is not running")
			return nil
		}

		// Get status via API
		apiClient := client.NewClient(getDaemonURL())
		status, err := apiClient.GetStatus()
		if err != nil {
			fmt.Println("Daemon is running but API is not responding")
			return nil
		}

		fmt.Println("Daemon Status:")
		fmt.Printf("  PID: %v\n", status["pid"])
		fmt.Printf("  Uptime: %v\n", status["uptime"])
		fmt.Printf("  Active Transfers: %v\n", status["active_transfers"])
		fmt.Printf("  Total Peers: %v\n", status["total_peers"])
		fmt.Printf("  DHT Nodes: %v\n", status["dht_nodes"])

		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Silmaril daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running
		if isDaemonRunning() {
			fmt.Println("Stopping daemon...")
			if err := daemonStopCmd.RunE(cmd, args); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		}

		// Start daemon
		fmt.Println("Starting daemon...")
		return daemonStartCmd.RunE(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd, daemonStopCmd, daemonStatusCmd, daemonRestartCmd)
	
	// Flags for daemon start
	daemonStartCmd.Flags().Bool("foreground", false, "Run daemon in foreground")
	daemonStartCmd.Flags().Int("port", 0, "API port (default: 8737)")
	
	// Flags for daemon restart
	daemonRestartCmd.Flags().Bool("foreground", false, "Run daemon in foreground after restart")
	daemonRestartCmd.Flags().Int("port", 0, "API port (default: 8737)")
}

func isDaemonRunning() bool {
	lockFile := getLockFile()
	if _, err := os.Stat(lockFile); err != nil {
		return false
	}

	// Check if process is actually running
	pidFile := getPIDFile()
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process is alive
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func runDaemonForeground(port int) error {
	cfg := config.Get()
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	// Setup API routes
	routes := api.SetupRoutes(d)
	d.SetAPIHandler(routes)

	return d.Start(port)
}

func startDaemonBackground(port int) error {
	// Get the path to the current executable
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon as background process
	cmd := exec.Command(exe, "daemon", "start", "--foreground", "--port", strconv.Itoa(port))
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Detach from the process
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to detach daemon process: %w", err)
	}

	fmt.Printf("Starting daemon on port %d...\n", port)
	
	// Wait for daemon to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if isDaemonRunning() {
			// Check if API is responding
			apiClient := client.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port))
			if _, err := apiClient.GetStatus(); err == nil {
				fmt.Println("Daemon started successfully")
				return nil
			}
		}
	}

	return fmt.Errorf("daemon failed to start within timeout")
}

func getLockFile() string {
	baseDir := storage.GetBaseDir()
	return filepath.Join(baseDir, "daemon", "daemon.lock")
}

func getPIDFile() string {
	baseDir := storage.GetBaseDir()
	return filepath.Join(baseDir, "daemon", "daemon.pid")
}

func getDaemonURL() string {
	port := viper.GetInt("daemon.port")
	if port == 0 {
		port = 8737
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}