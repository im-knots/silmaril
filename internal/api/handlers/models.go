package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/internal/torrent"
	"github.com/silmaril/silmaril/pkg/types"
)

// ListModels returns all local models
func (h *Handlers) ListModels(c *gin.Context) {
	paths, err := storage.NewPaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to initialize paths: %v", err),
		})
		return
	}
	
	registry, err := models.NewRegistry(paths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to create registry: %v", err),
		})
		return
	}
	
	if err := registry.ScanModels(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to scan models: %v", err),
		})
		return
	}
	
	// Get model names
	modelNames := registry.ListModels()
	
	// Convert to model details
	var modelDetails []map[string]interface{}
	for _, name := range modelNames {
		manifest, err := registry.GetManifest(name)
		if err != nil {
			// Skip models we can't load
			continue
		}
		
		// Convert manifest to map for API response
		modelMap := map[string]interface{}{
			"name":        manifest.Name,
			"version":     manifest.Version,
			"description": manifest.Description,
			"model_type":  manifest.ModelType,
			"license":     manifest.License,
		}
		
		// Add optional fields if present
		if manifest.Architecture != "" {
			modelMap["architecture"] = manifest.Architecture
		}
		if manifest.Parameters > 0 {
			modelMap["parameters"] = manifest.Parameters
		}
		if manifest.TotalSize > 0 {
			modelMap["total_size"] = manifest.TotalSize
		}
		if manifest.MagnetURI != "" {
			modelMap["magnet_uri"] = manifest.MagnetURI
		}
		// InferenceHints is a struct, not a pointer, so just add it directly
		modelMap["inference_hints"] = manifest.InferenceHints
		
		modelDetails = append(modelDetails, modelMap)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"models": modelDetails,
		"count":  len(modelDetails),
	})
}

// GetModel returns details about a specific model
func (h *Handlers) GetModel(c *gin.Context) {
	modelName := c.Param("name")
	
	paths, err := storage.NewPaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to initialize paths: %v", err),
		})
		return
	}
	
	registry, err := models.NewRegistry(paths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to create registry: %v", err),
		})
		return
	}
	
	if err := registry.ScanModels(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to scan models: %v", err),
		})
		return
	}
	
	manifest, err := registry.GetManifest(modelName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("model %s not found", modelName),
		})
		return
	}
	
	c.JSON(http.StatusOK, manifest)
}

// DownloadModelRequest represents a download request
type DownloadModelRequest struct {
	ModelName string `json:"model_name" binding:"required"`
	InfoHash  string `json:"info_hash"`
	Seed      bool   `json:"seed"`
}

// DownloadModel starts downloading a model
func (h *Handlers) DownloadModel(c *gin.Context) {
	var req DownloadModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid request: %v", err),
		})
		return
	}
	
	// Create transfer
	tm := h.daemon.GetTransferManager()
	transfer := tm.CreateDownload(req.ModelName, req.InfoHash, 0)
	
	// Start download
	torrentPath := filepath.Join(storage.GetTorrentsDir(), req.InfoHash+".torrent")
	mt, err := h.daemon.GetTorrentManager().AddTorrent(torrentPath, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to start download: %v", err),
		})
		return
	}
	
	// Update transfer with torrent info
	transfer.InfoHash = mt.InfoHash
	transfer.TotalBytes = mt.Torrent.Length()
	transfer.Status = "active"
	
	c.JSON(http.StatusOK, gin.H{
		"transfer_id": transfer.ID,
		"model_name":  req.ModelName,
		"info_hash":   mt.InfoHash,
		"message":     "download started",
	})
}

// ShareModelRequest represents a share request
type ShareModelRequest struct {
	ModelName    string `json:"model_name"`
	Path         string `json:"path"`
	All          bool   `json:"all"`
	// Publishing parameters (when sharing from directory)
	Name         string `json:"name"`         // Model name for new models
	License      string `json:"license"`      // License for new models
	Version      string `json:"version"`      // Version for new models
	PieceLength  int64  `json:"piece_length"` // Piece length for torrent
	SkipDHT      bool   `json:"skip_dht"`      // Skip DHT announcement
	SignManifest bool   `json:"sign_manifest"` // Sign the manifest
	// Repository cloning parameters
	RepoURL      string `json:"repo_url"`      // Git/HF repository URL
	Branch       string `json:"branch"`        // Git branch
	Depth        int    `json:"depth"`         // Git clone depth
	SkipLFS      bool   `json:"skip_lfs"`      // Skip Git LFS files
}

// ShareModel starts sharing a model
func (h *Handlers) ShareModel(c *gin.Context) {
	var req ShareModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid request: %v", err),
		})
		return
	}
	
	// Handle repository URL first (clone and share)
	if req.RepoURL != "" {
		// Set defaults for git operations
		if req.Branch == "" {
			req.Branch = "main"
		}
		if req.Depth == 0 {
			req.Depth = 1
		}
		
		// Parse repository URL to get model name
		modelName := parseRepoURL(req.RepoURL)
		if modelName == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid repository URL",
			})
			return
		}
		
		// Get storage paths
		paths, err := storage.NewPaths()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to initialize paths: %v", err),
			})
			return
		}
		
		// Determine clone destination
		modelPath := paths.ModelPath(modelName)
		
		// Check if model already exists
		if _, err := os.Stat(modelPath); err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("model %s already exists", modelName),
			})
			return
		}
		
		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create model directory: %v", err),
			})
			return
		}
		
		// Execute git clone in background
		go func() {
			fmt.Printf("[ShareModel] Cloning repository: %s to %s\n", req.RepoURL, modelPath)
			
			// Prepare clone options
			cloneOptions := &git.CloneOptions{
				URL:      req.RepoURL,
				Progress: os.Stdout,
			}
			
			// Set branch if not main/master
			if req.Branch != "" && req.Branch != "main" && req.Branch != "master" {
				cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(req.Branch)
			}
			
			// Set depth for shallow clone
			if req.Depth > 0 {
				cloneOptions.Depth = req.Depth
			}
			
			// Handle authentication for private repos (optional)
			// For HuggingFace, we might need token authentication
			if strings.Contains(req.RepoURL, "huggingface.co") {
				// Check for HF token in environment
				if token := os.Getenv("HF_TOKEN"); token != "" {
					cloneOptions.Auth = &githttp.BasicAuth{
						Username: "hf",
						Password: token,
					}
				}
			}
			
			// Clone the repository
			_, err := git.PlainClone(modelPath, false, cloneOptions)
			if err != nil {
				// Handle specific errors
				if err == transport.ErrAuthenticationRequired {
					fmt.Printf("[ShareModel] Authentication required for repository: %v\n", err)
				} else if err == transport.ErrRepositoryNotFound {
					fmt.Printf("[ShareModel] Repository not found: %v\n", err)
				} else {
					fmt.Printf("[ShareModel] Failed to clone repository: %v\n", err)
				}
				// Clean up partial clone
				os.RemoveAll(modelPath)
				return
			}
			
			fmt.Printf("[ShareModel] Repository cloned successfully to %s\n", modelPath)
			
			// Remove .git directory to save space
			gitDir := filepath.Join(modelPath, ".git")
			if err := os.RemoveAll(gitDir); err != nil {
				fmt.Printf("[ShareModel] Warning: failed to remove .git directory: %v\n", err)
			}
			
			// Create registry to generate manifest
			registry, err := models.NewRegistry(paths)
			if err != nil {
				fmt.Printf("[ShareModel] Failed to create registry: %v\n", err)
				return
			}
			
			// Generate manifest for the cloned model
			manifest := &types.ModelManifest{
				Name:    modelName,
				Version: req.Branch,
				License: "Unknown", // Will be detected from repo if possible
			}
			
			// Try to detect license from common files
			licenseFiles := []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "LICENCE.txt", "LICENCE.md"}
			for _, lf := range licenseFiles {
				if _, err := os.Stat(filepath.Join(modelPath, lf)); err == nil {
					// License file exists, could parse it to detect type
					manifest.License = "See LICENSE file"
					break
				}
			}
			
			// Calculate model size
			var totalSize int64
			filepath.Walk(modelPath, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					totalSize += info.Size()
				}
				return nil
			})
			manifest.TotalSize = totalSize
			
			// Save manifest
			if err := registry.SaveManifest(manifest); err != nil {
				fmt.Printf("[ShareModel] Failed to save manifest: %v\n", err)
				return
			}
			
			// Create torrent
			torrentPath := filepath.Join(paths.TorrentsDir(), modelName+".torrent")
			
			// Ensure torrents directory exists (including parent directories for nested model names)
			torrentDir := filepath.Dir(torrentPath)
			if err := os.MkdirAll(torrentDir, 0755); err != nil {
				fmt.Printf("[ShareModel] Failed to create torrents directory: %v\n", err)
				return
			}
			
			// Create the torrent file
			pieceLength := int64(4 * 1024 * 1024) // 4MB pieces
			if req.PieceLength > 0 {
				pieceLength = req.PieceLength
			}
			
			infoHash, err := torrent.CreateTorrentFromDirectory(modelPath, torrentPath, pieceLength)
			if err != nil {
				fmt.Printf("[ShareModel] Failed to create torrent: %v\n", err)
				return
			}
			
			fmt.Printf("[ShareModel] Torrent created: %s (InfoHash: %s)\n", torrentPath, infoHash)
			
			// Start sharing the model
			torrentManager := h.daemon.GetTorrentManager()
			managedTorrent, err := torrentManager.AddTorrent(torrentPath, modelName)
			if err != nil {
				fmt.Printf("[ShareModel] Failed to add torrent: %v\n", err)
				return
			}
			
			// Start seeding
			if err := torrentManager.StartSeeding(managedTorrent.InfoHash); err != nil {
				fmt.Printf("[ShareModel] Failed to start seeding: %v\n", err)
				return
			}
			
			fmt.Printf("[ShareModel] Started sharing model: %s\n", modelName)
			
			// Announce on DHT unless disabled
			if !req.SkipDHT {
				announcement := types.ModelAnnouncement{
					Name:     modelName,
					InfoHash: managedTorrent.InfoHash,
					Size:     totalSize,
				}
				h.daemon.GetDHTManager().AnnounceModel(&announcement)
				fmt.Printf("[ShareModel] Announced model on DHT: %s\n", modelName)
			}
		}()
		
		c.JSON(http.StatusAccepted, gin.H{
			"message": "share operation started",
			"model_name": modelName,
			"repo_url": req.RepoURL,
			"status": "cloning",
		})
		return
	}
	
	if req.All {
		// Share all models
		paths, err := storage.NewPaths()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to initialize paths: %v", err),
			})
			return
		}
		
		registry, err := models.NewRegistry(paths)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create registry: %v", err),
			})
			return
		}
		
		if err := registry.ScanModels(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to scan models: %v", err),
			})
			return
		}
		
		modelsList := registry.GetAllManifests()
		shared := 0
		var errors []string
		
		for _, manifest := range modelsList {
			// Look for the torrent file
			torrentPath := filepath.Join(paths.TorrentsDir(), manifest.Name+".torrent")
			if _, err := os.Stat(torrentPath); os.IsNotExist(err) {
				// Try without .torrent extension in case it's already included
				torrentPath = filepath.Join(paths.TorrentsDir(), manifest.Name)
				if _, err := os.Stat(torrentPath); os.IsNotExist(err) {
					errors = append(errors, fmt.Sprintf("%s: torrent file not found", manifest.Name))
					continue
				}
			}
			
			// Add torrent to torrent manager
			torrentManager := h.daemon.GetTorrentManager()
			managedTorrent, err := torrentManager.AddTorrent(torrentPath, filepath.Join(paths.ModelsDir(), manifest.Name))
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", manifest.Name, err))
				continue
			}
			
			// Mark as seeding
			managedTorrent.Seeding = true
			
			// Create seed transfer
			tm := h.daemon.GetTransferManager()
			transfer := tm.CreateSeed(manifest.Name, managedTorrent.InfoHash)
			transfer.Status = "active"
			
			// Announce to DHT if not skipping
			if !req.SkipDHT {
				announcement := &types.ModelAnnouncement{
					Name:     manifest.Name,
					InfoHash: managedTorrent.InfoHash,
					Size:     manifest.TotalSize,
				}
				h.daemon.GetDHTManager().AnnounceModel(announcement)
			}
			
			shared++
		}
		
		response := gin.H{
			"message":      "started sharing models",
			"models_shared": shared,
			"total_models": len(modelsList),
		}
		
		if len(errors) > 0 {
			response["warnings"] = errors
		}
		
		c.JSON(http.StatusOK, response)
		return
	}
	
	// Share specific model
	if req.ModelName != "" {
		paths, err := storage.NewPaths()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to initialize paths: %v", err),
			})
			return
		}
		
		registry, err := models.NewRegistry(paths)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create registry: %v", err),
			})
			return
		}
		
		if err := registry.ScanModels(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to scan models: %v", err),
			})
			return
		}
		
		manifest, err := registry.GetManifest(req.ModelName)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": fmt.Sprintf("model %s not found", req.ModelName),
			})
			return
		}
		
		// Create seed transfer
		tm := h.daemon.GetTransferManager()
		infoHash := manifest.Name // Use model name as identifier for now
		transfer := tm.CreateSeed(manifest.Name, infoHash)
		
		// Start seeding
		if err := h.daemon.GetTorrentManager().StartSeeding(infoHash); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to start sharing: %v", err),
			})
			return
		}
		
		transfer.Status = "active"
		
		// Announce to DHT
		announcement := &types.ModelAnnouncement{
			Name:     manifest.Name,
			InfoHash: infoHash,
			Size:     manifest.TotalSize,
		}
		h.daemon.GetDHTManager().AnnounceModel(announcement)
		
		c.JSON(http.StatusOK, gin.H{
			"message":     "started sharing model",
			"model_name":  manifest.Name,
			"info_hash":   infoHash,
			"transfer_id": transfer.ID,
		})
		return
	}
	
	// Share from path (publish new model from directory)
	if req.Path != "" {
		fmt.Printf("[ShareModel] Publishing model from directory: %s\n", req.Path)
		
		// For publishing new models, Name and License are required
		if req.Name == "" || req.License == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "name and license are required when publishing from directory",
			})
			return
		}
		fmt.Printf("[ShareModel] Model name: %s, License: %s, Version: %s\n", req.Name, req.License, req.Version)

		// Verify path exists and is a directory
		info, err := os.Stat(req.Path)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("path not found: %v", err),
			})
			return
		}
		if !info.IsDir() {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "path must be a directory",
			})
			return
		}

		// Get storage paths
		paths, err := storage.NewPaths()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to initialize paths: %v", err),
			})
			return
		}

		// Create registry to generate manifest
		registry, err := models.NewRegistry(paths)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create registry: %v", err),
			})
			return
		}

		// Copy model to models directory if not already there
		modelPath := paths.ModelPath(req.Name)
		if req.Path != modelPath {
			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("failed to create model directory: %v", err),
				})
				return
			}

			// Copy directory contents
			if err := copyDir(req.Path, modelPath); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("failed to copy model: %v", err),
				})
				return
			}
		}

		// Scan to pick up the new model
		if err := registry.ScanModels(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to scan models: %v", err),
			})
			return
		}
		
		// Get or generate manifest for the model
		manifest, err := registry.GetManifest(req.Name)
		if err != nil {
			// Model not found, need to refresh
			if err := registry.RefreshModel(req.Name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("failed to generate manifest: %v", err),
				})
				return
			}
			manifest, err = registry.GetManifest(req.Name)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("failed to get manifest: %v", err),
				})
				return
			}
		}
		
		// Update manifest with provided metadata
		manifest.License = req.License
		if req.Version != "" {
			manifest.Version = req.Version
		}

		// Create torrent file
		torrentPath := paths.TorrentPath(req.Name)
		fmt.Printf("[ShareModel] Creating torrent at: %s\n", torrentPath)
		if err := os.MkdirAll(filepath.Dir(torrentPath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create torrent directory: %v", err),
			})
			return
		}

		fmt.Printf("[ShareModel] Generating torrent from directory: %s\n", modelPath)
		infoHash, err := torrent.CreateTorrentFromDirectory(modelPath, torrentPath, req.PieceLength)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create torrent: %v", err),
			})
			return
		}
		fmt.Printf("[ShareModel] Torrent created with InfoHash: %s\n", infoHash)

		// Save manifest to disk
		if err := registry.SaveManifest(manifest); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to save manifest: %v", err),
			})
			return
		}

		// Add torrent to torrent manager for seeding
		tm := h.daemon.GetTorrentManager()
		fmt.Printf("[ShareModel] Adding torrent to torrent manager\n")
		managedTorrent, err := tm.AddTorrent(torrentPath, req.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to add torrent: %v", err),
			})
			return
		}
		fmt.Printf("[ShareModel] Torrent added to manager with InfoHash: %s\n", managedTorrent.InfoHash)
		
		// Start seeding
		fmt.Printf("[ShareModel] Starting seeding for model: %s\n", req.Name)
		if err := tm.StartSeeding(managedTorrent.InfoHash); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to start seeding: %v", err),
			})
			return
		}
		fmt.Printf("[ShareModel] Seeding started successfully\n")

		// Announce to DHT (both regular DHT and BEP44)
		fmt.Printf("[ShareModel] Announcing model to DHT\n")
		dhtManager := h.daemon.GetDHTManager()
		if !req.SkipDHT {
			// Create announcement for BEP44 discovery
			announcement := &types.ModelAnnouncement{
				Name:     req.Name,
				InfoHash: managedTorrent.InfoHash,
				Size:     manifest.TotalSize,
				Version:  req.Version,
			}
			fmt.Printf("[ShareModel] Creating BEP44 announcement for model: %s\n", req.Name)
			if err := dhtManager.AnnounceModel(announcement); err != nil {
				fmt.Printf("[ShareModel] Warning: BEP44 announcement failed: %v\n", err)
			} else {
				fmt.Printf("[ShareModel] BEP44 announcement successful\n")
			}
			
			// Regular DHT announcement happens automatically via BitTorrent client
			fmt.Printf("[ShareModel] Regular DHT announcement will be handled by BitTorrent client\n")
		} else {
			fmt.Printf("[ShareModel] Skipping DHT announcement (--skip-dht flag)\n")
		}

		// Create transfer entry
		transferManager := h.daemon.GetTransferManager()
		transfer := transferManager.CreateSeed(req.Name, managedTorrent.InfoHash)
		transfer.Status = "active"

		c.JSON(http.StatusOK, gin.H{
			"message":     "model published and seeding started",
			"model_name":  req.Name,
			"info_hash":   infoHash,
			"transfer_id": transfer.ID,
		})
		return
	}
	
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "must specify model_name, path, or all=true",
	})
}


// parseRepoURL extracts model name from repository URL
func parseRepoURL(repoURL string) string {
	// Handle HuggingFace URLs
	if strings.Contains(repoURL, "huggingface.co") {
		parts := strings.Split(repoURL, "/")
		if len(parts) >= 5 {
			// Format: https://huggingface.co/owner/model
			return parts[3] + "/" + parts[4]
		}
	}
	
	// Handle GitHub URLs
	if strings.Contains(repoURL, "github.com") {
		parts := strings.Split(repoURL, "/")
		if len(parts) >= 5 {
			// Format: https://github.com/owner/repo
			owner := parts[3]
			repo := strings.TrimSuffix(parts[4], ".git")
			return owner + "/" + repo
		}
	}
	
	// For other git URLs, try to extract a reasonable name
	parts := strings.Split(repoURL, "/")
	if len(parts) >= 2 {
		modelName := strings.TrimSuffix(parts[len(parts)-1], ".git")
		if len(parts) >= 3 {
			return parts[len(parts)-2] + "/" + modelName
		}
		return "unknown/" + modelName
	}
	
	return ""
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// RemoveModel removes a model from local storage
func (h *Handlers) RemoveModel(c *gin.Context) {
	modelName := c.Param("name")
	
	// Clean up model name
	modelName = strings.ReplaceAll(modelName, "/", "_")
	
	paths, err := storage.NewPaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to initialize paths: %v", err),
		})
		return
	}
	
	registry, err := models.NewRegistry(paths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to create registry: %v", err),
		})
		return
	}
	
	if err := registry.ScanModels(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to scan models: %v", err),
		})
		return
	}
	
	// Check if model exists
	_, err = registry.GetManifest(modelName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("model %s not found: %v", modelName, err),
		})
		return
	}
	
	// Get the info hash from the manifest (we need to extract it from magnet URI)
	// For now, just use the model name as identifier
	infoHash := modelName
	
	// Stop seeding if active
	h.daemon.GetTorrentManager().RemoveTorrent(infoHash)
	
	// Remove from DHT
	h.daemon.GetDHTManager().RemoveTorrentFromDHT(infoHash)
	
	// Note: We don't actually delete the files here - that would be done separately
	// This just removes it from active management
	
	c.JSON(http.StatusOK, gin.H{
		"message":    "model removed from active management",
		"model_name": modelName,
	})
}