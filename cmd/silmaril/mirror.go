package main

import (
	"fmt"
	"strings"

	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/spf13/cobra"
)

var mirrorCmd = &cobra.Command{
	Use:   "mirror <huggingface-url>",
	Short: "Mirror a HuggingFace model repository locally",
	Long: `Download a model from HuggingFace using git, add it to the local registry,
and broadcast it on the DHT network for P2P sharing.

Examples:
  silmaril mirror https://huggingface.co/meta-llama/Llama-3.1-8B
  silmaril mirror meta-llama/Llama-3.1-8B
  silmaril mirror https://huggingface.co/mistralai/Mistral-7B-v0.1 --branch main`,
	Args: cobra.ExactArgs(1),
	RunE: runMirror,
}

var (
	mirrorBranch string
	mirrorDepth  int
	skipLFS      bool
	noBroadcast  bool
	noAutoShare  bool
)

func init() {
	mirrorCmd.Flags().StringVar(&mirrorBranch, "branch", "main", "Git branch to clone")
	mirrorCmd.Flags().IntVar(&mirrorDepth, "depth", 1, "Git clone depth (0 for full history)")
	mirrorCmd.Flags().BoolVar(&skipLFS, "skip-lfs", false, "Skip downloading large files via Git LFS")
	mirrorCmd.Flags().BoolVar(&noBroadcast, "no-broadcast", false, "Don't broadcast the model on DHT after mirroring")
	mirrorCmd.Flags().BoolVar(&noAutoShare, "no-auto-share", false, "Don't automatically start sharing after mirroring")
	
	rootCmd.AddCommand(mirrorCmd)
}

func runMirror(cmd *cobra.Command, args []string) error {
	repoURL := args[0]
	
	// Parse and normalize the HuggingFace URL
	modelName, gitURL := parseHFURL(repoURL)
	
	fmt.Printf("Mirroring model: %s\n", modelName)
	fmt.Printf("Repository URL: %s\n", gitURL)
	
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Create API client
	apiClient := client.NewClient(getDaemonURL())
	
	// Check if model already exists
	if _, err := apiClient.GetModel(modelName); err == nil {
		return fmt.Errorf("model %s already exists in registry. Use 'silmaril update' to refresh it", modelName)
	}
	
	fmt.Println("Starting mirror operation...")
	
	// Call mirror API
	result, err := apiClient.MirrorModel(
		gitURL,
		mirrorBranch,
		mirrorDepth,
		skipLFS,
		noBroadcast,
		!noAutoShare, // autoShare is the inverse of noAutoShare
	)
	if err != nil {
		return fmt.Errorf("failed to mirror model: %w", err)
	}
	
	// Display result
	if msg, ok := result["message"].(string); ok {
		fmt.Printf("✅ %s\n", msg)
	}
	
	if status, ok := result["status"].(string); ok {
		fmt.Printf("Status: %s\n", status)
	}
	
	fmt.Println("\nNote: The mirror operation is running in the background.")
	fmt.Println("Use 'silmaril list' to check when the model is available.")
	fmt.Printf("Use 'silmaril share %s' to start sharing once mirroring is complete.\n", modelName)
	
	return nil
}

// parseHFURL parses a HuggingFace URL or model identifier
func parseHFURL(input string) (modelName, gitURL string) {
	// Handle full URLs
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		// Extract model name from URL
		parts := strings.Split(input, "/")
		if len(parts) >= 5 && strings.Contains(input, "huggingface.co") {
			// Format: https://huggingface.co/owner/model
			modelName = parts[3] + "/" + parts[4]
			gitURL = fmt.Sprintf("https://huggingface.co/%s", modelName)
			return
		}
		// Use as-is if not a recognized format
		gitURL = input
		// Try to extract model name from the end
		modelName = parts[len(parts)-2] + "/" + parts[len(parts)-1]
		return
	}
	
	// Handle short format (e.g., "meta-llama/Llama-3.1-8B")
	parts := strings.Split(input, "/")
	if len(parts) == 2 {
		modelName = input
		gitURL = fmt.Sprintf("https://huggingface.co/%s", modelName)
		return
	}
	
	// Default: use input as both
	modelName = input
	gitURL = input
	return
}