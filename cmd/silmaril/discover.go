package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/api/client"
)

var discoverCmd = &cobra.Command{
	Use:   "discover [pattern]",
	Short: "Search for models available on the P2P network",
	Long: `Discover models being shared by other users on the P2P network.

You can optionally provide a search pattern to filter results:
  silmaril discover          # Show all available models
  silmaril discover llama     # Show models containing "llama"
  silmaril discover meta-     # Show models starting with "meta-"

This searches for models via DHT (Distributed Hash Table) on the BitTorrent network.`,
	RunE: runDiscover,
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().IntP("timeout", "t", 30, "Discovery timeout in seconds")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Get search pattern
	pattern := ""
	if len(args) > 0 {
		pattern = strings.Join(args, " ")
	}

	fmt.Println("Discovering models on the P2P network...")
	if pattern != "" {
		fmt.Printf("Searching for: %s\n", pattern)
	}
	fmt.Println()

	// Create API client
	apiClient := client.NewClient(getDaemonURL())

	// Discover models via API
	models, err := apiClient.DiscoverModels(pattern)
	if err != nil {
		return fmt.Errorf("failed to discover models: %w", err)
	}

	if len(models) == 0 {
		fmt.Println("No models found on the network.")
		if pattern != "" {
			fmt.Println("\nTry a different search pattern or run without arguments to see all models.")
		} else {
			fmt.Println("\nModels shared by other users will appear here as they are announced to the DHT.")
			fmt.Println("To share your own models, use: silmaril share <model-name>")
		}
		return nil
	}

	fmt.Printf("Found %d model(s) on the network:\n\n", len(models))

	// Group by organization
	byOrg := make(map[string][]map[string]interface{})
	for _, model := range models {
		name := ""
		if n, ok := model["name"].(string); ok {
			name = n
		}
		
		parts := strings.Split(name, "/")
		org := "unknown"
		if len(parts) > 1 {
			org = parts[0]
		}
		byOrg[org] = append(byOrg[org], model)
	}

	// Display grouped by organization
	for org, orgModels := range byOrg {
		fmt.Printf("  %s:\n", org)
		for _, model := range orgModels {
			displayDiscoveredModel(model, true)
		}
		fmt.Println()
	}

	fmt.Println("To download a model, use: silmaril get <model-name>")

	return nil
}

func displayDiscoveredModel(model map[string]interface{}, indent bool) {
	prefix := "  "
	if indent {
		prefix = "    - "
	}
	
	name := ""
	if n, ok := model["name"].(string); ok {
		name = n
	}
	
	fmt.Printf("%s%s", prefix, name)
	
	if version, ok := model["version"].(string); ok && version != "" && version != "main" {
		fmt.Printf(" (v%s)", version)
	}
	
	// Size
	if size, ok := model["size"].(float64); ok && size > 0 {
		sizeGB := size / (1024 * 1024 * 1024)
		fmt.Printf(" - %.2f GB", sizeGB)
	}
	
	fmt.Println()
}