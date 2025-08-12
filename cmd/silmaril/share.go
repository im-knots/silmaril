package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	anacrolixtorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/signing"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/internal/torrent"
	"github.com/silmaril/silmaril/pkg/types"
)

var shareCmd = &cobra.Command{
	Use:   "share [model-name, path, or directory]",
	Short: "Share/publish models to the P2P network",
	Long: `Share models by seeding them to the P2P network, or publish a new model
by creating a torrent and manifest from a directory.

You can specify:
  - A model name from registry (e.g., "meta-llama/Llama-3.1-8B")
  - A path to a torrent file to seed
  - A path to a model directory to publish and seed (creates torrent if needed)
  - Use --all to seed all downloaded models

Examples:
  silmaril share --all                          # Seed all models
  silmaril share meta-llama/Llama-3.1-8B        # Seed specific model
  silmaril share /path/to/model.torrent         # Seed from torrent file
  silmaril share /path/to/model/dir --name org/model --license apache-2.0  # Publish new model`,
	RunE: runShare,
}

var (
	shareAll      bool
	modelName     string
	modelVersion  string
	modelLicense  string
	pieceLength   int64
	skipDHT       bool
	signManifest  bool
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
}

func runShare(cmd *cobra.Command, args []string) error {
	// Initialize paths and registry
	paths, err := storage.NewPaths()
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	registry, err := models.NewRegistry(paths)
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}
	
	// Rescan to ensure we have the latest models
	if err := registry.Rescan(); err != nil {
		fmt.Printf("Warning: Failed to rescan registry: %v\n", err)
	}
	
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
	defer client.Close()
	
	var torrentsToSeed []string
	
	if shareAll {
		// Seed all models in the registry
		fmt.Println("Seeding all downloaded models...")
		
		models := registry.ListModels()
		if len(models) == 0 {
			fmt.Println("No models found in registry.")
			fmt.Println("Use 'silmaril get' or 'silmaril mirror' to download models first.")
			return nil
		}
		
		for _, modelName := range models {
			torrentPath := filepath.Join(paths.TorrentsDir(), strings.ReplaceAll(modelName, "/", "_")+".torrent")
			if _, err := os.Stat(torrentPath); err == nil {
				torrentsToSeed = append(torrentsToSeed, torrentPath)
				fmt.Printf("Found torrent for: %s\n", modelName)
			} else {
				// Try to find torrent in model directory
				modelPath := paths.ModelPath(modelName)
				modelTorrent := filepath.Join(modelPath, "model.torrent")
				if _, err := os.Stat(modelTorrent); err == nil {
					torrentsToSeed = append(torrentsToSeed, modelTorrent)
					fmt.Printf("Found torrent for: %s\n", modelName)
				}
			}
		}
	} else if len(args) > 0 {
		input := args[0]
		
		// Try as model name first if it contains /
		if strings.Contains(input, "/") {
			manifest, err := registry.GetManifest(input)
			if err == nil {
				// Found in registry
				fmt.Printf("Seeding model: %s\n", input)
				
				// Look for torrent file
				torrentPath := filepath.Join(paths.TorrentsDir(), strings.ReplaceAll(input, "/", "_")+".torrent")
				if _, err := os.Stat(torrentPath); err == nil {
					torrentsToSeed = append(torrentsToSeed, torrentPath)
					fmt.Printf("Found torrent file: %s\n", torrentPath)
				} else {
					// Try model directory
					modelPath := paths.ModelPath(input)
					modelTorrent := filepath.Join(modelPath, "model.torrent")
					if _, err := os.Stat(modelTorrent); err == nil {
						torrentsToSeed = append(torrentsToSeed, modelTorrent)
						fmt.Printf("Found torrent in model directory: %s\n", modelTorrent)
					} else if manifest.MagnetURI != "" {
						// Use magnet URI if available
						fmt.Printf("No torrent file found, using magnet URI\n")
						fmt.Printf("Magnet: %s\n", manifest.MagnetURI)
						dl, err := client.AddMagnet(manifest.MagnetURI)
						if err != nil {
							return fmt.Errorf("failed to add magnet: %w", err)
						}
						// For magnet links, we can still monitor
						fmt.Printf("  ✓ Added via magnet: %s\n", dl.Torrent.InfoHash().HexString())
						
						// Jump to monitoring without going through file-based torrent loading
						// Set up signal handling for graceful shutdown
						sigChan := make(chan os.Signal, 1)
						signal.Notify(sigChan, os.Interrupt)
						
						// Monitor seeding
						ticker := time.NewTicker(10 * time.Second)
						defer ticker.Stop()
						
						fmt.Println("\nSeeding models... Press Ctrl+C to stop")
						fmt.Println()
						
						for {
							select {
							case <-ticker.C:
								// Print seeding statistics
								var totalUploaded int64
								var activeTorrents int
								var totalPeers int
								
								// Get stats from all torrents
								allTorrents := client.GetTorrents()
								for _, t := range allTorrents {
									if t.Info() != nil {
										activeTorrents++
										stats := t.Stats()
										totalUploaded += stats.BytesWrittenData.Int64()
										totalPeers += len(t.PeerConns())
									}
								}
								
								fmt.Printf("\rActive torrents: %d | Peers: %d | Total uploaded: %.2f GB",
									activeTorrents, totalPeers, float64(totalUploaded)/(1024*1024*1024))
								
							case <-sigChan:
								fmt.Println("\n\nShutting down...")
								return nil
							}
						}
						// Function returns in the loop above, so no need for explicit return
					} else {
						// Debug: show what we checked
						fmt.Printf("Debug: Checked for torrent at: %s (not found)\n", torrentPath)
						fmt.Printf("Debug: Checked for torrent at: %s (not found)\n", modelTorrent)
						fmt.Printf("Debug: Manifest has magnet URI: %v\n", manifest.MagnetURI != "")
						return fmt.Errorf("no torrent file or magnet URI found for model: %s", input)
					}
				}
				goto addTorrents
			}
		}
		
		// Not a model name or not in registry, check as path
		info, err := os.Stat(input)
		if err != nil {
			return fmt.Errorf("'%s' is not a valid model name or path: %w", input, err)
		}
		
		if info.IsDir() {
			// Look for existing torrent file in directory
			torrentPath := filepath.Join(input, "model.torrent")
			if _, err := os.Stat(torrentPath); err == nil {
				torrentsToSeed = append(torrentsToSeed, torrentPath)
				fmt.Printf("Found torrent in directory: %s\n", torrentPath)
			} else {
				// Check if it's in torrents directory
				baseName := filepath.Base(input)
				torrentPath = filepath.Join(paths.TorrentsDir(), baseName+".torrent")
				if _, err := os.Stat(torrentPath); err == nil {
					torrentsToSeed = append(torrentsToSeed, torrentPath)
				} else {
					// No torrent exists - create one if --name and --license provided
					if modelName != "" && modelLicense != "" {
						fmt.Printf("Publishing new model from directory: %s\n", input)
						torrentPath, err := publishModelFromDirectory(input, paths, registry)
						if err != nil {
							return fmt.Errorf("failed to publish model: %w", err)
						}
						torrentsToSeed = append(torrentsToSeed, torrentPath)
					} else {
						fmt.Println("No torrent file found for directory.")
						fmt.Println("To create and share a new model, provide --name and --license:")
						fmt.Printf("  silmaril share %s --name org/model --license apache-2.0\n", input)
						return nil
					}
				}
			}
		} else if filepath.Ext(input) == ".torrent" {
			// Direct torrent file
			torrentsToSeed = append(torrentsToSeed, input)
		} else {
			return fmt.Errorf("invalid input: must be a model name, directory, or torrent file")
		}
	} else {
		// No arguments and not --all
		fmt.Println("Please specify a model name or use --all to seed all models")
		fmt.Println("\nExamples:")
		fmt.Println("  silmaril share --all")
		fmt.Println("  silmaril share meta-llama/Llama-3.1-8B")
		fmt.Println("  silmaril share /path/to/model.torrent")
		return nil
	}
	
addTorrents:
	// Add all torrents to client for seeding
	var addedTorrents []*anacrolixtorrent.Torrent
	for _, torrentPath := range torrentsToSeed {
		fmt.Printf("Loading torrent: %s\n", filepath.Base(torrentPath))
		t, err := client.AddTorrentForSeeding(torrentPath)
		if err != nil {
			fmt.Printf("Warning: Failed to add %s: %v\n", torrentPath, err)
			continue
		}
		addedTorrents = append(addedTorrents, t)
		fmt.Printf("  ✓ Added: %s (Info Hash: %s)\n", t.Name(), t.InfoHash().HexString())
	}
	
	if len(addedTorrents) == 0 {
		return fmt.Errorf("no torrents were successfully added")
	}
	
	// Torrents will be automatically announced to DHT by the client
	fmt.Println("\nTorrents loaded and announcing to DHT network...")
	
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	// Monitor seeding
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	fmt.Println("\nSeeding models... Press Ctrl+C to stop")
	fmt.Println()
	
	for {
		select {
		case <-ticker.C:
			// Print seeding statistics
			var totalUploaded int64
			var activeTorrents int
			var totalPeers int
			
			// Get stats from all torrents
			allTorrents := client.GetTorrents()
			for _, t := range allTorrents {
				if t.Info() != nil {
					activeTorrents++
					stats := t.Stats()
					totalUploaded += stats.BytesWrittenData.Int64()
					totalPeers += len(t.PeerConns())
				}
			}
			
			fmt.Printf("\rActive torrents: %d | Peers: %d | Total uploaded: %.2f GB",
				activeTorrents, totalPeers, float64(totalUploaded)/(1024*1024*1024))
			
		case <-sigChan:
			fmt.Println("\n\nShutting down...")
			return nil
		}
	}
}

// publishModelFromDirectory creates a torrent and manifest for a model directory
func publishModelFromDirectory(modelDir string, paths *storage.Paths, registry *models.Registry) (string, error) {
	// Parse config.json if it exists
	var hfConfig types.HFConfig
	var architecture string
	var parameters int64
	
	configPath := filepath.Join(modelDir, "config.json")
	if configData, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(configData, &hfConfig)
		if len(hfConfig.Architectures) > 0 {
			architecture = hfConfig.Architectures[0]
		}
		// Estimate parameters if not provided
		if hfConfig.NumParameters > 0 {
			parameters = hfConfig.NumParameters
		} else if hfConfig.HiddenSize > 0 && hfConfig.NumHiddenLayers > 0 {
			// Rough estimation for transformer models
			parameters = int64(hfConfig.HiddenSize) * int64(hfConfig.NumHiddenLayers) * 12 * 1_000_000
		}
	}
	
	// Scan directory and calculate SHA256 for each file
	var files []types.ModelFile
	var totalSize int64
	
	err := filepath.Walk(modelDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if info.IsDir() {
			return nil
		}
		
		// Skip hidden files and .git
		relPath, _ := filepath.Rel(modelDir, path)
		if strings.HasPrefix(relPath, ".git") || strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		
		fmt.Printf("Processing: %s\n", relPath)
		
		// Calculate SHA256
		hash, err := calculateFileSHA256(path)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", relPath, err)
		}
		
		files = append(files, types.ModelFile{
			Path:   relPath,
			Size:   info.Size(),
			SHA256: hash,
		})
		
		totalSize += info.Size()
		
		return nil
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to scan directory: %w", err)
	}
	
	fmt.Printf("\nTotal files: %d\n", len(files))
	fmt.Printf("Total size: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	
	// Create torrent
	fmt.Println("\nCreating torrent...")
	torrentInfo := metainfo.Info{
		PieceLength: pieceLength,
	}
	
	err = torrentInfo.BuildFromFilePath(modelDir)
	if err != nil {
		return "", fmt.Errorf("failed to build torrent info: %w", err)
	}
	
	// Create metainfo
	mi := metainfo.MetaInfo{
		CreationDate: time.Now().Unix(),
		CreatedBy:    "Silmaril P2P Model Client",
		Comment:      fmt.Sprintf("Model: %s", modelName),
	}
	
	mi.InfoBytes, err = bencode.Marshal(torrentInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal torrent info: %w", err)
	}
	
	// Generate magnet URI
	magnetURI := mi.Magnet(nil, &torrentInfo).String()
	
	// Create manifest
	manifest := &types.ModelManifest{
		Name:         modelName,
		Version:      modelVersion,
		Description:  fmt.Sprintf("Model published via Silmaril"),
		License:      modelLicense,
		CreatedAt:    time.Now(),
		Architecture: architecture,
		ModelType:    hfConfig.ModelType,
		Parameters:   parameters,
		InferenceHints: types.InferenceHints{
			MinRAM:        totalSize / (1024 * 1024 * 1024) * 2, // Rough estimate
			ContextLength: hfConfig.MaxPositionEmbeddings,
		},
		TotalSize: totalSize,
		Files:     files,
		MagnetURI: magnetURI,
	}
	
	// Sign manifest if requested
	if signManifest {
		keyPair, err := signing.GetOrCreateKeys()
		if err != nil {
			fmt.Printf("Warning: Failed to get signing keys: %v\n", err)
		} else {
			err = signing.SignManifest(manifest, keyPair.PrivateKey)
			if err != nil {
				fmt.Printf("Warning: Failed to sign manifest: %v\n", err)
			} else {
				fmt.Println("✅ Manifest signed successfully")
			}
		}
	}
	
	// Save manifest to registry
	err = registry.SaveManifest(manifest)
	if err != nil {
		fmt.Printf("Warning: Failed to save manifest: %v\n", err)
	}
	
	// Save torrent file
	torrentDir := paths.TorrentsDir()
	if err := os.MkdirAll(torrentDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create torrents directory: %w", err)
	}
	
	torrentPath := filepath.Join(torrentDir, strings.ReplaceAll(modelName, "/", "_")+".torrent")
	torrentFile, err := os.Create(torrentPath)
	if err != nil {
		return "", fmt.Errorf("failed to create torrent file: %w", err)
	}
	defer torrentFile.Close()
	
	err = mi.Write(torrentFile)
	if err != nil {
		return "", fmt.Errorf("failed to write torrent file: %w", err)
	}
	
	fmt.Println("\n✅ Model published successfully!")
	fmt.Printf("Torrent saved to: %s\n", torrentPath)
	fmt.Printf("Magnet link:\n%s\n", magnetURI)
	
	// Announce to DHT
	if !skipDHT {
		fmt.Println("\nAnnouncing to DHT network...")
		dht, err := federation.NewDHTDiscovery(paths.BaseDir(), 0)
		if err != nil {
			fmt.Printf("Warning: Failed to create DHT client: %v\n", err)
		} else {
			defer dht.Close()
			
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err = dht.Bootstrap(ctx)
			if err != nil {
				fmt.Printf("Warning: DHT bootstrap failed: %v\n", err)
			} else {
				err = dht.AnnounceModel(manifest)
				if err != nil {
					fmt.Printf("Warning: Failed to announce to DHT: %v\n", err)
				} else {
					fmt.Println("✅ Model announced to DHT network!")
				}
			}
		}
	}
	
	return torrentPath, nil
}

func calculateFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	
	return hex.EncodeToString(hasher.Sum(nil)), nil
}