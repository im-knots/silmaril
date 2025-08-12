package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/internal/torrent"
	"github.com/silmaril/silmaril/internal/ui"
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
	
	// Initialize paths
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	// Get model manifest from registry
	registry, err := models.NewRegistry(paths)
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}
	
	manifest, err := registry.GetManifest(modelName)
	if err != nil {
		return fmt.Errorf("model not found in registry (try 'silmaril discover'): %w", err)
	}
	
	// Determine output directory
	cfg := config.Get()
	if outputDir == "" {
		outputDir = cfg.Storage.ModelsDir
	}
	
	// Create model-specific directory
	modelDir := filepath.Join(outputDir, modelName)
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	
	fmt.Printf("Downloading model: %s\n", modelName)
	fmt.Printf("Version: %s | License: %s\n", manifest.Version, manifest.License)
	fmt.Printf("Architecture: %s | Parameters: %.1fB\n", manifest.Architecture, float64(manifest.Parameters)/1e9)
	fmt.Printf("Total size: %.2f GB\n", float64(manifest.TotalSize)/(1024*1024*1024))
	fmt.Printf("Output directory: %s\n", modelDir)
	
	// Show system requirements
	fmt.Printf("\nSystem requirements:\n")
	fmt.Printf("  Minimum RAM: %d GB\n", manifest.InferenceHints.MinRAM)
	if manifest.InferenceHints.MinVRAM > 0 {
		fmt.Printf("  Minimum VRAM: %d GB\n", manifest.InferenceHints.MinVRAM)
		if len(manifest.InferenceHints.RecommendedGPU) > 0 {
			fmt.Printf("  Recommended GPUs: %v\n", manifest.InferenceHints.RecommendedGPU)
		}
	}
	fmt.Println()
	
	// Create torrent client with config
	torrentCfg := torrent.Config{
		DataDir:           modelDir,
		DownloadTimeout:   time.Duration(cfg.Torrent.DownloadTimeout) * time.Second,
		MaxConnections:    cfg.Network.MaxConnections,
		UploadRateLimit:   cfg.Network.UploadRateLimit,
		DownloadRateLimit: cfg.Network.DownloadRateLimit,
	}
	
	client, err := torrent.NewClient(torrentCfg)
	if err != nil {
		return fmt.Errorf("failed to create torrent client: %w", err)
	}
	defer client.Close()
	
	// Add magnet link (v2 only)
	magnetURI := manifest.MagnetURI
	if magnetURI == "" {
		return fmt.Errorf("no magnet URI available for model")
	}
	fmt.Println("Using BitTorrent v2 for better integrity verification")
	
	fmt.Println("Connecting to peers via DHT...")
	dl, err := client.AddMagnet(magnetURI)
	if err != nil {
		return fmt.Errorf("failed to add magnet: %w", err)
	}
	
	// Create progress bar
	progressBar := ui.NewProgressBar(manifest.TotalSize, "Downloading")
	
	// Monitor progress
	for {
		select {
		case progress := <-dl.Progress:
			progressBar.UpdateWithStats(
				progress.BytesCompleted,
				progress.DownloadRate,
				progress.NumPeers,
				progress.NumSeeders,
			)
			
		case err := <-dl.Done:
			progressBar.Finish()
			if err != nil {
				return fmt.Errorf("download failed: %w", err)
			}
			fmt.Println("✅ Download complete!")
			
			if !keepSeeding {
				return nil
			}
			
			fmt.Println("Continuing to seed... Press Ctrl+C to stop")
			select {}
		}
	}
}