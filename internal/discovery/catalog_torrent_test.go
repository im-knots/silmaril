package discovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCatalogTorrent(t *testing.T) (*CatalogTorrent, *torrent.Client, string) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "silmaril-catalog-test-*")
	require.NoError(t, err)

	// Save old env and set environment variable to use temp dir
	oldEnv := os.Getenv("SILMARIL_BASE_DIR")
	os.Setenv("SILMARIL_BASE_DIR", tmpDir)
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
		if oldEnv != "" {
			os.Setenv("SILMARIL_BASE_DIR", oldEnv)
		} else {
			os.Unsetenv("SILMARIL_BASE_DIR")
		}
	})

	// Create torrent client config
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = tmpDir
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cfg.NoDHT = true

	// Create torrent client
	client, err := torrent.NewClient(cfg)
	require.NoError(t, err)

	// Create catalog torrent
	ct, err := NewCatalogTorrent(client)
	require.NoError(t, err)
	
	// Reset catalog to empty state for test isolation
	ct.catalog = &ModelCatalog{
		Version: 1,
		Models:  make(map[string]ModelEntry),
	}
	ct.catalog.Sequence = 0

	return ct, client, tmpDir
}

func TestNewCatalogTorrent(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	assert.NotNil(t, ct)
	assert.NotNil(t, ct.catalog)
	assert.NotNil(t, ct.catalog.Models)
	assert.Equal(t, 1, ct.catalog.Version)
}

func TestCatalogTorrentAddModel(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Add a model
	infoHash, err := ct.AddModel("test-org/test-model", "abc123def456", 1000000)
	require.NoError(t, err)
	assert.NotEmpty(t, infoHash)

	// Check model was added
	assert.Equal(t, 1, len(ct.catalog.Models))
	model, exists := ct.catalog.Models["test-org/test-model"]
	assert.True(t, exists)
	assert.Equal(t, "abc123def456", model.InfoHash)
	assert.Equal(t, int64(1000000), model.Size)
	assert.NotEmpty(t, model.Tags)
}

func TestAddMultipleModels(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Add multiple models
	models := []struct {
		name     string
		infoHash string
		size     int64
	}{
		{"test-org/model-alpha", "hash1", 1000},
		{"test-org/model-beta", "hash2", 2000},
		{"test-org/model-gamma", "hash3", 3000},
	}

	for _, m := range models {
		infoHash, err := ct.AddModel(m.name, m.infoHash, m.size)
		require.NoError(t, err)
		assert.NotEmpty(t, infoHash)
	}

	// Check all models were added
	assert.Equal(t, 3, len(ct.catalog.Models))
	
	// Verify catalog sequence incremented
	assert.Equal(t, int64(3), ct.catalog.Sequence)
}

func TestCatalogTorrentGetModels(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Add test models
	ct.AddModel("meta-llama/llama-7b", "hash1", 7000000000)
	ct.AddModel("mistralai/mistral-7b", "hash2", 7000000000)
	ct.AddModel("openai/gpt-3b", "hash3", 3000000000)

	// Test wildcard search
	results, err := ct.GetModels("*")
	require.NoError(t, err)
	assert.Equal(t, 3, len(results))

	// Test specific search
	results, err = ct.GetModels("llama")
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "meta-llama/llama-7b", results[0].Name)

	// Test size tag search
	results, err = ct.GetModels("7b")
	require.NoError(t, err)
	assert.Equal(t, 2, len(results))

	// Test empty search
	results, err = ct.GetModels("")
	require.NoError(t, err)
	assert.Equal(t, 3, len(results))
}

func TestCatalogPersistence(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Add a model
	ct.AddModel("test-org/persistent-model", "persisthash", 5000)

	// Check catalog file exists
	catalogFile := filepath.Join(tmpDir, "catalog", "catalog.json")
	assert.FileExists(t, catalogFile)

	// Create new catalog instance
	ct2, err := NewCatalogTorrent(client)
	require.NoError(t, err)

	// Should have loaded the persisted catalog
	assert.Equal(t, 1, len(ct2.catalog.Models))
	model, exists := ct2.catalog.Models["test-org/persistent-model"]
	assert.True(t, exists)
	assert.Equal(t, "persisthash", model.InfoHash)
}

func TestMergeCatalog(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Clear the catalog first to ensure clean state
	ct.catalog.Models = make(map[string]ModelEntry)
	
	// Add initial models with older timestamps
	baseTime := time.Now().Unix() - 1000
	ct.catalog.Models["model1"] = ModelEntry{InfoHash: "hash1", Size: 1000, Added: baseTime}
	ct.catalog.Models["model2"] = ModelEntry{InfoHash: "hash2", Size: 2000, Added: baseTime}

	// Create another catalog to merge with newer timestamps
	otherCatalog := &ModelCatalog{
		Version: 1,
		Models: map[string]ModelEntry{
			"model2": {InfoHash: "hash2-updated", Size: 2500, Added: time.Now().Unix()},
			"model3": {InfoHash: "hash3", Size: 3000, Added: time.Now().Unix()},
		},
	}

	// Merge catalogs
	changed := ct.MergeCatalog(otherCatalog)
	assert.True(t, changed)

	// Check merged results
	assert.Equal(t, 3, len(ct.catalog.Models))
	
	// model1 should be unchanged
	assert.Equal(t, "hash1", ct.catalog.Models["model1"].InfoHash)
	
	// model2 should be updated (newer timestamp)
	assert.Equal(t, "hash2-updated", ct.catalog.Models["model2"].InfoHash)
	assert.Equal(t, int64(2500), ct.catalog.Models["model2"].Size)
	
	// model3 should be added
	assert.Equal(t, "hash3", ct.catalog.Models["model3"].InfoHash)
}

func TestGetCatalogReference(t *testing.T) {
	ct, client, tmpDir := setupTestCatalogTorrent(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()

	// Initially no reference
	ref := ct.GetCatalogReference()
	assert.Nil(t, ref)

	// Clear catalog to ensure clean state
	ct.catalog.Models = make(map[string]ModelEntry)
	ct.catalog.Sequence = 0
	
	// Add a model to create catalog torrent
	infoHash, err := ct.AddModel("test-model", "testhash", 1000)
	require.NoError(t, err)

	// Now should have reference
	ref = ct.GetCatalogReference()
	assert.NotNil(t, ref)
	assert.Equal(t, infoHash, ref.InfoHash)
	assert.Equal(t, int64(1), ref.Sequence)
}

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
	}{
		{
			name:     "meta-llama/llama-7b",
			expected: []string{"meta", "llama", "7b"},
		},
		{
			name:     "mistralai/mistral-8x7b",
			expected: []string{"mistralai", "mistral", "8x7b"},
		},
		{
			name:     "openai/gpt-3b-instruct",
			expected: []string{"openai", "gpt", "3b", "instruct"},
		},
		{
			name:     "simple-model",
			expected: []string{"simple", "model"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := extractTags(tt.name)
			for _, expectedTag := range tt.expected {
				assert.Contains(t, tags, expectedTag)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{"meta-llama/llama-7b", "*", true},
		{"meta-llama/llama-7b", "llama", true},
		{"meta-llama/llama-7b", "LLAMA", true}, // case insensitive
		{"meta-llama/llama-7b", "mistral", false},
		{"meta-llama/llama-7b", "7b", true},
		{"meta-llama/llama-7b", "", true},
		{"mistralai/mistral-7b", "mistral", true},
		{"mistralai/mistral-7b", "ai", true},
	}

	for _, tt := range tests {
		t.Run(tt.name+" with "+tt.pattern, func(t *testing.T) {
			result := matchesPattern(tt.name, tt.pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}