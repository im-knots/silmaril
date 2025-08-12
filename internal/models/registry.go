package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
)

const (
	ManifestFileName = ".silmaril.json"
	HFConfigFile     = "config.json"
)

// Registry manages model manifests dynamically
type Registry struct {
	mu       sync.RWMutex
	models   map[string]*types.ModelManifest
	paths    *storage.Paths
}

// NewRegistry creates a new registry instance and scans for models
func NewRegistry(paths *storage.Paths) (*Registry, error) {
	r := &Registry{
		models: make(map[string]*types.ModelManifest),
		paths:  paths,
	}
	
	// Initialize directories
	if err := paths.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize paths: %w", err)
	}
	
	// Scan for existing models
	if err := r.ScanModels(); err != nil {
		return nil, fmt.Errorf("failed to scan models: %w", err)
	}
	
	return r, nil
}

// ScanModels scans the models directory and builds the registry
func (r *Registry) ScanModels() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	modelsDir := r.paths.ModelsDir()
	
	// Check if models directory exists
	if _, err := os.Stat(modelsDir); os.IsNotExist(err) {
		// No models directory yet, that's ok
		return nil
	}
	
	// Walk through the models directory
	err := filepath.Walk(modelsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip problematic paths
		}
		
		// Skip if not a directory
		if !info.IsDir() {
			return nil
		}
		
		// Skip the root models directory itself
		if path == modelsDir {
			return nil
		}
		
		// Check for Silmaril manifest
		manifestPath := filepath.Join(path, ManifestFileName)
		if manifest, err := r.loadManifest(manifestPath); err == nil {
			// Found a Silmaril-managed model
			modelName := strings.TrimPrefix(path, modelsDir+string(filepath.Separator))
			modelName = filepath.ToSlash(modelName) // Convert to forward slashes
			manifest.Name = modelName // Ensure name matches directory
			r.models[modelName] = manifest
			return filepath.SkipDir // Don't recurse into this model's subdirectories
		}
		
		// Check if this looks like a HuggingFace model (has config.json)
		configPath := filepath.Join(path, HFConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			// Found a potential model without Silmaril manifest
			modelName := strings.TrimPrefix(path, modelsDir+string(filepath.Separator))
			modelName = filepath.ToSlash(modelName)
			
			// Generate a manifest for this model
			manifest, err := r.generateManifest(path, modelName)
			if err == nil {
				r.models[modelName] = manifest
				// Save the generated manifest
				r.saveManifestToDisk(manifest)
			}
			return filepath.SkipDir
		}
		
		return nil
	})
	
	return err
}

// loadManifest loads a Silmaril manifest from disk
func (r *Registry) loadManifest(path string) (*types.ModelManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	
	var manifest types.ModelManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	
	return &manifest, nil
}

// generateManifest creates a manifest for a model without one
func (r *Registry) generateManifest(modelPath, modelName string) (*types.ModelManifest, error) {
	manifest := &types.ModelManifest{
		Name:        modelName,
		Version:     "unknown",
		Description: fmt.Sprintf("Model imported from %s", modelName),
		CreatedAt:   time.Now(),
		ModelType:   "unknown",
	}
	
	// Try to load HuggingFace config to get more info
	configPath := filepath.Join(modelPath, HFConfigFile)
	if configData, err := os.ReadFile(configPath); err == nil {
		var hfConfig types.HFConfig
		if err := json.Unmarshal(configData, &hfConfig); err == nil {
			// Extract useful information from HF config
			if hfConfig.ModelType != "" {
				manifest.ModelType = hfConfig.ModelType
			}
			if len(hfConfig.Architectures) > 0 {
				manifest.Architecture = hfConfig.Architectures[0]
			}
			if hfConfig.NumParameters > 0 {
				manifest.Parameters = hfConfig.NumParameters
			}
			
			// Set inference hints based on config
			manifest.InferenceHints = types.InferenceHints{
				ContextLength: hfConfig.MaxPositionEmbeddings,
			}
			
			// Estimate RAM requirements (rough estimate)
			if manifest.Parameters > 0 {
				// Assume 2 bytes per parameter for FP16
				minRAMGB := (manifest.Parameters * 2) / (1024 * 1024 * 1024)
				manifest.InferenceHints.MinRAM = minRAMGB + 2 // Add 2GB overhead
				manifest.InferenceHints.MinVRAM = minRAMGB
			}
		}
	}
	
	// Scan files in the model directory
	var totalSize int64
	var files []types.ModelFile
	
	err := filepath.Walk(modelPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		
		relPath, _ := filepath.Rel(modelPath, path)
		relPath = filepath.ToSlash(relPath)
		
		// Calculate file hash (expensive for large files, so we'll do it lazily)
		hash := ""
		if info.Size() < 100*1024*1024 { // Only hash files < 100MB for now
			if h, err := r.hashFile(path); err == nil {
				hash = h
			}
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
		return nil, err
	}
	
	manifest.Files = files
	manifest.TotalSize = totalSize
	
	return manifest, nil
}

// hashFile calculates SHA256 hash of a file
func (r *Registry) hashFile(path string) (string, error) {
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

// GetManifest retrieves a model manifest
func (r *Registry) GetManifest(name string) (*types.ModelManifest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	manifest, ok := r.models[name]
	if !ok {
		return nil, fmt.Errorf("model %s not found in registry", name)
	}
	return manifest, nil
}

// SaveManifest saves a model manifest
func (r *Registry) SaveManifest(manifest *types.ModelManifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Save to memory
	r.models[manifest.Name] = manifest
	
	// Save to disk
	return r.saveManifestToDisk(manifest)
}

// saveManifestToDisk saves a manifest to the model's directory
func (r *Registry) saveManifestToDisk(manifest *types.ModelManifest) error {
	modelPath := r.paths.ModelPath(manifest.Name)
	
	// Ensure model directory exists
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}
	
	// Save manifest to model directory
	manifestPath := filepath.Join(modelPath, ManifestFileName)
	
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	
	return os.WriteFile(manifestPath, data, 0644)
}

// ListModels returns all model names in the registry
func (r *Registry) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	models := make([]string, 0, len(r.models))
	for name := range r.models {
		models = append(models, name)
	}
	return models
}

// GetAllManifests returns all manifests in the registry
func (r *Registry) GetAllManifests() []*types.ModelManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	manifests := make([]*types.ModelManifest, 0, len(r.models))
	for _, manifest := range r.models {
		manifests = append(manifests, manifest)
	}
	return manifests
}

// DeleteModel removes a model from the registry (but not from disk)
func (r *Registry) DeleteModel(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if _, ok := r.models[name]; !ok {
		return fmt.Errorf("model %s not found", name)
	}
	
	delete(r.models, name)
	return nil
}

// RefreshModel re-scans a specific model and updates its manifest
func (r *Registry) RefreshModel(name string) error {
	modelPath := r.paths.ModelPath(name)
	
	// Check if model directory exists
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return fmt.Errorf("model directory does not exist: %s", modelPath)
	}
	
	// Always regenerate the manifest to pick up file changes
	manifest, err := r.generateManifest(modelPath, name)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %w", err)
	}
	
	// Try to preserve some fields from existing manifest if it exists
	manifestPath := filepath.Join(modelPath, ManifestFileName)
	if oldManifest, err := r.loadManifest(manifestPath); err == nil {
		// Preserve user-editable fields
		if oldManifest.Description != "" && !strings.Contains(manifest.Description, "imported from") {
			manifest.Description = oldManifest.Description
		}
		if oldManifest.License != "" {
			manifest.License = oldManifest.License
		}
		if oldManifest.Version != "unknown" {
			manifest.Version = oldManifest.Version
		}
		if oldManifest.MagnetURI != "" {
			manifest.MagnetURI = oldManifest.MagnetURI
		}
	}
	
	// Update registry
	r.mu.Lock()
	r.models[name] = manifest
	r.mu.Unlock()
	
	// Save to disk
	return r.saveManifestToDisk(manifest)
}

// UpdateManifest updates specific fields of a manifest
func (r *Registry) UpdateManifest(name string, updates map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	manifest, ok := r.models[name]
	if !ok {
		return fmt.Errorf("model %s not found", name)
	}
	
	// Apply updates (simplified - in production you'd want more sophisticated merging)
	if desc, ok := updates["description"].(string); ok {
		manifest.Description = desc
	}
	if version, ok := updates["version"].(string); ok {
		manifest.Version = version
	}
	if license, ok := updates["license"].(string); ok {
		manifest.License = license
	}
	if magnetURI, ok := updates["magnet_uri"].(string); ok {
		manifest.MagnetURI = magnetURI
	}
	
	// Save updated manifest
	return r.saveManifestToDisk(manifest)
}

// Rescan triggers a full rescan of the models directory
func (r *Registry) Rescan() error {
	// Clear existing models
	r.mu.Lock()
	r.models = make(map[string]*types.ModelManifest)
	r.mu.Unlock()
	
	// Scan again
	return r.ScanModels()
}