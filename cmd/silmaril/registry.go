package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
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

func init() {
	rootCmd.AddCommand(registryCmd)
	registryCmd.AddCommand(importCmd)
	registryCmd.AddCommand(exportCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	manifestPath := args[0]
	
	// Read manifest file
	data, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}
	
	var manifest models.ModelManifest
	err = json.Unmarshal(data, &manifest)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	
	// Validate manifest
	if manifest.Name == "" {
		return fmt.Errorf("manifest missing model name")
	}
	if manifest.MagnetURI == "" {
		return fmt.Errorf("manifest missing magnet URI")
	}
	
	// Save to local registry
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	
	registryDir := filepath.Join(home, ".silmaril", "registry")
	os.MkdirAll(registryDir, 0755)
	
	// Use model name as filename (replace / with -)
	filename := filepath.Base(manifest.Name) + ".json"
	registryPath := filepath.Join(registryDir, filename)
	
	// Write manifest to registry
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	
	err = ioutil.WriteFile(registryPath, manifestData, 0644)
	if err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}
	
	fmt.Printf("✅ Imported model: %s\n", manifest.Name)
	fmt.Printf("   Version: %s\n", manifest.Version)
	fmt.Printf("   License: %s\n", manifest.License)
	fmt.Printf("   Size: %.2f GB\n", float64(manifest.TotalSize)/(1024*1024*1024))
	fmt.Printf("   Saved to: %s\n", registryPath)
	
	return nil
}

func runExport(cmd *cobra.Command, args []string) error {
	modelName := args[0]
	
	// Initialize paths
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	// Get from registry
	registry, err := models.NewRegistry(paths)
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}
	
	manifest, err := registry.GetManifest(modelName)
	if err != nil {
		return fmt.Errorf("model not found in registry: %w", err)
	}
	
	// Export to stdout
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	
	fmt.Println(string(manifestData))
	
	return nil
}