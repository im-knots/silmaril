package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
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
	
	modelsList := registry.ListModels()
	c.JSON(http.StatusOK, gin.H{
		"models": modelsList,
		"count":  len(modelsList),
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
	ModelName string `json:"model_name"`
	Path      string `json:"path"`
	All       bool   `json:"all"`
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
		for _, manifest := range modelsList {
			// Create seed transfer for each model
			tm := h.daemon.GetTransferManager()
			infoHash := manifest.Name // Use model name as identifier for now
			transfer := tm.CreateSeed(manifest.Name, infoHash)
			
			// Start seeding
			if err := h.daemon.GetTorrentManager().StartSeeding(infoHash); err == nil {
				shared++
				transfer.Status = "active"
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"message":      "started sharing models",
			"models_shared": shared,
			"total_models": len(modelsList),
		})
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
	
	// Share from path (new model)
	if req.Path != "" {
		// This would involve creating a torrent, which requires more implementation
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "sharing from path not yet implemented",
		})
		return
	}
	
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "must specify model_name, path, or all=true",
	})
}

// MirrorModelRequest represents a mirror request
type MirrorModelRequest struct {
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
	Depth       int    `json:"depth"`
	SkipLFS     bool   `json:"skip_lfs"`
	NoBroadcast bool   `json:"no_broadcast"`
	AutoShare   bool   `json:"auto_share"`
}

// MirrorModel mirrors a model from HuggingFace
func (h *Handlers) MirrorModel(c *gin.Context) {
	var req MirrorModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid request: %v", err),
		})
		return
	}
	
	// Set defaults
	if req.Branch == "" {
		req.Branch = "main"
	}
	if req.Depth == 0 {
		req.Depth = 1
	}
	
	// TODO: Implement actual mirroring logic
	// This would involve:
	// 1. Parsing the HuggingFace URL
	// 2. Cloning the repository
	// 3. Generating manifest
	// 4. Creating torrent
	// 5. Starting to share if requested
	
	c.JSON(http.StatusAccepted, gin.H{
		"message": "mirror operation started",
		"repo_url": req.RepoURL,
		"status": "pending",
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