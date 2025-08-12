package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/models"
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

	// Create registry
	registry, err := models.NewRegistry(paths)
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}

	// Rescan to ensure we have the latest
	if err := registry.Rescan(); err != nil {
		fmt.Printf("Warning: Failed to rescan models: %v\n", err)
	}

	fmt.Println("Locally managed models:")
	fmt.Println()

	// Get all models from registry
	modelList := registry.ListModels()
	
	if len(modelList) == 0 {
		fmt.Println("No models found.")
		fmt.Println("\nUse 'silmaril get <model-name>' to download a model.")
		fmt.Println("Use 'silmaril mirror <huggingface-url>' to mirror a model from HuggingFace.")
		fmt.Println("Use 'silmaril discover' to see available models on the network.")
		return nil
	}

	// Sort models alphabetically
	sort.Strings(modelList)

	// Display each model with details
	for _, modelName := range modelList {
		manifest, err := registry.GetManifest(modelName)
		if err != nil {
			continue
		}

		displayModel(manifest)
	}

	// Show summary
	fmt.Printf("\nTotal models: %d\n", len(modelList))
	
	usage, err := paths.GetDiskUsage()
	if err == nil {
		fmt.Printf("Disk usage: %.2f GB\n", float64(usage.Models)/(1024*1024*1024))
	}

	return nil
}

func displayModel(manifest *types.ModelManifest) {
	sizeGB := float64(manifest.TotalSize) / (1024 * 1024 * 1024)
	
	fmt.Printf("  %s", manifest.Name)
	if manifest.Version != "" && manifest.Version != "local" && manifest.Version != "main" {
		fmt.Printf(" (v%s)", manifest.Version)
	}
	fmt.Println()
	
	fmt.Printf("    Size: %.2f GB", sizeGB)
	if manifest.License != "" {
		fmt.Printf(" | License: %s", manifest.License)
	}
	fmt.Println()
	
	if manifest.ModelType != "" || manifest.Architecture != "" {
		fmt.Printf("    Type: %s", manifest.ModelType)
		if manifest.Architecture != "" {
			fmt.Printf(" | Architecture: %s", manifest.Architecture)
		}
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
	
	if manifest.Description != "" && manifest.Description != fmt.Sprintf("Model mirrored from HuggingFace: %s", manifest.Name) {
		// Truncate long descriptions
		desc := manifest.Description
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		fmt.Printf("    %s\n", desc)
	}
	
	if manifest.MagnetURI != "" {
		fmt.Println("    ✓ Ready to share via P2P")
	}
	
	fmt.Println()
}