package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/api/client"
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
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Create API client
	apiClient := client.NewClient(getDaemonURL())

	// Get list of models from API
	models, err := apiClient.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	fmt.Println("Locally managed models:")
	fmt.Println()

	if len(models) == 0 {
		fmt.Println("No models found.")
		fmt.Println("\nUse 'silmaril get <model-name>' to download a model.")
		fmt.Println("Use 'silmaril mirror <huggingface-url>' to mirror a model from HuggingFace.")
		fmt.Println("Use 'silmaril discover' to see available models on the network.")
		return nil
	}

	// Sort models by name
	sort.Slice(models, func(i, j int) bool {
		return getModelName(models[i]) < getModelName(models[j])
	})

	// Display each model
	totalSize := int64(0)
	for _, model := range models {
		displayModelFromAPI(model)
		if size, ok := model["total_size"].(float64); ok {
			totalSize += int64(size)
		} else if size, ok := model["size"].(float64); ok {
			totalSize += int64(size)
		}
	}

	// Show summary
	fmt.Printf("\nTotal models: %d\n", len(models))
	if totalSize > 0 {
		fmt.Printf("Total disk usage: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	}

	return nil
}

func displayModelFromAPI(model map[string]interface{}) {
	name := getModelName(model)
	fmt.Printf("  %s", name)
	
	if version, ok := model["version"].(string); ok && version != "" && version != "local" && version != "main" {
		fmt.Printf(" (v%s)", version)
	}
	fmt.Println()
	
	// Size
	var sizeGB float64
	if size, ok := model["total_size"].(float64); ok {
		sizeGB = size / (1024 * 1024 * 1024)
	} else if size, ok := model["size"].(float64); ok {
		sizeGB = size / (1024 * 1024 * 1024)
	}
	
	if sizeGB > 0 {
		fmt.Printf("    Size: %.2f GB", sizeGB)
	}
	
	if license, ok := model["license"].(string); ok && license != "" {
		fmt.Printf(" | License: %s", license)
	}
	fmt.Println()
	
	// Model type and architecture
	if modelType, ok := model["model_type"].(string); ok && modelType != "" {
		fmt.Printf("    Type: %s", modelType)
		
		if arch, ok := model["architecture"].(string); ok && arch != "" {
			fmt.Printf(" | Architecture: %s", arch)
		}
		
		if params, ok := model["parameters"].(float64); ok && params > 0 {
			fmt.Printf(" | Parameters: %.1fB", params/1e9)
		}
		fmt.Println()
	}
	
	// Inference hints
	if hints, ok := model["inference_hints"].(map[string]interface{}); ok {
		if minRAM, ok := hints["min_ram_gb"].(float64); ok && minRAM > 0 {
			fmt.Printf("    Min RAM: %.0f GB", minRAM)
			
			if minVRAM, ok := hints["min_vram_gb"].(float64); ok && minVRAM > 0 {
				fmt.Printf(" | Min VRAM: %.0f GB", minVRAM)
			}
			fmt.Println()
		}
	}
	
	// Description
	if desc, ok := model["description"].(string); ok && desc != "" {
		defaultDesc := fmt.Sprintf("Model imported from %s", name)
		if desc != defaultDesc {
			// Truncate long descriptions
			if len(desc) > 100 {
				desc = desc[:97] + "..."
			}
			fmt.Printf("    %s\n", desc)
		}
	}
	
	// P2P status
	if magnet, ok := model["magnet_uri"].(string); ok && magnet != "" {
		fmt.Println("    ✓ Ready to share via P2P")
	}
	
	fmt.Println()
}

func getModelName(model map[string]interface{}) string {
	if name, ok := model["name"].(string); ok {
		return name
	}
	return "unknown"
}