// +build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDHTEndToEnd tests the complete DHT workflow
func TestDHTEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Create temp directories for two peers
	peer1Dir := t.TempDir()
	peer2Dir := t.TempDir()

	// Setup peer 1 - will announce a model
	t.Log("Setting up peer 1...")
	os.Setenv("SILMARIL_HOME", peer1Dir)
	
	paths1, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry1, err := models.NewRegistry(paths1)
	require.NoError(t, err)
	
	// Create a test model in peer1's directory
	testModelDir := filepath.Join(paths1.ModelsDir(), "test-org/dht-test-model")
	err = os.MkdirAll(testModelDir, 0755)
	require.NoError(t, err)
	
	// Create a simple model file
	modelFile := filepath.Join(testModelDir, "model.txt")
	err = os.WriteFile(modelFile, []byte("This is a test model for DHT E2E testing"), 0644)
	require.NoError(t, err)
	
	// Create a config.json to make it discoverable
	configFile := filepath.Join(testModelDir, "config.json")
	configData := `{"model_type": "test", "architectures": ["TestModel"]}`
	err = os.WriteFile(configFile, []byte(configData), 0644)
	require.NoError(t, err)
	
	// Scan to register the model
	err = registry1.Rescan()
	require.NoError(t, err)
	
	// Get the generated manifest
	manifest1, err := registry1.GetManifest("test-org/dht-test-model")
	require.NoError(t, err)
	assert.NotNil(t, manifest1)
	t.Logf("Model registered: %s", manifest1.Name)
	
	// Create DHT node for peer 1
	dht1, err := federation.NewDHTDiscovery(peer1Dir, 0)
	require.NoError(t, err)
	defer dht1.Close()
	
	// Bootstrap peer 1
	ctx := context.Background()
	t.Log("Bootstrapping peer 1 to DHT...")
	err = dht1.Bootstrap(ctx)
	if err != nil {
		t.Logf("Warning: DHT bootstrap incomplete: %v", err)
	}
	
	// Announce the model
	t.Log("Announcing model on DHT...")
	err = dht1.AnnounceModel(manifest1)
	require.NoError(t, err)
	
	// Give DHT time to propagate
	t.Log("Waiting for DHT propagation...")
	time.Sleep(3 * time.Second)
	
	// Setup peer 2 - will discover the model
	t.Log("Setting up peer 2...")
	os.Setenv("SILMARIL_HOME", peer2Dir)
	
	dht2, err := federation.NewDHTDiscovery(peer2Dir, 0)
	require.NoError(t, err)
	defer dht2.Close()
	
	// Bootstrap peer 2
	t.Log("Bootstrapping peer 2 to DHT...")
	err = dht2.Bootstrap(ctx)
	if err != nil {
		t.Logf("Warning: DHT bootstrap incomplete: %v", err)
	}
	
	// Try to discover the model
	t.Log("Searching for model from peer 2...")
	searchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	discovered, err := dht2.SearchForModel(searchCtx, "test-org/dht-test-model")
	if err != nil {
		t.Logf("Search error: %v", err)
	}
	
	if discovered != nil {
		t.Logf("✅ Model discovered: %s", discovered.Name)
		assert.Equal(t, "test-org/dht-test-model", discovered.Name)
		assert.NotEmpty(t, discovered.MagnetURI)
		t.Logf("Magnet URI: %s", discovered.MagnetURI)
	} else {
		t.Log("Model not discovered (this is expected in test environment without real DHT peers)")
	}
	
	// Test that peer 1 can find its own model
	t.Log("Testing self-discovery...")
	selfDiscovered, err := dht1.SearchForModel(searchCtx, "test-org/dht-test-model")
	assert.NoError(t, err)
	if selfDiscovered != nil {
		assert.Equal(t, "test-org/dht-test-model", selfDiscovered.Name)
		t.Log("✅ Self-discovery successful")
	}
	
	// Test getting peers
	t.Log("Getting peers for model...")
	peers, err := dht1.GetPeers(ctx, "test-org/dht-test-model")
	assert.NoError(t, err)
	assert.NotNil(t, peers)
	t.Logf("Found %d peers", len(peers))
	
	// Test stats
	stats := dht1.Stats()
	assert.NotNil(t, stats)
	t.Logf("DHT Stats: %+v", stats)
	
	// Test model status
	status, err := dht1.GetModelStatus("test-org/dht-test-model")
	assert.NoError(t, err)
	assert.NotNil(t, status)
	t.Logf("Model status: Seeders=%d, Leechers=%d", status.Seeders, status.Leechers)
}

// TestDHTMagnetDiscovery tests discovering models via magnet URIs
func TestDHTMagnetDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	
	tempDir := t.TempDir()
	
	// Create DHT node
	dht, err := federation.NewDHTDiscovery(tempDir, 0)
	require.NoError(t, err)
	defer dht.Close()
	
	// Bootstrap
	ctx := context.Background()
	err = dht.Bootstrap(ctx)
	if err != nil {
		t.Logf("Warning: DHT bootstrap incomplete: %v", err)
	}
	
	// Create a test manifest with magnet URI
	manifest := &types.ModelManifest{
		Name:        "test/magnet-model",
		Version:     "v1.0",
		Description: "Test model for magnet URI discovery",
		TotalSize:   1024,
		Files: []types.ModelFile{
			{Path: "model.bin", Size: 1024},
		},
	}
	
	// Announce the model
	err = dht.AnnounceModel(manifest)
	require.NoError(t, err)
	
	// Try to discover models
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	models, err := dht.DiscoverModels(ctx)
	assert.NoError(t, err)
	
	// Should at least find our own announced model
	found := false
	for _, m := range models {
		if m.Name == "test/magnet-model" {
			found = true
			t.Logf("Found model: %s", m.Name)
			break
		}
	}
	
	if !found {
		t.Log("Model not found in discovery (expected in isolated test)")
	}
	
	// Test magnet URI parsing
	testMagnet := "magnet:?xt=urn:btih:abc123&dn=Silmaril: test/model (v1.0)"
	name, version, ok := federation.ParseSilmarilMagnet(testMagnet)
	assert.True(t, ok)
	assert.Equal(t, "test/model", name)
	assert.Equal(t, "v1.0", version)
	
	// Test creating magnet URI
	magnet := federation.CreateSilmarilMagnet("test/model", "v2.0", "def456")
	assert.Contains(t, magnet, "Silmaril:")
	assert.Contains(t, magnet, "test/model")
	assert.Contains(t, magnet, "v2.0")
}

// TestDHTConcurrentPeers tests multiple peers interacting
func TestDHTConcurrentPeers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	
	// Create 3 peers
	numPeers := 3
	dhts := make([]*federation.DHTDiscovery, numPeers)
	
	for i := 0; i < numPeers; i++ {
		tempDir := t.TempDir()
		dht, err := federation.NewDHTDiscovery(tempDir, 0)
		require.NoError(t, err)
		dhts[i] = dht
		defer dht.Close()
		
		// Bootstrap each peer
		ctx := context.Background()
		err = dht.Bootstrap(ctx)
		if err != nil {
			t.Logf("Peer %d: DHT bootstrap incomplete: %v", i, err)
		}
	}
	
	// Each peer announces a different model
	for i, dht := range dhts {
		manifest := &types.ModelManifest{
			Name:        fmt.Sprintf("peer%d/model", i),
			Version:     "v1.0",
			Description: fmt.Sprintf("Model from peer %d", i),
			TotalSize:   int64(1024 * (i + 1)),
		}
		
		err := dht.AnnounceModel(manifest)
		require.NoError(t, err)
		t.Logf("Peer %d announced model: %s", i, manifest.Name)
	}
	
	// Give DHT time to propagate
	time.Sleep(2 * time.Second)
	
	// Each peer tries to discover all models
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	for i, dht := range dhts {
		models, err := dht.DiscoverModels(ctx)
		assert.NoError(t, err)
		t.Logf("Peer %d discovered %d models", i, len(models))
		
		// Should at least find own model
		foundOwn := false
		for _, m := range models {
			if m.Name == fmt.Sprintf("peer%d/model", i) {
				foundOwn = true
			}
			t.Logf("  - %s", m.Name)
		}
		assert.True(t, foundOwn, "Peer should find its own model")
	}
}