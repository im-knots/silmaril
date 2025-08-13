package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/schollz/progressbar/v3"
)

var getCmd = &cobra.Command{
	Use:   "get [model-name]",
	Short: "Download a model from the P2P network",
	Long: `Downloads a model from the Silmaril P2P network.
Shows progress with speed and ETA, verifies checksums after download.`,
	Args: cobra.ExactArgs(1),
	RunE: runGet,
}

var (
	outputDir   string
	keepSeeding bool
	noVerify    bool
)

func init() {
	rootCmd.AddCommand(getCmd)
	
	getCmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default: ~/.silmaril/models/)")
	getCmd.Flags().BoolVar(&keepSeeding, "seed", true, "continue seeding after download")
	getCmd.Flags().BoolVar(&noVerify, "no-verify", false, "skip checksum verification")
	
	viper.BindPFlag("output", getCmd.Flags().Lookup("output"))
	viper.BindPFlag("seed", getCmd.Flags().Lookup("seed"))
	viper.BindPFlag("no-verify", getCmd.Flags().Lookup("no-verify"))
}

func runGet(cmd *cobra.Command, args []string) error {
	modelName := args[0]
	
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Create API client
	apiClient := client.NewClient(getDaemonURL())
	
	// Check if model exists
	model, err := apiClient.GetModel(modelName)
	if err != nil {
		// Model not found locally, try to discover it
		fmt.Printf("Model not found locally, searching on P2P network...\n")
		
		models, err := apiClient.DiscoverModels(modelName)
		if err != nil {
			return fmt.Errorf("failed to discover model: %w", err)
		}
		
		if len(models) == 0 {
			return fmt.Errorf("model '%s' not found on the network", modelName)
		}
		
		// Use the first matching model
		model = models[0]
	} else {
		fmt.Printf("Model already exists locally. Use 'silmaril share %s' to seed it.\n", modelName)
		return nil
	}
	
	// Display model information
	fmt.Printf("\nModel: %s\n", modelName)
	if version, ok := model["version"].(string); ok && version != "" {
		fmt.Printf("Version: %s\n", version)
	}
	if license, ok := model["license"].(string); ok && license != "" {
		fmt.Printf("License: %s\n", license)
	}
	
	var totalSize float64
	if size, ok := model["size"].(float64); ok {
		totalSize = size
	} else if size, ok := model["total_size"].(float64); ok {
		totalSize = size
	}
	
	if totalSize > 0 {
		fmt.Printf("Size: %.2f GB\n", totalSize/(1024*1024*1024))
	}
	
	fmt.Println("\nStarting download...")
	
	// Start the download via API
	infoHash := ""
	if ih, ok := model["info_hash"].(string); ok {
		infoHash = ih
	}
	
	result, err := apiClient.DownloadModel(modelName, infoHash, keepSeeding)
	if err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}
	
	transferID := ""
	if tid, ok := result["transfer_id"].(string); ok {
		transferID = tid
	}
	
	if transferID == "" {
		return fmt.Errorf("no transfer ID returned from daemon")
	}
	
	fmt.Printf("Download started (Transfer ID: %s)\n", transferID)
	
	// Create progress bar
	bar := progressbar.NewOptions64(
		int64(totalSize),
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionSetRenderBlankState(true),
	)
	
	// Monitor progress
	for {
		transfer, err := apiClient.GetTransfer(transferID)
		if err != nil {
			return fmt.Errorf("failed to get transfer status: %w", err)
		}
		
		status := ""
		if s, ok := transfer["status"].(string); ok {
			status = s
		}
		
		if status == "completed" {
			bar.Finish()
			fmt.Println("\n✅ Download complete!")
			
			if keepSeeding {
				fmt.Println("Model is now seeding. Use 'silmaril share' to manage seeding.")
			}
			return nil
		}
		
		if status == "failed" {
			errorMsg := "unknown error"
			if e, ok := transfer["error"].(string); ok {
				errorMsg = e
			}
			return fmt.Errorf("download failed: %s", errorMsg)
		}
		
		if status == "cancelled" {
			return fmt.Errorf("download was cancelled")
		}
		
		// Update progress
		if bytesTransferred, ok := transfer["bytes_transferred"].(float64); ok {
			bar.Set64(int64(bytesTransferred))
		}
		
		// Show peer info
		if peers, ok := transfer["peers"].(float64); ok {
			if seeders, ok := transfer["seeders"].(float64); ok {
				bar.Describe(fmt.Sprintf("Downloading [%d peers, %d seeders]", int(peers), int(seeders)))
			}
		}
		
		// Wait before next poll
		time.Sleep(1 * time.Second)
	}
}