package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/internal/torrent"
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
	
	// Rescan registry to ensure the model is found
	if err := registry.Rescan(); err != nil {
		fmt.Printf("Warning: Failed to rescan registry: %v\n", err)
	}
	
	fmt.Printf("Model registered: %s (version: %s)\n", manifest.Name, manifest.Version)
	fmt.Printf("Total size: %.2f GB\n", float64(manifest.TotalSize)/(1024*1024*1024))
	fmt.Printf("Files: %d\n", len(manifest.Files))
	
	// Create torrent for the model
	fmt.Println("Creating torrent for P2P distribution...")
	torrentPath, magnetURI, err := createModelTorrent(modelPath, modelName)
	if err != nil {
		fmt.Printf("Error: Failed to create torrent: %v\n", err)
		fmt.Println("Model was cloned but torrent creation failed.")
		fmt.Println("You can try creating a torrent manually with 'silmaril publish'")
		return nil
	}
	
	fmt.Printf("Torrent created: %s\n", torrentPath)
	
	// Update manifest with magnet URI
	manifest.MagnetURI = magnetURI
	if err := registry.SaveManifest(manifest); err != nil {
		fmt.Printf("Warning: Failed to update manifest with magnet URI: %v\n", err)
	}
	
	// Force registry rescan to ensure the updated manifest is loaded
	if err := registry.Rescan(); err != nil {
		fmt.Printf("Warning: Failed to rescan registry: %v\n", err)
	}
	
	// Broadcast on DHT if not disabled (but don't start long-running seeding here)
	if !noBroadcast {
		fmt.Println("Broadcasting model on DHT network...")
		if err := broadcastModelQuick(manifest); err != nil {
			fmt.Printf("Warning: Failed to broadcast on DHT: %v\n", err)
		} else {
			fmt.Println("Model announced to DHT network")
		}
	}
	
	fmt.Printf("\n✅ Model successfully mirrored!\n")
	fmt.Printf("Model location: %s\n", modelPath)
	if magnetURI != "" {
		fmt.Printf("Magnet URI: %s\n", magnetURI)
	}
	
	// Auto-share if not disabled
	if !noAutoShare && torrentPath != "" {
		fmt.Println("\nStarting to share the model...")
		if err := startSharingModel(torrentPath, paths); err != nil {
			fmt.Printf("Warning: Failed to start sharing: %v\n", err)
			fmt.Println("You can manually share the model with:")
			fmt.Printf("  silmaril share %s\n", modelName)
		} else {
			fmt.Println("✅ Model is now being shared on the P2P network!")
		}
	}
	
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

// createModelTorrent creates a torrent file for the model
func createModelTorrent(modelPath, modelName string) (torrentPath, magnetURI string, err error) {
	// First, create a temporary directory with just the model files (no .git)
	tempDir := filepath.Join(os.TempDir(), "silmaril-torrent-"+strings.ReplaceAll(modelName, "/", "_"))
	defer os.RemoveAll(tempDir) // Clean up temp dir
	
	// Create the temp directory
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	
	// Copy all non-.git files to temp directory
	modelFiles := 0
	err = filepath.Walk(modelPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		relPath, _ := filepath.Rel(modelPath, path)
		
		// Skip .git directory and its contents
		if strings.HasPrefix(relPath, ".git") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Skip the directory itself
		if path == modelPath {
			return nil
		}
		
		targetPath := filepath.Join(tempDir, relPath)
		
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		
		// Create hard link to avoid copying data
		if err := os.Link(path, targetPath); err != nil {
			// If hard link fails, fall back to copy
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			
			dst, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			defer dst.Close()
			
			if _, err := io.Copy(dst, src); err != nil {
				return err
			}
		}
		
		modelFiles++
		return nil
	})
	
	if err != nil {
		return "", "", fmt.Errorf("failed to prepare files for torrent: %w", err)
	}
	
	fmt.Printf("  Prepared %d files for torrent (excluding .git)\n", modelFiles)
	
	// Create torrent info from temp directory
	info := metainfo.Info{
		PieceLength: 4 * 1024 * 1024, // 4MB pieces
		Name:        filepath.Base(modelPath),
	}
	
	// Build from the clean temp directory
	err = info.BuildFromFilePath(tempDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to build torrent info: %w", err)
	}
	
	// Create metainfo
	mi := metainfo.MetaInfo{
		CreatedBy:    "Silmaril Mirror",
		CreationDate: time.Now().Unix(),
		Comment:      fmt.Sprintf("Model: %s", modelName),
	}
	
	mi.InfoBytes, err = bencode.Marshal(info)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal torrent info: %w", err)
	}
	
	// Generate magnet URI
	magnetURI = mi.Magnet(nil, &info).String()
	
	// Save torrent file
	paths, err := storage.NewPaths()
	if err != nil {
		return "", "", err
	}
	
	torrentDir := paths.TorrentsDir()
	if err := os.MkdirAll(torrentDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create torrents directory: %w", err)
	}
	
	torrentPath = filepath.Join(torrentDir, strings.ReplaceAll(modelName, "/", "_")+".torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create torrent file: %w", err)
	}
	defer f.Close()
	
	if err := mi.Write(f); err != nil {
		return "", "", fmt.Errorf("failed to write torrent file: %w", err)
	}
	
	return torrentPath, magnetURI, nil
}

// broadcastModelQuick announces a model to DHT without long-running seeding
func broadcastModelQuick(manifest *types.ModelManifest) error {
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
	
	// Bootstrap DHT with shorter timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Try to bootstrap but don't fail if it takes too long
	if err := dht.Bootstrap(ctx); err != nil {
		// Continue anyway, we might still be able to announce
		fmt.Printf("Note: DHT bootstrap incomplete, announcement might be limited\n")
	}
	
	// Announce the model
	if err := dht.AnnounceModel(manifest); err != nil {
		return fmt.Errorf("failed to announce model: %w", err)
	}
	
	return nil
}

// startSharingModel starts sharing a model in the background
func startSharingModel(torrentPath string, paths *storage.Paths) error {
	// Create torrent client
	cfg := torrent.Config{
		DataDir:        paths.ModelsDir(),
		MaxConnections: 100,
		SeedRatio:      0, // Seed indefinitely
	}
	
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create torrent client: %w", err)
	}
	
	// Add torrent to client
	_, err = client.AddTorrentFile(torrentPath)
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to add torrent: %w", err)
	}
	
	// Start sharing in background goroutine
	go func() {
		// Keep client alive and seeding
		// This will continue until the process exits
		time.Sleep(365 * 24 * time.Hour)
		client.Close()
	}()
	
	return nil
}