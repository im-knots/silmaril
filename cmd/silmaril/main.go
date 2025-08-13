package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/silmaril/silmaril/internal/config"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "silmaril",
		Short: "P2P distribution system for AI models",
		Long: `Silmaril is a peer-to-peer distribution system for AI models using BitTorrent.
Download and share large language models efficiently across the network.

Key Commands:
  list      - Show models downloaded to your local machine
  discover  - Search for models available on the P2P network
  get       - Download a model from the network
  publish   - Create and share a new model on the network
  share     - Seed models to help others download`,
	}
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/silmaril/config.yaml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose output")
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
	// Initialize our config system
	if err := config.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		os.Exit(1)
	}
	
	// Create all necessary directories
	if err := config.CreateAllDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directories: %v\n", err)
		os.Exit(1)
	}
	
	// If user specified a config file, load it
	if cfgFile != "" {
		v := config.GetViper()
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
			os.Exit(1)
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// ensureDaemonRunning checks if the daemon is running and starts it if not
func ensureDaemonRunning() error {
	// Skip daemon check for daemon commands themselves
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		return nil
	}

	// Skip daemon check for init command
	if len(os.Args) > 1 && os.Args[1] == "init" {
		return nil
	}

	// Check if daemon is running
	apiClient := client.NewClient(getDaemonURL())
	if err := apiClient.Health(); err == nil {
		return nil // Already running
	}

	// Auto-start is disabled if explicitly set to false
	if viper.IsSet("daemon.auto_start") && !viper.GetBool("daemon.auto_start") {
		return fmt.Errorf("daemon is not running. Start it with: silmaril daemon start")
	}

	// Start daemon in background
	fmt.Println("Starting daemon...")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := exec.Command(exe, "daemon", "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Detach from the process
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to detach daemon process: %w", err)
	}

	// Wait for daemon to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if err := apiClient.Health(); err == nil {
			fmt.Println("Daemon started successfully")
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start within timeout")
}