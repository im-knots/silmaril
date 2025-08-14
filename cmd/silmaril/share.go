package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/spf13/cobra"
)

var shareCmd = &cobra.Command{
	Use:   "share [model-name, path, or directory]",
	Short: "Share/publish models to the P2P network",
	Long: `Share models by seeding them to the P2P network, or publish a new model
by creating a torrent and manifest from a directory.

You can specify:
  - A model name from registry (e.g., "meta-llama/Llama-3.1-8B")
  - A path to a torrent file to seed
  - A path to a model directory to publish and seed (creates torrent if needed)
  - Use --all to seed all downloaded models

Examples:
  silmaril share --all                          # Seed all models
  silmaril share meta-llama/Llama-3.1-8B        # Seed specific model
  silmaril share /path/to/model.torrent         # Seed from torrent file
  silmaril share /path/to/model/dir --name org/model --license apache-2.0  # Publish new model`,
	RunE: runShare,
}

var (
	shareAll     bool
	modelName    string
	modelVersion string
	modelLicense string
	pieceLength  int64
	skipDHT      bool
	signManifest bool
	noMonitor    bool
)

func init() {
	rootCmd.AddCommand(shareCmd)

	shareCmd.Flags().BoolVar(&shareAll, "all", false, "seed all downloaded models")

	// Publish flags (only needed when creating torrent from directory)
	shareCmd.Flags().StringVar(&modelName, "name", "", "model name for publishing (e.g., org/model-name)")
	shareCmd.Flags().StringVar(&modelVersion, "version", "main", "model version/revision")
	shareCmd.Flags().StringVar(&modelLicense, "license", "", "model license")
	shareCmd.Flags().Int64Var(&pieceLength, "piece-length", 4*1024*1024, "piece length for torrent (default 4MB)")
	shareCmd.Flags().BoolVar(&skipDHT, "skip-dht", false, "skip DHT announcement")
	shareCmd.Flags().BoolVar(&signManifest, "sign", true, "sign the manifest")
	shareCmd.Flags().BoolVar(&noMonitor, "no-monitor", true, "don't monitor seeding progress after sharing")
}

func runShare(cmd *cobra.Command, args []string) error {
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Create API client
	apiClient := client.NewClient(getDaemonURL())

	var modelNameToShare string
	var pathToShare string

	if shareAll {
		// Share all models
		fmt.Println("Seeding all downloaded models...")

		opts := client.ShareModelOptions{
			All: true,
		}
		result, err := apiClient.ShareModel(opts)
		if err != nil {
			return fmt.Errorf("failed to share models: %w", err)
		}

		modelsShared := 0
		totalModels := 0
		if ms, ok := result["models_shared"].(float64); ok {
			modelsShared = int(ms)
		}
		if tm, ok := result["total_models"].(float64); ok {
			totalModels = int(tm)
		}

		if totalModels == 0 {
			fmt.Println("No models found in registry.")
			fmt.Println("Use 'silmaril get' or 'silmaril mirror' to download models first.")
			return nil
		}

		fmt.Printf("✅ Started sharing %d out of %d models\n", modelsShared, totalModels)

	} else if len(args) > 0 {
		input := args[0]

		// First check if it's a file or directory that exists
		info, err := os.Stat(input)
		if err == nil {
			// Path exists - check what it is
			if info.IsDir() {
				// Directory - we can publish via the API
				// Convert to absolute path for the daemon
				absPath, err := filepath.Abs(input)
				if err != nil {
					return fmt.Errorf("failed to get absolute path: %w", err)
				}
				pathToShare = absPath
				fmt.Printf("Publishing model from directory: %s\n", input)
			} else if filepath.Ext(input) == ".torrent" {
				// Direct torrent file - not supported via directory path
				return fmt.Errorf("sharing torrent files directly is not yet implemented")
			} else {
				return fmt.Errorf("invalid input: must be a model name or directory")
			}
		} else if strings.Contains(input, "/") {
			// Path doesn't exist but contains "/" - treat as model name from registry
			modelNameToShare = input
			fmt.Printf("Seeding model: %s\n", input)
		} else {
			// Not a valid path and not a model name
			return fmt.Errorf("'%s' is not a valid model name or path: %w", input, err)
		}

		// Build share options
		opts := client.ShareModelOptions{
			ModelName:    modelNameToShare,
			Path:         pathToShare,
			All:          false,
			Name:         modelName,    // From --name flag
			License:      modelLicense, // From --license flag
			Version:      modelVersion, // From --version flag
			PieceLength:  pieceLength,  // From --piece-length flag
			SkipDHT:      skipDHT,      // From --skip-dht flag
			SignManifest: signManifest, // From --sign flag
		}
		

		// Share the specific model or path
		result, err := apiClient.ShareModel(opts)
		if err != nil {
			return fmt.Errorf("failed to share: %w", err)
		}

		// Check if the result contains an error
		if errMsg, ok := result["error"].(string); ok {
			return fmt.Errorf("API error: %s", errMsg)
		}

		if msg, ok := result["message"].(string); ok {
			fmt.Printf("✅ %s\n", msg)
		}

		if transferID, ok := result["transfer_id"].(string); ok {
			fmt.Printf("Transfer ID: %s\n", transferID)
		}

	} else {
		// No arguments and not --all
		fmt.Println("Please specify a model name or use --all to seed all models")
		fmt.Println("\nExamples:")
		fmt.Println("  silmaril share --all")
		fmt.Println("  silmaril share meta-llama/Llama-3.1-8B")
		fmt.Println("  silmaril share /path/to/model.torrent")
		return nil
	}

	// Exit early if monitoring disabled
	if noMonitor {
		fmt.Println("\nModel is being shared by the daemon in the background.")
		fmt.Println("Use 'silmaril daemon status' to check the daemon status.")
		return nil
	}

	// Monitor seeding if requested
	fmt.Println("\nSeeding models... Press Ctrl+C to stop")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Monitor seeding
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get transfer stats from daemon
			transfers, err := apiClient.ListTransfers("active")
			if err != nil {
				fmt.Printf("\rError getting transfer stats: %v", err)
				continue
			}

			seedCount := 0
			totalPeers := 0
			var totalUploaded float64

			for _, transfer := range transfers {
				if transferType, ok := transfer["type"].(string); ok && transferType == "seed" {
					seedCount++
				}
				if peers, ok := transfer["peers"].(float64); ok {
					totalPeers += int(peers)
				}
				if uploaded, ok := transfer["bytes_transferred"].(float64); ok {
					totalUploaded += uploaded
				}
			}

			fmt.Printf("\rActive seeds: %d | Peers: %d | Total uploaded: %.2f GB",
				seedCount, totalPeers, totalUploaded/(1024*1024*1024))

		case <-sigChan:
			fmt.Println("\n\nShutting down...")
			// The daemon will continue seeding even after we exit
			fmt.Println("Note: The daemon will continue seeding in the background.")
			fmt.Println("Use 'silmaril daemon stop' to stop the daemon and all transfers.")
			return nil
		}
	}
}
