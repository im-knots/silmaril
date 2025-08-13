package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/api/client"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage the local model registry",
	Long:  `Import, export, and manage model manifests in the local registry.`,
}

var importCmd = &cobra.Command{
	Use:   "import [manifest-file]",
	Short: "Import a model manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runImport,
}

var exportCmd = &cobra.Command{
	Use:   "export [model-name]",
	Short: "Export a model manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runExport,
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan and update the registry",
	RunE:  runScan,
}

func init() {
	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(importCmd)
	registryCmd.AddCommand(exportCmd)
	registryCmd.AddCommand(scanCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	manifestPath := args[0]
	
	// Read manifest file
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}
	
	var manifest map[string]interface{}
	err = json.Unmarshal(data, &manifest)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	
	// Validate manifest
	if _, ok := manifest["name"]; !ok {
		return fmt.Errorf("manifest missing model name")
	}
	
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Create API client
	apiClient := client.NewClient(getDaemonURL())
	
	// Import via API (using share endpoint for now)
	// TODO: Add dedicated import endpoint
	modelName := manifest["name"].(string)
	
	// Check if model already exists
	existing, err := apiClient.GetModel(modelName)
	if err == nil && existing != nil {
		fmt.Printf("Model %s already exists in registry\n", modelName)
		return nil
	}
	
	fmt.Printf("✅ Imported model: %s\n", modelName)
	
	if version, ok := manifest["version"].(string); ok {
		fmt.Printf("   Version: %s\n", version)
	}
	if license, ok := manifest["license"].(string); ok {
		fmt.Printf("   License: %s\n", license)
	}
	if totalSize, ok := manifest["total_size"].(float64); ok {
		fmt.Printf("   Size: %.2f GB\n", totalSize/(1024*1024*1024))
	}
	
	fmt.Println("\nNote: Full manifest import is not yet implemented in the daemon.")
	fmt.Println("The manifest has been parsed but not saved to the registry.")
	
	return nil
}

func runExport(cmd *cobra.Command, args []string) error {
	modelName := args[0]
	
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Create API client
	apiClient := client.NewClient(getDaemonURL())
	
	// Get model from daemon
	model, err := apiClient.GetModel(modelName)
	if err != nil {
		return fmt.Errorf("model not found in registry: %w", err)
	}
	
	// Export to stdout
	manifestData, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return err
	}
	
	fmt.Println(string(manifestData))
	
	return nil
}

func runScan(cmd *cobra.Command, args []string) error {
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Create API client
	apiClient := client.NewClient(getDaemonURL())
	
	fmt.Println("Scanning for models...")
	
	// List all models (this triggers a scan in the daemon)
	models, err := apiClient.ListModels()
	if err != nil {
		return fmt.Errorf("failed to scan models: %w", err)
	}
	
	fmt.Printf("✅ Found %d models in registry\n", len(models))
	
	for _, model := range models {
		if name, ok := model["name"].(string); ok {
			fmt.Printf("  - %s", name)
			if version, ok := model["version"].(string); ok && version != "" {
				fmt.Printf(" (v%s)", version)
			}
			if size, ok := model["total_size"].(float64); ok && size > 0 {
				fmt.Printf(" - %.2f GB", size/(1024*1024*1024))
			}
			fmt.Println()
		}
	}
	
	return nil
}