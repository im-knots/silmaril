package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/signing"
	"github.com/silmaril/silmaril/internal/storage"
)

var publishCmd = &cobra.Command{
	Use:   "publish [model-directory]",
	Short: "Publish a model to the P2P network",
	Long: `Creates a torrent from a HuggingFace format model directory and generates
a manifest for distribution via the Silmaril network.`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

var (
	modelName     string
	modelVersion  string
	modelLicense  string
	pieceLength   int64
	announceURL   string
	skipDHT       bool
	signManifest  bool
	ipfsCIDs      string
)

func init() {
	rootCmd.AddCommand(publishCmd)
	
	publishCmd.Flags().StringVar(&modelName, "name", "", "model name (e.g., org/model-name)")
	publishCmd.Flags().StringVar(&modelVersion, "version", "main", "model version/revision")
	publishCmd.Flags().StringVar(&modelLicense, "license", "", "model license")
	publishCmd.Flags().Int64Var(&pieceLength, "piece-length", 4*1024*1024, "piece length for torrent (default 4MB)")
	publishCmd.Flags().StringVar(&announceURL, "announce", "", "optional announce URL (DHT preferred)")
	publishCmd.Flags().BoolVar(&skipDHT, "skip-dht", false, "skip DHT announcement")
	publishCmd.Flags().BoolVar(&signManifest, "sign", true, "sign the manifest")
	publishCmd.Flags().StringVar(&ipfsCIDs, "ipfs-cids", "", "comma-separated list of filename:CID pairs for IPFS")
	
	publishCmd.MarkFlagRequired("name")
	publishCmd.MarkFlagRequired("license")
}

type HFConfig struct {
	ModelType       string                 `json:"model_type"`
	Architectures   []string              `json:"architectures"`
	NumParameters   int64                 `json:"num_parameters"`
	HiddenSize      int                   `json:"hidden_size"`
	NumHiddenLayers int                   `json:"num_hidden_layers"`
	NumAttentionHeads int                 `json:"num_attention_heads"`
	MaxPositionEmbeddings int              `json:"max_position_embeddings"`
	Quantization    map[string]interface{} `json:"quantization_config"`
}

func runPublish(cmd *cobra.Command, args []string) error {
	modelDir := args[0]
	
	// Verify directory exists
	info, err := os.Stat(modelDir)
	if err != nil {
		return fmt.Errorf("cannot access model directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", modelDir)
	}
	
	fmt.Printf("Publishing model from: %s\n", modelDir)
	fmt.Printf("Model name: %s\n", modelName)
	fmt.Printf("Version: %s\n", modelVersion)
	fmt.Printf("License: %s\n", modelLicense)
	fmt.Println()
	
	// Parse config.json if it exists
	var hfConfig HFConfig
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
	var files []models.ModelFile
	var totalSize int64
	
	err = filepath.Walk(modelDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		if info.IsDir() {
			return nil
		}
		
		// Skip hidden files and non-model files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		
		relPath, err := filepath.Rel(modelDir, path)
		if err != nil {
			return err
		}
		
		fmt.Printf("Processing: %s\n", relPath)
		
		// Calculate SHA256
		hash, err := calculateFileSHA256(path)
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", relPath, err)
		}
		
		files = append(files, models.ModelFile{
			Path:   relPath,
			Size:   info.Size(),
			SHA256: hash,
		})
		
		totalSize += info.Size()
		
		return nil
	})
	
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
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
		return fmt.Errorf("failed to build torrent info: %w", err)
	}
	
	// Create v2 torrent if supported
	mi := metainfo.MetaInfo{
		InfoBytes: bencode.MustMarshal(torrentInfo),
	}
	
	// Enable v2 features
	mi.CreationDate = time.Now().Unix()
	mi.CreatedBy = "Silmaril P2P Model Client"
	
	// Add DHT nodes for bootstrap
	// Note: The Node type has changed in newer versions
	// We'll skip this for now as it's optional
	
	// Optionally add announce URL
	if announceURL != "" {
		mi.Announce = announceURL
	}
	
	// Generate magnet links
	magnetURI := mi.Magnet(nil, &torrentInfo).String()
	
	// Parse IPFS CIDs if provided
	ipfsMap := make(map[string]string)
	if ipfsCIDs != "" {
		pairs := strings.Split(ipfsCIDs, ",")
		for _, pair := range pairs {
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) == 2 {
				ipfsMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	
	// Create manifest
	manifest := models.ModelManifest{
		Name:         modelName,
		Version:      modelVersion,
		Description:  fmt.Sprintf("Model published via Silmaril"),
		License:      modelLicense,
		CreatedAt:    time.Now(),
		Architecture: architecture,
		ModelType:    hfConfig.ModelType,
		Parameters:   parameters,
		InferenceHints: models.InferenceHints{
			MinRAM:        totalSize / (1024 * 1024 * 1024) * 2, // Rough estimate
			ContextLength: hfConfig.MaxPositionEmbeddings,
		},
		TotalSize:   totalSize,
		Files:       files,
		MagnetURI:   magnetURI, // This will be v2
		IPFSCIDs:    ipfsMap,
	}
	
	// Sign manifest if requested
	if signManifest {
		keyPair, err := signing.GetOrCreateKeys()
		if err != nil {
			fmt.Printf("Warning: Failed to get signing keys: %v\n", err)
		} else {
			err = signing.SignManifest(&manifest, keyPair.PrivateKey)
			if err != nil {
				fmt.Printf("Warning: Failed to sign manifest: %v\n", err)
			} else {
				fmt.Println("✅ Manifest signed successfully")
			}
		}
	}
	
	// Save manifest to file
	manifestPath := filepath.Join(modelDir, "silmaril-manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	
	err = os.WriteFile(manifestPath, manifestData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	
	// Save torrent file
	torrentPath := filepath.Join(modelDir, "model.torrent")
	torrentFile, err := os.Create(torrentPath)
	if err != nil {
		return fmt.Errorf("failed to create torrent file: %w", err)
	}
	defer torrentFile.Close()
	
	err = mi.Write(torrentFile)
	if err != nil {
		return fmt.Errorf("failed to write torrent file: %w", err)
	}
	
	fmt.Println("\n✅ Model published successfully!")
	fmt.Printf("Manifest saved to: %s\n", manifestPath)
	fmt.Printf("Torrent saved to: %s\n", torrentPath)
	fmt.Printf("\nMagnet link:\n%s\n", magnetURI)
	
	if len(ipfsMap) > 0 {
		fmt.Printf("\nIPFS CIDs included: %d files\n", len(ipfsMap))
	}
	
	// Initialize paths
	paths, err := storage.NewPaths()
	if err != nil {
		fmt.Printf("Warning: Failed to initialize paths: %v\n", err)
	}
	
	// Save to local registry
	registry, err := models.NewRegistry(paths)
	if err != nil {
		fmt.Printf("Warning: Failed to create registry: %v\n", err)
	} else {
		err = registry.SaveManifest(&manifest)
		if err != nil {
			fmt.Printf("Warning: Failed to save to registry: %v\n", err)
		} else {
			fmt.Println("✅ Model saved to local registry!")
		}
	}
	
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
				err = dht.AnnounceModel(&manifest)
				if err != nil {
					fmt.Printf("Warning: Failed to announce to DHT: %v\n", err)
				} else {
					fmt.Println("✅ Model announced to DHT network!")
				}
			}
		}
	}
	
	// Show sharing instructions
	fmt.Println("\nTo share this model with the community:")
	fmt.Printf("1. Upload %s to a public URL (GitHub, IPFS, etc)\n", manifestPath)
	fmt.Println("2. Share the URL so others can import with:")
	fmt.Println("   silmaril discover <manifest-url>")
	
	fmt.Println("\nTo start seeding, run:")
	fmt.Printf("  silmaril share %s\n", torrentPath)
	
	return nil
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