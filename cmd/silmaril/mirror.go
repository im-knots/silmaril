package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
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
)

func init() {
	mirrorCmd.Flags().StringVar(&mirrorBranch, "branch", "main", "Git branch to clone")
	mirrorCmd.Flags().IntVar(&mirrorDepth, "depth", 1, "Git clone depth (0 for full history)")
	mirrorCmd.Flags().BoolVar(&skipLFS, "skip-lfs", false, "Skip downloading large files via Git LFS")
	mirrorCmd.Flags().BoolVar(&noBroadcast, "no-broadcast", false, "Don't broadcast the model on DHT after mirroring")
	
	rootCmd.AddCommand(mirrorCmd)
}

func runMirror(cmd *cobra.Command, args []string) error {
	repoURL := args[0]
	
	// Parse and normalize the HuggingFace URL
	modelName, gitURL, err := parseHFURL(repoURL)
	if err != nil {
		return fmt.Errorf("invalid HuggingFace URL: %w", err)
	}
	
	fmt.Printf("Mirroring model: %s\n", modelName)
	fmt.Printf("Repository URL: %s\n", gitURL)
	
	// Initialize paths
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	// Initialize registry
	registry, err := models.NewRegistry(paths)
	if err != nil {
		return fmt.Errorf("failed to initialize registry: %w", err)
	}
	
	// Check if model already exists
	if _, err := registry.GetManifest(modelName); err == nil {
		return fmt.Errorf("model %s already exists in registry. Use 'silmaril update' to refresh it", modelName)
	}
	
	// Create model directory
	modelPath := paths.ModelPath(modelName)
	if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}
	
	// Clone the repository
	fmt.Printf("Cloning repository to %s...\n", modelPath)
	if err := cloneRepository(gitURL, modelPath); err != nil {
		// Clean up on failure
		os.RemoveAll(modelPath)
		return fmt.Errorf("failed to clone repository: %w", err)
	}
	
	fmt.Println("Repository cloned successfully")
	
	// Generate manifest for the model
	fmt.Println("Generating model manifest...")
	manifest, err := generateManifestFromRepo(modelPath, modelName)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %w", err)
	}
	
	// Add version from git
	if version, err := getGitRevision(modelPath); err == nil {
		manifest.Version = version[:8] // Use first 8 chars of commit hash
	}
	
	// Save manifest to registry
	if err := registry.SaveManifest(manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}
	
	fmt.Printf("Model registered: %s (version: %s)\n", manifest.Name, manifest.Version)
	fmt.Printf("Total size: %.2f GB\n", float64(manifest.TotalSize)/(1024*1024*1024))
	fmt.Printf("Files: %d\n", len(manifest.Files))
	
	// Broadcast on DHT if not disabled
	if !noBroadcast {
		fmt.Println("Broadcasting model on DHT network...")
		if err := broadcastModel(manifest); err != nil {
			fmt.Printf("Warning: Failed to broadcast on DHT: %v\n", err)
			// Don't fail the command if broadcast fails
		} else {
			fmt.Println("Model successfully broadcast on DHT network")
		}
	}
	
	fmt.Printf("\nModel successfully mirrored!\n")
	fmt.Printf("You can now share it with: silmaril share %s\n", modelName)
	
	return nil
}

// parseHFURL parses a HuggingFace URL or model identifier
func parseHFURL(input string) (modelName, gitURL string, err error) {
	// Handle full URLs
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", "", err
		}
		
		// Extract model name from path
		path := strings.TrimPrefix(u.Path, "/")
		parts := strings.Split(path, "/")
		
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid HuggingFace URL format")
		}
		
		modelName = parts[0] + "/" + parts[1]
		gitURL = fmt.Sprintf("https://huggingface.co/%s", modelName)
		return modelName, gitURL, nil
	}
	
	// Handle short format (e.g., "meta-llama/Llama-3.1-8B")
	parts := strings.Split(input, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model identifier format. Expected: owner/model")
	}
	
	modelName = input
	gitURL = fmt.Sprintf("https://huggingface.co/%s", modelName)
	return modelName, gitURL, nil
}

// cloneRepository clones a git repository
func cloneRepository(gitURL, targetPath string) error {
	args := []string{"clone"}
	
	// Add depth flag if specified
	if mirrorDepth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", mirrorDepth))
	}
	
	// Add branch flag
	if mirrorBranch != "" {
		args = append(args, "--branch", mirrorBranch)
	}
	
	// Skip LFS if requested
	if skipLFS {
		// Set GIT_LFS_SKIP_SMUDGE to skip LFS downloads
		os.Setenv("GIT_LFS_SKIP_SMUDGE", "1")
		defer os.Unsetenv("GIT_LFS_SKIP_SMUDGE")
	}
	
	args = append(args, gitURL, targetPath)
	
	// Execute git clone
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	
	// If not skipping LFS, pull LFS files
	if !skipLFS {
		// Check if repository uses LFS
		if hasLFSFiles(targetPath) {
			fmt.Println("Downloading LFS files...")
			lfscmd := exec.Command("git", "lfs", "pull")
			lfscmd.Dir = targetPath
			lfscmd.Stdout = os.Stdout
			lfscmd.Stderr = os.Stderr
			
			if err := lfscmd.Run(); err != nil {
				// LFS might not be installed, warn but don't fail
				fmt.Printf("Warning: Failed to pull LFS files: %v\n", err)
				fmt.Println("You may need to install Git LFS to download large model files")
			}
		}
	}
	
	return nil
}

// hasLFSFiles checks if a repository has LFS files
func hasLFSFiles(repoPath string) bool {
	// Check for .gitattributes file
	gitattributesPath := filepath.Join(repoPath, ".gitattributes")
	if data, err := os.ReadFile(gitattributesPath); err == nil {
		// Look for LFS patterns
		return strings.Contains(string(data), "filter=lfs")
	}
	return false
}

// getGitRevision gets the current git commit hash
func getGitRevision(repoPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(string(output)), nil
}

// generateManifestFromRepo generates a manifest from a cloned repository
func generateManifestFromRepo(repoPath, modelName string) (*types.ModelManifest, error) {
	manifest := &types.ModelManifest{
		Name:        modelName,
		Version:     mirrorBranch,
		Description: fmt.Sprintf("Model mirrored from HuggingFace: %s", modelName),
		CreatedAt:   time.Now(),
		ModelType:   "unknown",
		License:     "See repository for license information",
	}
	
	// Try to load HuggingFace config
	configPath := filepath.Join(repoPath, "config.json")
	if configData, err := os.ReadFile(configPath); err == nil {
		var hfConfig types.HFConfig
		if err := json.Unmarshal(configData, &hfConfig); err == nil {
			// Extract model information
			if hfConfig.ModelType != "" {
				manifest.ModelType = hfConfig.ModelType
			}
			if len(hfConfig.Architectures) > 0 {
				manifest.Architecture = hfConfig.Architectures[0]
			}
			if hfConfig.NumParameters > 0 {
				manifest.Parameters = hfConfig.NumParameters
			}
			
			// Set inference hints
			manifest.InferenceHints = types.InferenceHints{
				ContextLength: hfConfig.MaxPositionEmbeddings,
			}
			
			// Estimate RAM requirements
			if manifest.Parameters > 0 {
				minRAMGB := (manifest.Parameters * 2) / (1024 * 1024 * 1024)
				manifest.InferenceHints.MinRAM = minRAMGB + 2
				manifest.InferenceHints.MinVRAM = minRAMGB
			}
		}
	}
	
	// Try to read README for description
	readmePath := filepath.Join(repoPath, "README.md")
	if readmeData, err := os.ReadFile(readmePath); err == nil {
		// Extract first paragraph as description
		lines := strings.Split(string(readmeData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				manifest.Description = line
				if len(manifest.Description) > 200 {
					manifest.Description = manifest.Description[:197] + "..."
				}
				break
			}
		}
	}
	
	// Scan files in the repository
	var totalSize int64
	var files []types.ModelFile
	
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		
		// Skip git files
		if strings.Contains(path, ".git/") {
			return nil
		}
		
		relPath, _ := filepath.Rel(repoPath, path)
		relPath = filepath.ToSlash(relPath)
		
		// Skip hidden files except important ones
		if strings.HasPrefix(filepath.Base(relPath), ".") && 
		   !strings.HasPrefix(filepath.Base(relPath), ".gitattributes") {
			return nil
		}
		
		files = append(files, types.ModelFile{
			Path:   relPath,
			Size:   info.Size(),
			SHA256: "", // Will be calculated later if needed
		})
		
		totalSize += info.Size()
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	
	manifest.Files = files
	manifest.TotalSize = totalSize
	
	return manifest, nil
}

// broadcastModel broadcasts a model on the DHT network
func broadcastModel(manifest *types.ModelManifest) error {
	// Initialize DHT discovery
	paths, err := storage.NewPaths()
	if err != nil {
		return err
	}
	
	dht, err := federation.NewDHTDiscovery(paths.BaseDir(), 0)
	if err != nil {
		return fmt.Errorf("failed to initialize DHT: %w", err)
	}
	defer dht.Close()
	
	// Bootstrap DHT
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}
	
	// Announce the model
	if err := dht.AnnounceModel(manifest); err != nil {
		return fmt.Errorf("failed to announce model: %w", err)
	}
	
	return nil
}