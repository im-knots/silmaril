package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/spf13/cobra"
)

var shareCmd = &cobra.Command{
	Use:   "share [model-name, path, directory, or repository URL]",
	Short: "Share/publish models to the P2P network",
	Long: `Share models by seeding them to the P2P network, publish a new model
from a directory, or clone and share from a git/HuggingFace repository.

You can specify:
  - A model name from registry (e.g., "meta-llama/Llama-3.1-8B")
  - A path to a model directory to publish and seed
  - A git repository URL (automatically clones and shares)
  - A HuggingFace model URL or identifier
  - Use --all to seed all downloaded models

Examples:
  silmaril share --all                          # Seed all models
  silmaril share meta-llama/Llama-3.1-8B        # Seed specific model from registry
  silmaril share https://huggingface.co/meta-llama/Llama-3.1-8B  # Clone and share from HF
  silmaril share mistralai/Mistral-7B-v0.1      # Clone and share using HF short format
  silmaril share /path/to/model/dir --name org/model --license apache-2.0  # Publish local dir`,
	RunE: runShare,
}

var (
	shareAll     bool
	modelName    string
	modelVersion string
	modelLicense string
	pieceLength  int64
	skipDHT      bool
	signManifest bool
	noMonitor    bool
	// Git/repo cloning options
	gitBranch    string
	gitDepth     int
	skipLFS      bool
)

func init() {
	rootCmd.AddCommand(shareCmd)

	shareCmd.Flags().BoolVar(&shareAll, "all", false, "seed all downloaded models")

	// Publish flags (only needed when creating torrent from directory)
	shareCmd.Flags().StringVar(&modelName, "name", "", "model name for publishing (e.g., org/model-name)")
	shareCmd.Flags().StringVar(&modelVersion, "version", "main", "model version/revision")
	shareCmd.Flags().StringVar(&modelLicense, "license", "", "model license")
	shareCmd.Flags().Int64Var(&pieceLength, "piece-length", 4*1024*1024, "piece length for torrent (default 4MB)")
	shareCmd.Flags().BoolVar(&skipDHT, "skip-dht", false, "skip DHT announcement")
	shareCmd.Flags().BoolVar(&signManifest, "sign", true, "sign the manifest")
	shareCmd.Flags().BoolVar(&noMonitor, "no-monitor", true, "don't monitor seeding progress after sharing")
	
	// Git/repo cloning flags
	shareCmd.Flags().StringVar(&gitBranch, "branch", "main", "Git branch to clone (for repository URLs)")
	shareCmd.Flags().IntVar(&gitDepth, "depth", 1, "Git clone depth, 0 for full history (for repository URLs)")
	shareCmd.Flags().BoolVar(&skipLFS, "skip-lfs", false, "Skip downloading large files via Git LFS (for repository URLs)")
}

func runShare(cmd *cobra.Command, args []string) error {
	// Ensure daemon is running
	if err := ensureDaemonRunning(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Create API client
	apiClient := client.NewClient(getDaemonURL())

	var modelNameToShare string
	var pathToShare string

	if shareAll {
		// Share all models
		fmt.Println("Seeding all downloaded models...")

		opts := client.ShareModelOptions{
			All: true,
		}
		result, err := apiClient.ShareModel(opts)
		if err != nil {
			return fmt.Errorf("failed to share models: %w", err)
		}

		modelsShared := 0
		totalModels := 0
		if ms, ok := result["models_shared"].(float64); ok {
			modelsShared = int(ms)
		}
		if tm, ok := result["total_models"].(float64); ok {
			totalModels = int(tm)
		}

		if totalModels == 0 {
			fmt.Println("No models found in registry.")
			fmt.Println("Use 'silmaril get' or 'silmaril mirror' to download models first.")
			return nil
		}

		fmt.Printf("✅ Started sharing %d out of %d models\n", modelsShared, totalModels)

	} else if len(args) > 0 {
		input := args[0]

		// Check if it's a repository URL (git or HuggingFace)
		if isRepositoryURL(input) {
			// It's a git/HF repo - need to clone it first
			repoModelName, gitURL := parseRepositoryURL(input)
			
			fmt.Printf("Cloning repository: %s\n", gitURL)
			fmt.Printf("Model will be named: %s\n", repoModelName)
			
			// Check if it's a HuggingFace URL and HF_TOKEN is not set
			if strings.Contains(gitURL, "huggingface.co") && os.Getenv("HF_TOKEN") == "" {
				fmt.Println("Note: Some models require authentication. Set HF_TOKEN environment variable for gated models.")
			}
			
			// Use the share API with repository options
			opts := client.ShareModelOptions{
				RepoURL: gitURL,
				Branch:  gitBranch,
				Depth:   gitDepth,
				SkipLFS: skipLFS,
				SkipDHT: skipDHT,
			}
			
			result, err := apiClient.ShareModel(opts)
			if err != nil {
				return fmt.Errorf("failed to clone and share repository: %w", err)
			}
			
			if msg, ok := result["message"].(string); ok {
				fmt.Printf("✅ %s\n", msg)
			}
			
			fmt.Println("\nRepository is being cloned and shared in the background.")
			fmt.Println("Use 'silmaril list' to check when the model is available.")
			return nil
			
		} else {
			// Check if it's a file or directory that exists
			info, err := os.Stat(input)
			if err == nil {
				// Path exists - check what it is
				if info.IsDir() {
					// Directory - we can publish via the API
					// Convert to absolute path for the daemon
					absPath, err := filepath.Abs(input)
					if err != nil {
						return fmt.Errorf("failed to get absolute path: %w", err)
					}
					pathToShare = absPath
					fmt.Printf("Publishing model from directory: %s\n", input)
				} else if filepath.Ext(input) == ".torrent" {
					// Direct torrent file - not supported via directory path
					return fmt.Errorf("sharing torrent files directly is not yet implemented")
				} else {
					return fmt.Errorf("invalid input: must be a model name or directory")
				}
			} else if strings.Contains(input, "/") && !strings.HasPrefix(input, "./") && !strings.HasPrefix(input, "../") {
				// Path doesn't exist but contains "/" and not a relative path - could be model name or HF short format
				// Try to detect if it's a HuggingFace short format (org/model)
				if isHuggingFaceShortFormat(input) {
					// It's a HF short format - treat as repo
					gitURL := fmt.Sprintf("https://huggingface.co/%s", input)
					fmt.Printf("Cloning HuggingFace model: %s\n", input)
					
					// Check if HF_TOKEN is not set
					if os.Getenv("HF_TOKEN") == "" {
						fmt.Println("Note: Some models require authentication. Set HF_TOKEN environment variable for gated models.")
					}
					
					// Use the share API with repository options
					opts := client.ShareModelOptions{
						RepoURL: gitURL,
						Branch:  gitBranch,
						Depth:   gitDepth,
						SkipLFS: skipLFS,
						SkipDHT: skipDHT,
					}
					
					result, err := apiClient.ShareModel(opts)
					if err != nil {
						return fmt.Errorf("failed to clone and share HuggingFace model: %w", err)
					}
					
					if msg, ok := result["message"].(string); ok {
						fmt.Printf("✅ %s\n", msg)
					}
					
					fmt.Println("\nModel is being cloned and shared in the background.")
					fmt.Println("Use 'silmaril list' to check when the model is available.")
					return nil
				} else {
					// Assume it's a model name from registry
					modelNameToShare = input
					fmt.Printf("Seeding model from registry: %s\n", input)
				}
			} else {
				// Not a valid path and not a model name
				return fmt.Errorf("'%s' is not a valid model name, path, or repository URL", input)
			}
		}

		// Build share options
		opts := client.ShareModelOptions{
			ModelName:    modelNameToShare,
			Path:         pathToShare,
			All:          false,
			Name:         modelName,    // From --name flag
			License:      modelLicense, // From --license flag
			Version:      modelVersion, // From --version flag
			PieceLength:  pieceLength,  // From --piece-length flag
			SkipDHT:      skipDHT,      // From --skip-dht flag
			SignManifest: signManifest, // From --sign flag
		}
		

		// Share the specific model or path
		result, err := apiClient.ShareModel(opts)
		if err != nil {
			return fmt.Errorf("failed to share: %w", err)
		}

		// Check if the result contains an error
		if errMsg, ok := result["error"].(string); ok {
			return fmt.Errorf("API error: %s", errMsg)
		}

		if msg, ok := result["message"].(string); ok {
			fmt.Printf("✅ %s\n", msg)
		}

		if transferID, ok := result["transfer_id"].(string); ok {
			fmt.Printf("Transfer ID: %s\n", transferID)
		}

	} else {
		// No arguments and not --all
		fmt.Println("Please specify a model name, repository URL, or use --all to seed all models")
		fmt.Println("\nExamples:")
		fmt.Println("  silmaril share --all                                    # Seed all models")
		fmt.Println("  silmaril share meta-llama/Llama-3.1-8B                  # Share from registry or clone from HF")
		fmt.Println("  silmaril share https://huggingface.co/mistralai/Mistral-7B-v0.1  # Clone and share")
		fmt.Println("  silmaril share /path/to/model/dir --name org/model      # Publish local directory")
		return nil
	}

	// Exit early if monitoring disabled
	if noMonitor {
		fmt.Println("\nModel is being shared by the daemon in the background.")
		fmt.Println("Use 'silmaril daemon status' to check the daemon status.")
		return nil
	}

	// Monitor seeding if requested
	fmt.Println("\nSeeding models... Press Ctrl+C to stop")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Monitor seeding
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get transfer stats from daemon
			transfers, err := apiClient.ListTransfers("active")
			if err != nil {
				fmt.Printf("\rError getting transfer stats: %v", err)
				continue
			}

			seedCount := 0
			totalPeers := 0
			var totalUploaded float64

			for _, transfer := range transfers {
				if transferType, ok := transfer["type"].(string); ok && transferType == "seed" {
					seedCount++
				}
				if peers, ok := transfer["peers"].(float64); ok {
					totalPeers += int(peers)
				}
				if uploaded, ok := transfer["bytes_transferred"].(float64); ok {
					totalUploaded += uploaded
				}
			}

			fmt.Printf("\rActive seeds: %d | Peers: %d | Total uploaded: %.2f GB",
				seedCount, totalPeers, totalUploaded/(1024*1024*1024))

		case <-sigChan:
			fmt.Println("\n\nShutting down...")
			// The daemon will continue seeding even after we exit
			fmt.Println("Note: The daemon will continue seeding in the background.")
			fmt.Println("Use 'silmaril daemon stop' to stop the daemon and all transfers.")
			return nil
		}
	}
}

// isRepositoryURL checks if the input is a git or HuggingFace repository URL
func isRepositoryURL(input string) bool {
	return strings.HasPrefix(input, "http://") || 
		strings.HasPrefix(input, "https://") || 
		strings.HasPrefix(input, "git://") ||
		strings.HasPrefix(input, "git@")
}

// parseRepositoryURL parses a repository URL and returns model name and git URL
func parseRepositoryURL(input string) (modelName, gitURL string) {
	// Handle HuggingFace URLs
	if strings.Contains(input, "huggingface.co") {
		parts := strings.Split(input, "/")
		if len(parts) >= 5 {
			// Format: https://huggingface.co/owner/model
			modelName = parts[3] + "/" + parts[4]
			gitURL = input
			return
		}
	}
	
	// Handle GitHub URLs
	if strings.Contains(input, "github.com") {
		parts := strings.Split(input, "/")
		if len(parts) >= 5 {
			// Format: https://github.com/owner/repo
			owner := parts[3]
			repo := strings.TrimSuffix(parts[4], ".git")
			modelName = owner + "/" + repo
			gitURL = input
			return
		}
	}
	
	// For other git URLs, try to extract a reasonable name
	gitURL = input
	parts := strings.Split(input, "/")
	if len(parts) >= 2 {
		modelName = strings.TrimSuffix(parts[len(parts)-1], ".git")
		if len(parts) >= 3 {
			modelName = parts[len(parts)-2] + "/" + modelName
		}
	} else {
		modelName = "unknown/model"
	}
	
	return
}

// isHuggingFaceShortFormat checks if the input looks like a HuggingFace model identifier
func isHuggingFaceShortFormat(input string) bool {
	// Check if it's in format "org/model" without being a file path
	if !strings.Contains(input, "://") && strings.Count(input, "/") == 1 {
		parts := strings.Split(input, "/")
		// Both parts should be non-empty and not contain path indicators
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" &&
			!strings.Contains(parts[0], ".") && !strings.Contains(parts[1], "..") {
			// Check if model already exists in local registry
			// If it does, it's probably a reference to an existing model
			// If not, treat it as a HuggingFace identifier to clone
			apiClient := client.NewClient(getDaemonURL())
			if _, err := apiClient.GetModel(input); err != nil {
				// Model doesn't exist locally, likely a HF identifier
				return true
			}
		}
	}
	return false
}
