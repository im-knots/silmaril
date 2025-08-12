package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List locally downloaded models",
	Long: `Shows models that have been downloaded and are managed by Silmaril on this machine.

This command only shows models stored locally on your computer.
Use 'silmaril discover' to search for models available on the P2P network.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	// Get storage paths
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to get storage paths: %w", err)
	}

	fmt.Println("Locally managed models:")
	fmt.Println()

	// Check models directory
	modelsDir := paths.ModelsDir()
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No models downloaded yet.")
			fmt.Println("\nUse 'silmaril get <model-name>' to download a model.")
			fmt.Println("Use 'silmaril discover' to see available models on the network.")
			return nil
		}
		return fmt.Errorf("failed to read models directory: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No models downloaded yet.")
		fmt.Println("\nUse 'silmaril get <model-name>' to download a model.")
		fmt.Println("Use 'silmaril discover' to see available models on the network.")
		return nil
	}

	// List each model
	modelCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check for manifest file
		manifestPath := filepath.Join(modelsDir, entry.Name(), "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}

		// Read manifest
		manifest, err := readManifest(manifestPath)
		if err != nil {
			fmt.Printf("  %s (error reading manifest: %v)\n", entry.Name(), err)
			continue
		}

		modelCount++
		displayModel(manifest, entry.Name())
	}

	if modelCount == 0 {
		fmt.Println("No valid models found.")
		fmt.Println("\nUse 'silmaril get <model-name>' to download a model.")
		fmt.Println("Use 'silmaril discover' to see available models on the network.")
	} else {
		// Show disk usage
		fmt.Printf("\nTotal models: %d\n", modelCount)
		
		usage, err := paths.GetDiskUsage()
		if err == nil {
			fmt.Printf("Disk usage: %.2f GB\n", float64(usage.Models)/(1024*1024*1024))
		}
	}

	return nil
}

func readManifest(path string) (*types.ModelManifest, error) {
	// This is a placeholder - in a real implementation we would read and parse the JSON
	// For now, return a basic manifest
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	return &types.ModelManifest{
		Name:      filepath.Base(filepath.Dir(path)),
		Version:   "local",
		TotalSize: getDirSize(filepath.Dir(path)),
		CreatedAt: info.ModTime(),
	}, nil
}

func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func displayModel(manifest *types.ModelManifest, dirName string) {
	sizeGB := float64(manifest.TotalSize) / (1024 * 1024 * 1024)
	
	fmt.Printf("  %s", dirName)
	if manifest.Version != "" && manifest.Version != "local" {
		fmt.Printf(" (v%s)", manifest.Version)
	}
	fmt.Println()
	
	fmt.Printf("    Size: %.2f GB", sizeGB)
	if manifest.License != "" {
		fmt.Printf(" | License: %s", manifest.License)
	}
	fmt.Println()
	
	if manifest.ModelType != "" || manifest.Architecture != "" {
		fmt.Printf("    Type: %s | Architecture: %s", manifest.ModelType, manifest.Architecture)
		if manifest.Parameters > 0 {
			fmt.Printf(" | Parameters: %.1fB", float64(manifest.Parameters)/1e9)
		}
		fmt.Println()
	}
	
	if manifest.InferenceHints.MinRAM > 0 {
		fmt.Printf("    Min RAM: %d GB", manifest.InferenceHints.MinRAM)
		if manifest.InferenceHints.MinVRAM > 0 {
			fmt.Printf(" | Min VRAM: %d GB", manifest.InferenceHints.MinVRAM)
		}
		fmt.Println()
	}
	
	if manifest.Description != "" {
		fmt.Printf("    %s\n", manifest.Description)
	}
	
	fmt.Println()
}