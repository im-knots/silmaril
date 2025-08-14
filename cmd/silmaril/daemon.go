package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/silmaril/silmaril/internal/api"
	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/daemon"
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
	Long: `Start the Silmaril daemon.

The daemon will:
- Maintain persistent DHT connections
- Continue seeding downloaded models
- Handle download/upload operations
- Provide an HTTP API on port 8737 (configurable)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		
		if port == 0 {
			port = viper.GetInt("daemon.port")
			if port == 0 {
				port = 8737 // Default port
			}
		}

		// Check if something is already running on this port
		apiClient := client.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err := apiClient.Health(); err == nil {
			fmt.Printf("Daemon is already running on port %d\n", port)
			return nil
		}

		// Create daemon
		cfg := config.Get()
		d, err := daemon.New(cfg)
		if err != nil {
			return fmt.Errorf("failed to create daemon: %w", err)
		}

		// Setup API routes and set them on the daemon
		routes := api.SetupRoutes(d)
		d.SetAPIHandler(routes)
		
		// Start the daemon (this starts all components and the API server)
		if err := d.Start(port); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}
		
		// Wait for interrupt signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		
		fmt.Println("\nShutting down daemon...")
		return d.Shutdown()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Silmaril daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		if port == 0 {
			port = viper.GetInt("daemon.port")
			if port == 0 {
				port = 8737 // Default port
			}
		}

		// Check if daemon is running by trying the API
		apiClient := client.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err := apiClient.Health(); err != nil {
			fmt.Println("Daemon is not running")
			return nil
		}

		// Shutdown via API
		if err := apiClient.Shutdown(); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}

		fmt.Println("Daemon shutdown initiated")
		
		// Wait briefly to confirm shutdown
		time.Sleep(2 * time.Second)
		if err := apiClient.Health(); err != nil {
			// Daemon is no longer responding, it has stopped
			fmt.Println("Daemon stopped successfully")
			return nil
		}

		return fmt.Errorf("daemon did not stop within timeout")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		if port == 0 {
			port = viper.GetInt("daemon.port")
			if port == 0 {
				port = 8737 // Default port
			}
		}

		// Check daemon via API
		apiClient := client.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err := apiClient.Health(); err != nil {
			fmt.Printf("Daemon is not running on port %d\n", port)
			return nil
		}

		// Get status via API
		status, err := apiClient.GetStatus()
		if err != nil {
			fmt.Printf("Daemon is running on port %d but unable to get status: %v\n", port, err)
			return nil
		}

		fmt.Printf("Daemon Status (port %d):\n", port)
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
		port, _ := cmd.Flags().GetInt("port")
		if port == 0 {
			port = viper.GetInt("daemon.port")
			if port == 0 {
				port = 8737 // Default port
			}
		}

		// Check if daemon is running
		apiClient := client.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err := apiClient.Health(); err == nil {
			// Daemon is running, stop it first
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
	daemonStartCmd.Flags().Int("port", 0, "API port (default: 8737)")
	
	// Flags for other commands
	daemonStopCmd.Flags().Int("port", 0, "API port (default: 8737)")
	daemonStatusCmd.Flags().Int("port", 0, "API port (default: 8737)")
	daemonRestartCmd.Flags().Int("port", 0, "API port (default: 8737)")
}

// Helper function to get daemon URL with the specified or default port
func getDaemonURL() string {
	port := viper.GetInt("daemon.port")
	if port == 0 {
		port = 8737
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}