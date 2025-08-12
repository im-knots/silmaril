package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	assert.NotNil(t, registry)
	assert.NotNil(t, registry.models)
	assert.NotNil(t, registry.paths)
}

func TestScanModels(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	// Create a test model directory with manifest
	modelDir := filepath.Join(paths.ModelsDir(), "test-org/test-model")
	err = os.MkdirAll(modelDir, 0755)
	require.NoError(t, err)
	
	// Create a manifest file
	manifest := &types.ModelManifest{
		Name:        "test-org/test-model",
		Version:     "v1.0",
		Description: "Test model",
		TotalSize:   1000,
		Files: []types.ModelFile{
			{Path: "model.bin", Size: 1000, SHA256: "abc123"},
		},
	}
	
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	
	manifestPath := filepath.Join(modelDir, ManifestFileName)
	err = os.WriteFile(manifestPath, manifestData, 0644)
	require.NoError(t, err)
	
	// Create another model without manifest (HF style)
	hfModelDir := filepath.Join(paths.ModelsDir(), "huggingface/model")
	err = os.MkdirAll(hfModelDir, 0755)
	require.NoError(t, err)
	
	// Create HF config.json
	hfConfig := types.HFConfig{
		ModelType:     "transformer",
		Architectures: []string{"GPT2Model"},
	}
	configData, err := json.Marshal(hfConfig)
	require.NoError(t, err)
	
	configPath := filepath.Join(hfModelDir, HFConfigFile)
	err = os.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)
	
	// Create a dummy model file
	modelFilePath := filepath.Join(hfModelDir, "model.bin")
	err = os.WriteFile(modelFilePath, []byte("model data"), 0644)
	require.NoError(t, err)
	
	// Create registry and scan
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Check that both models were found
	models := registry.ListModels()
	assert.Len(t, models, 2)
	assert.Contains(t, models, "test-org/test-model")
	assert.Contains(t, models, "huggingface/model")
	
	// Check manifest details
	testManifest, err := registry.GetManifest("test-org/test-model")
	require.NoError(t, err)
	assert.Equal(t, "test-org/test-model", testManifest.Name)
	assert.Equal(t, "v1.0", testManifest.Version)
	
	// Check generated manifest for HF model
	hfManifest, err := registry.GetManifest("huggingface/model")
	require.NoError(t, err)
	assert.Equal(t, "huggingface/model", hfManifest.Name)
	assert.Equal(t, "transformer", hfManifest.ModelType)
	assert.Equal(t, "GPT2Model", hfManifest.Architecture)
}

func TestSaveManifest(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Create and save a new manifest
	manifest := &types.ModelManifest{
		Name:        "new-org/new-model",
		Version:     "v2.0",
		Description: "New test model",
		CreatedAt:   time.Now(),
		TotalSize:   2000,
		Files: []types.ModelFile{
			{Path: "weights.bin", Size: 2000, SHA256: "def456"},
		},
	}
	
	err = registry.SaveManifest(manifest)
	require.NoError(t, err)
	
	// Check it's in memory
	retrieved, err := registry.GetManifest("new-org/new-model")
	require.NoError(t, err)
	assert.Equal(t, manifest.Name, retrieved.Name)
	assert.Equal(t, manifest.Version, retrieved.Version)
	
	// Check it's saved to disk
	manifestPath := filepath.Join(paths.ModelPath("new-org/new-model"), ManifestFileName)
	assert.FileExists(t, manifestPath)
	
	// Load from disk and verify
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	
	var diskManifest types.ModelManifest
	err = json.Unmarshal(data, &diskManifest)
	require.NoError(t, err)
	assert.Equal(t, manifest.Name, diskManifest.Name)
}

func TestGenerateManifest(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Create a model directory
	modelDir := filepath.Join(paths.ModelsDir(), "generated/model")
	err = os.MkdirAll(modelDir, 0755)
	require.NoError(t, err)
	
	// Create some files
	files := map[string]int64{
		"model.bin":     1000000,
		"config.json":   1000,
		"tokenizer.json": 5000,
	}
	
	for name, size := range files {
		path := filepath.Join(modelDir, name)
		err = os.WriteFile(path, make([]byte, size), 0644)
		require.NoError(t, err)
	}
	
	// Generate manifest
	manifest, err := registry.generateManifest(modelDir, "generated/model")
	require.NoError(t, err)
	
	assert.Equal(t, "generated/model", manifest.Name)
	assert.Equal(t, "unknown", manifest.Version)
	assert.Contains(t, manifest.Description, "generated/model")
	assert.Equal(t, int64(1006000), manifest.TotalSize)
	assert.Len(t, manifest.Files, 3)
	
	// Check files are correctly listed
	fileMap := make(map[string]int64)
	for _, f := range manifest.Files {
		fileMap[f.Path] = f.Size
	}
	assert.Equal(t, files, fileMap)
}

func TestRefreshModel(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Create a model
	modelDir := filepath.Join(paths.ModelsDir(), "refresh/model")
	err = os.MkdirAll(modelDir, 0755)
	require.NoError(t, err)
	
	// Create config.json to make it discoverable
	configData := `{"model_type": "test"}`
	err = os.WriteFile(filepath.Join(modelDir, "config.json"), []byte(configData), 0644)
	require.NoError(t, err)
	
	// Initial file
	err = os.WriteFile(filepath.Join(modelDir, "model.bin"), []byte("data"), 0644)
	require.NoError(t, err)
	
	// Initial scan
	err = registry.Rescan()
	require.NoError(t, err)
	
	initial, err := registry.GetManifest("refresh/model")
	require.NoError(t, err)
	assert.Equal(t, int64(26), initial.TotalSize) // "data" (4) + config.json (22)
	
	// Add another file
	err = os.WriteFile(filepath.Join(modelDir, "weights.bin"), []byte("more data"), 0644)
	require.NoError(t, err)
	
	// Refresh the model
	err = registry.RefreshModel("refresh/model")
	require.NoError(t, err)
	
	// Check updated manifest
	updated, err := registry.GetManifest("refresh/model")
	require.NoError(t, err)
	// Files should now include: .silmaril.json, config.json, model.bin, weights.bin
	assert.Len(t, updated.Files, 4)
	// Check that weights.bin was added
	var hasWeights bool
	for _, f := range updated.Files {
		if f.Path == "weights.bin" {
			hasWeights = true
			assert.Equal(t, int64(9), f.Size)
		}
	}
	assert.True(t, hasWeights, "weights.bin should be in the file list")
}

func TestUpdateManifest(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Create initial manifest
	manifest := &types.ModelManifest{
		Name:        "update/model",
		Version:     "v1.0",
		Description: "Original description",
		License:     "MIT",
	}
	
	err = registry.SaveManifest(manifest)
	require.NoError(t, err)
	
	// Update specific fields
	updates := map[string]interface{}{
		"description": "Updated description",
		"version":     "v2.0",
		"license":     "Apache-2.0",
	}
	
	err = registry.UpdateManifest("update/model", updates)
	require.NoError(t, err)
	
	// Verify updates
	updated, err := registry.GetManifest("update/model")
	require.NoError(t, err)
	assert.Equal(t, "Updated description", updated.Description)
	assert.Equal(t, "v2.0", updated.Version)
	assert.Equal(t, "Apache-2.0", updated.License)
}

func TestDeleteModel(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Add a model
	manifest := &types.ModelManifest{
		Name:    "delete/model",
		Version: "v1.0",
	}
	
	err = registry.SaveManifest(manifest)
	require.NoError(t, err)
	
	// Verify it exists
	_, err = registry.GetManifest("delete/model")
	require.NoError(t, err)
	
	// Delete it
	err = registry.DeleteModel("delete/model")
	require.NoError(t, err)
	
	// Verify it's gone
	_, err = registry.GetManifest("delete/model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	
	// Try to delete non-existent model
	err = registry.DeleteModel("non-existent")
	assert.Error(t, err)
}

func TestGetAllManifests(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Add multiple models
	models := []string{"model1", "model2", "model3"}
	for _, name := range models {
		manifest := &types.ModelManifest{
			Name:    name,
			Version: "v1.0",
		}
		err = registry.SaveManifest(manifest)
		require.NoError(t, err)
	}
	
	// Get all manifests
	manifests := registry.GetAllManifests()
	assert.Len(t, manifests, 3)
	
	// Verify all models are present
	names := make([]string, len(manifests))
	for i, m := range manifests {
		names[i] = m.Name
	}
	for _, model := range models {
		assert.Contains(t, names, model)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := NewRegistry(paths)
	require.NoError(t, err)
	
	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			manifest := &types.ModelManifest{
				Name:    fmt.Sprintf("concurrent/model-%d", idx),
				Version: "v1.0",
			}
			err := registry.SaveManifest(manifest)
			assert.NoError(t, err)
			done <- true
		}(i)
	}
	
	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_, err := registry.GetManifest(fmt.Sprintf("concurrent/model-%d", idx))
			assert.NoError(t, err)
			done <- true
		}(i)
	}
	
	// Wait for all reads
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify all models exist
	models := registry.ListModels()
	assert.Len(t, models, 10)
}