package federation

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDHTDiscovery(t *testing.T) {
	// Create temp directory for test data
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create DHT discovery instance
	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Test bootstrap
	ctx := context.Background()
	err = dht.Bootstrap(ctx)
	assert.NoError(t, err)
}

func TestModelAnnouncement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Create a test manifest
	manifest := &types.ModelManifest{
		Name:        "test/model",
		Version:     "v1",
		Description: "Test model",
		TotalSize:   1024 * 1024 * 100, // 100MB
		Files: []types.ModelFile{
			{
				Path:   "model.bin",
				Size:   1024 * 1024 * 100,
				SHA256: "abc123",
			},
		},
	}

	// Announce the model
	err = dht.AnnounceModel(manifest)
	assert.NoError(t, err)

	// Verify it's stored locally
	ctx := context.Background()
	found, err := dht.SearchForModel(ctx, "test/model")
	assert.NoError(t, err)
	assert.NotNil(t, found)
	assert.Equal(t, "test/model", found.Name)
}

func TestModelDiscovery(t *testing.T) {
	t.Skip("Skipping test due to port binding issues in CI")
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create two DHT instances to simulate peers
	dht1, err := NewDHTDiscovery(tmpDir+"/1", 0)
	require.NoError(t, err)
	defer dht1.Close()

	dht2, err := NewDHTDiscovery(tmpDir+"/2", 0) // Will use different random port
	require.NoError(t, err)
	defer dht2.Close()

	// Bootstrap both
	ctx := context.Background()
	dht1.Bootstrap(ctx)
	dht2.Bootstrap(ctx)

	// Announce a model from dht1
	manifest := &types.ModelManifest{
		Name:      "shared/model",
		Version:   "v2",
		TotalSize: 5000000,
	}
	err = dht1.AnnounceModel(manifest)
	require.NoError(t, err)

	// Try to discover from dht2 (may not work in test environment without real DHT)
	// This is more of an integration test
	time.Sleep(2 * time.Second)
	
	models, err := dht2.DiscoverModels(ctx)
	assert.NoError(t, err)
	// In a real DHT network, we would find the model
	t.Logf("Discovered %d models", len(models))
}

func TestInfoHashGeneration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Test that same model name produces same info hash
	hash1 := dht.modelInfoHash("meta-llama/Llama-3.1-70B", "v1")
	hash2 := dht.modelInfoHash("meta-llama/Llama-3.1-70B", "v1")
	assert.Equal(t, hash1, hash2)

	// Different names produce different hashes
	hash3 := dht.modelInfoHash("meta-llama/Llama-3.1-8B", "v1")
	assert.NotEqual(t, hash1, hash3)

	// Different versions produce different hashes
	hash4 := dht.modelInfoHash("meta-llama/Llama-3.1-70B", "v2")
	assert.NotEqual(t, hash1, hash4)
}

func TestMagnetURIParsing(t *testing.T) {
	tests := []struct {
		name        string
		magnetURI   string
		wantModel   string
		wantVersion string
		wantOk      bool
	}{
		{
			name:        "Silmaril model with version",
			magnetURI:   "magnet:?xt=urn:btih:abc123&dn=Silmaril: meta-llama/Llama-3.1-70B (v1)",
			wantModel:   "meta-llama/Llama-3.1-70B",
			wantVersion: "v1",
			wantOk:      true,
		},
		{
			name:        "Silmaril model without version",
			magnetURI:   "magnet:?xt=urn:btih:def456&dn=Silmaril: google/gemma-7b",
			wantModel:   "google/gemma-7b",
			wantVersion: "",
			wantOk:      true,
		},
		{
			name:        "Non-Silmaril magnet",
			magnetURI:   "magnet:?xt=urn:btih:xyz789&dn=Ubuntu 22.04",
			wantModel:   "",
			wantVersion: "",
			wantOk:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, version, ok := ParseSilmarilMagnet(tt.magnetURI)
			assert.Equal(t, tt.wantModel, model)
			assert.Equal(t, tt.wantVersion, version)
			assert.Equal(t, tt.wantOk, ok)
		})
	}
}

func TestCreateSilmarilMagnet(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		version   string
		infoHash  string
		want      string
	}{
		{
			name:      "Model with version",
			modelName: "meta-llama/Llama-3.1-70B",
			version:   "v1",
			infoHash:  "abc123def456",
			want:      "magnet:?xt=urn:btih:abc123def456&dn=Silmaril: meta-llama/Llama-3.1-70B (v1)",
		},
		{
			name:      "Model without version",
			modelName: "google/gemma-7b",
			version:   "",
			infoHash:  "789xyz",
			want:      "magnet:?xt=urn:btih:789xyz&dn=Silmaril: google/gemma-7b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateSilmarilMagnet(tt.modelName, tt.version, tt.infoHash)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetPeers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Announce a model
	manifest := &types.ModelManifest{
		Name:      "test/peers",
		Version:   "v1",
		TotalSize: 1000,
	}
	err = dht.AnnounceModel(manifest)
	require.NoError(t, err)

	// Try to get peers (won't have any in test environment)
	ctx := context.Background()
	peers, err := dht.GetPeers(ctx, "test/peers")
	assert.NoError(t, err)
	// Peers will be empty array in test environment
	assert.NotNil(t, peers)
}

func TestStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Get stats
	stats := dht.Stats()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "models")
	assert.Contains(t, stats, "torrents")
}

func TestModelStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	// Announce a model
	manifest := &types.ModelManifest{
		Name:      "status/test",
		Version:   "v1",
		TotalSize: 2000,
	}
	err = dht.AnnounceModel(manifest)
	require.NoError(t, err)

	// Get status
	status, err := dht.GetModelStatus("status/test")
	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, "status/test", status.Manifest.Name)
	assert.Equal(t, "v1", status.Manifest.Version)
}

func TestConcurrentOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "silmaril-dht-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dht, err := NewDHTDiscovery(tmpDir, 0)
	require.NoError(t, err)
	defer dht.Close()

	ctx := context.Background()

	// Concurrent announcements
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			manifest := &types.ModelManifest{
				Name:      fmt.Sprintf("concurrent/model-%d", idx),
				Version:   "v1",
				TotalSize: int64(1000 * idx),
			}
			err := dht.AnnounceModel(manifest)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all announcements
	for i := 0; i < 5; i++ {
		<-done
	}

	// Verify all models are stored
	for i := 0; i < 5; i++ {
		found, err := dht.SearchForModel(ctx, fmt.Sprintf("concurrent/model-%d", i))
		assert.NoError(t, err)
		assert.NotNil(t, found)
	}
}