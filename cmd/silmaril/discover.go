package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
)

var discoverCmd = &cobra.Command{
	Use:   "discover [model-name]",
	Short: "Discover models available on the P2P network",
	Long: `Discover models shared on the DHT network by other Silmaril users.
Models are discovered using the 'silmaril:' prefix in the DHT.

This command searches the distributed network for available models.
Use 'silmaril list' to see models already downloaded to your machine.`,
	RunE: runDiscover,
}

var (
	discoverTimeout int
)

func init() {
	rootCmd.AddCommand(discoverCmd)
	
	discoverCmd.Flags().IntVarP(&discoverTimeout, "timeout", "t", 30, "discovery timeout in seconds")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	fmt.Println("Discovering models on the DHT network...")
	fmt.Printf("Using prefix: %s\n", federation.SilmarilPrefix)
	
	// Initialize paths
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	// Create DHT discovery
	dht, err := federation.NewDHTDiscovery(paths.BaseDir(), 0)
	if err != nil {
		return fmt.Errorf("failed to create DHT client: %w", err)
	}
	defer dht.Close()
	
	// Bootstrap to DHT
	fmt.Print("Connecting to DHT network...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	err = dht.Bootstrap(ctx)
	if err != nil {
		fmt.Printf("\nWarning: DHT bootstrap incomplete: %v\n", err)
		fmt.Println("Discovery might be limited.")
	} else {
		fmt.Println(" connected!")
	}
	
	// If specific model name provided, search for it
	if len(args) > 0 {
		modelName := args[0]
		fmt.Printf("\nSearching for model: %s\n", modelName)
		
		// Search with timeout
		searchCtx, searchCancel := context.WithTimeout(context.Background(), time.Duration(discoverTimeout)*time.Second)
		defer searchCancel()
		
		found := false
		// Try exact name and common patterns
		patterns := []string{
			modelName,
			fmt.Sprintf("%s/latest", modelName),
			fmt.Sprintf("%s/main", modelName),
		}
		
		for _, pattern := range patterns {
			manifest := searchForModel(dht, searchCtx, pattern)
			if manifest != nil {
				found = true
				displayDiscoveredModel(manifest)
				break
			}
		}
		
		if !found {
			fmt.Println("\nModel not found on the network.")
			fmt.Println("The model might not be shared yet, or you may need to wait for DHT propagation.")
		}
		
		return nil
	}
	
	// Otherwise, discover all available models
	fmt.Println("\nSearching for models (this may take a moment)...")
	
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(discoverTimeout)*time.Second)
	defer cancel()
	
	manifests, err := dht.DiscoverModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover models: %w", err)
	}
	
	if len(manifests) == 0 {
		fmt.Println("\nNo models found on the network yet.")
		fmt.Println("Models shared by other users will appear here as they are announced to the DHT.")
		fmt.Println("\nTo share your own model, use:")
		fmt.Println("  silmaril publish <model-directory> --name <org/model>")
		return nil
	}
	
	fmt.Printf("\nFound %d models on the network:\n\n", len(manifests))
	
	// Group by organization
	byOrg := make(map[string][]*types.ModelManifest)
	for _, manifest := range manifests {
		parts := strings.Split(manifest.Name, "/")
		org := "unknown"
		if len(parts) > 1 {
			org = parts[0]
		}
		byOrg[org] = append(byOrg[org], manifest)
	}
	
	// Display grouped
	for org, models := range byOrg {
		fmt.Printf("  %s:\n", org)
		for _, manifest := range models {
			fmt.Printf("    - %s", manifest.Name)
			if manifest.Version != "" && manifest.Version != "discovered" {
				fmt.Printf(" (v%s)", manifest.Version)
			}
			if manifest.Size > 0 {
				sizeGB := float64(manifest.Size) / (1024 * 1024 * 1024)
				fmt.Printf(" - %.1f GB", sizeGB)
			}
			fmt.Println()
		}
		fmt.Println()
	}
	
	fmt.Println("To get more information about a model:")
	fmt.Println("  silmaril discover <model-name>")
	fmt.Println("\nTo download a model:")
	fmt.Println("  silmaril get <model-name>")
	
	return nil
}

func searchForModel(dht *federation.DHTDiscovery, ctx context.Context, modelName string) *types.ModelManifest {
	// This is a wrapper around the DHT search
	// In a real implementation, this would query peers for manifest data
	manifest, _ := dht.SearchForModel(ctx, modelName)
	return manifest
}

func displayDiscoveredModel(manifest *types.ModelManifest) {
	fmt.Printf("\n✅ Found model: %s\n", manifest.Name)
	
	if manifest.Version != "" && manifest.Version != "discovered" {
		fmt.Printf("   Version: %s\n", manifest.Version)
	}
	
	if manifest.License != "" {
		fmt.Printf("   License: %s\n", manifest.License)
	}
	
	if manifest.TotalSize > 0 {
		sizeGB := float64(manifest.TotalSize) / (1024 * 1024 * 1024)
		fmt.Printf("   Size: %.2f GB\n", sizeGB)
	} else if manifest.Size > 0 {
		sizeGB := float64(manifest.Size) / (1024 * 1024 * 1024)
		fmt.Printf("   Size: %.2f GB\n", sizeGB)
	}
	
	if manifest.ModelType != "" {
		fmt.Printf("   Type: %s", manifest.ModelType)
		if manifest.Architecture != "" {
			fmt.Printf(" (%s)", manifest.Architecture)
		}
		fmt.Println()
	}
	
	if manifest.Parameters > 0 {
		fmt.Printf("   Parameters: %.1fB\n", float64(manifest.Parameters)/1e9)
	}
	
	if manifest.Description != "" {
		fmt.Printf("   Description: %s\n", manifest.Description)
	}
	
	if manifest.MagnetURI != "" {
		fmt.Printf("\n   Magnet: %s", manifest.MagnetURI)
		if len(manifest.MagnetURI) > 60 {
			fmt.Printf("...%s", manifest.MagnetURI[len(manifest.MagnetURI)-20:])
		}
		fmt.Println()
	}
	
	fmt.Println("\nTo download this model:")
	fmt.Printf("  silmaril get %s\n", manifest.Name)
}