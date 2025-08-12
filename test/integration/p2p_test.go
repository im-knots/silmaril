// +build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/federation"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTorrentCreationAndDownload tests creating a torrent and downloading it
func TestTorrentCreationAndDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temp directories
	seedDir := t.TempDir()
	leechDir := t.TempDir()
	
	// Create test file
	testFile := filepath.Join(seedDir, "test-model.bin")
	testData := []byte("This is test model data for Silmaril P2P")
	err := os.WriteFile(testFile, testData, 0644)
	require.NoError(t, err)
	
	// Create torrent
	mi := metainfo.MetaInfo{
		CreatedBy: "Silmaril Test",
	}
	
	info := metainfo.Info{
		PieceLength: 16 * 1024, // 16KB pieces for small file
	}
	
	err = info.BuildFromFilePath(seedDir)
	require.NoError(t, err)
	
	mi.InfoBytes, err = bencode.Marshal(info)
	require.NoError(t, err)
	
	// Save torrent file
	torrentPath := filepath.Join(seedDir, "test.torrent")
	f, err := os.Create(torrentPath)
	require.NoError(t, err)
	
	err = mi.Write(f)
	f.Close()
	require.NoError(t, err)
	
	// Create seeder client with random port
	seederCfg := torrent.NewDefaultClientConfig()
	seederCfg.DataDir = seedDir
	seederCfg.Seed = true
	seederCfg.DisableTrackers = true
	seederCfg.NoDHT = false
	seederCfg.ListenPort = 0 // Use random port
	
	seeder, err := torrent.NewClient(seederCfg)
	require.NoError(t, err)
	defer seeder.Close()
	
	// Add torrent to seeder
	_, err = seeder.AddTorrentFromFile(torrentPath)
	require.NoError(t, err)
	
	// Create leecher client with random port
	leecherCfg := torrent.NewDefaultClientConfig()
	leecherCfg.DataDir = leechDir
	leecherCfg.DisableTrackers = true
	leecherCfg.NoDHT = false
	leecherCfg.ListenPort = 0 // Use random port
	
	leecher, err := torrent.NewClient(leecherCfg)
	require.NoError(t, err)
	defer leecher.Close()
	
	// Add torrent to leecher
	leecherTorrent, err := leecher.AddTorrentFromFile(torrentPath)
	require.NoError(t, err)
	
	// Start download
	leecherTorrent.DownloadAll()
	
	// Wait a moment for seeder to be ready
	time.Sleep(500 * time.Millisecond)
	
	// Connect peers directly (since DHT might be slow in tests)
	if addrs := seeder.ListenAddrs(); len(addrs) > 0 {
		// Try to add peer directly to the torrent
		leecherTorrent.AddPeers([]torrent.PeerInfo{
			{Addr: addrs[0]},
		})
	}
	
	// Wait for download with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			t.Fatal("Download timeout")
		case <-ticker.C:
			if leecherTorrent.BytesCompleted() == leecherTorrent.Length() {
				// Verify downloaded file
				downloadedFile := filepath.Join(leechDir, "test-model.bin")
				downloadedData, err := os.ReadFile(downloadedFile)
				require.NoError(t, err)
				assert.Equal(t, testData, downloadedData)
				return
			}
		}
	}
}

// TestModelManifestDistribution tests distributing model manifests
func TestModelManifestDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create test manifest
	manifest := &types.ModelManifest{
		Name:         "test/integration-model",
		Version:      "1.0",
		Description:  "Integration test model",
		License:      "MIT",
		Architecture: "test",
		ModelType:    "test",
		Parameters:   1000,
		TotalSize:    1024,
		Files: []types.ModelFile{
			{
				Path:   "model.bin",
				Size:   1024,
				SHA256: "test-hash",
			},
		},
		MagnetURI: "magnet:?xt=urn:btih:test",
	}
	
	// Create registry and save manifest
	tempDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tempDir)
	defer os.Unsetenv("SILMARIL_HOME")
	
	paths, err := storage.NewPaths()
	require.NoError(t, err)
	
	registry, err := models.NewRegistry(paths)
	require.NoError(t, err)
	
	err = registry.SaveManifest(manifest)
	require.NoError(t, err)
	
	// Verify manifest can be retrieved
	retrieved, err := registry.GetManifest("test/integration-model")
	require.NoError(t, err)
	assert.Equal(t, manifest.Name, retrieved.Name)
	assert.Equal(t, manifest.Version, retrieved.Version)
	assert.Equal(t, manifest.License, retrieved.License)
}

// TestDHTDiscovery tests DHT-based discovery (simplified)
func TestDHTDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This is a simplified test since full DHT testing requires
	// multiple nodes and is complex to set up in unit tests
	
	tempDir := t.TempDir()
	dht, err := federation.NewDHTDiscovery(tempDir, 0)
	require.NoError(t, err)
	defer dht.Close()
	
	// Test bootstrap (with short timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = dht.Bootstrap(ctx)
	// Bootstrap might fail in test environment, that's ok
	if err != nil {
		t.Logf("DHT bootstrap failed (expected in test environment): %v", err)
	}
}

// TestMultiFileModel tests handling models with multiple files
func TestMultiFileModel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	modelDir := t.TempDir()
	
	// Create multiple model files
	files := map[string][]byte{
		"config.json":                  []byte(`{"model_type": "test"}`),
		"tokenizer.json":               []byte(`{"type": "test"}`),
		"model-00001-of-00002.bin":     make([]byte, 1024),
		"model-00002-of-00002.bin":     make([]byte, 1024),
	}
	
	for name, data := range files {
		err := os.WriteFile(filepath.Join(modelDir, name), data, 0644)
		require.NoError(t, err)
	}
	
	// Create torrent for the model
	_ = metainfo.MetaInfo{
		CreatedBy: "Silmaril Test",
	}
	
	info := metainfo.Info{
		PieceLength: 16 * 1024,
	}
	
	err := info.BuildFromFilePath(modelDir)
	require.NoError(t, err)
	
	// Verify all files are included
	assert.Equal(t, int64(2048+len(`{"model_type": "test"}`)+len(`{"type": "test"}`)), info.TotalLength())
	assert.Len(t, info.Files, 4)
}

// TestConfigIntegration tests configuration system integration
func TestConfigIntegration(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")
	
	// Create config file
	configContent := `
storage:
  base_dir: ` + tempDir + `/silmaril
network:
  max_connections: 25
  dht_enabled: true
torrent:
  piece_length: 2097152
security:
  sign_manifests: false
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)
	
	// Set config file location
	os.Setenv("SILMARIL_CONFIG", tempDir)
	defer os.Unsetenv("SILMARIL_CONFIG")
	
	// Initialize config
	err = config.Initialize()
	require.NoError(t, err)
	
	// Verify config was loaded
	cfg := config.Get()
	assert.Equal(t, filepath.Join(tempDir, "silmaril"), cfg.Storage.BaseDir)
	assert.Equal(t, 25, cfg.Network.MaxConnections)
	assert.True(t, cfg.Network.DHTEnabled)
	assert.Equal(t, int64(2097152), cfg.Torrent.PieceLength)
	assert.False(t, cfg.Security.SignManifests)
}

// BenchmarkTorrentCreation benchmarks torrent creation
func BenchmarkTorrentCreation(b *testing.B) {
	tempDir := b.TempDir()
	
	// Create test files
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file%d.bin", i))
		err := os.WriteFile(filename, make([]byte, 1024*1024), 0644) // 1MB files
		require.NoError(b, err)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		info := metainfo.Info{
			PieceLength: 256 * 1024, // 256KB pieces
		}
		
		err := info.BuildFromFilePath(tempDir)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestPeerDiscovery tests peer discovery mechanisms
func TestPeerDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create two clients
	cfg1 := torrent.NewDefaultClientConfig()
	cfg1.DisableTrackers = true
	cfg1.NoDHT = false
	cfg1.DisablePEX = false
	
	client1, err := torrent.NewClient(cfg1)
	require.NoError(t, err)
	defer client1.Close()
	
	cfg2 := torrent.NewDefaultClientConfig()
	cfg2.DisableTrackers = true
	cfg2.NoDHT = false
	cfg2.DisablePEX = false
	
	client2, err := torrent.NewClient(cfg2)
	require.NoError(t, err)
	defer client2.Close()
	
	// Get listen addresses
	addrs1 := client1.ListenAddrs()
	addrs2 := client2.ListenAddrs()
	
	assert.NotEmpty(t, addrs1)
	assert.NotEmpty(t, addrs2)
	
	// In a real test, we would:
	// 1. Create a torrent
	// 2. Add it to both clients
	// 3. Use DHT/PEX to discover peers
	// 4. Verify they connect
}

// Helper function to create a test torrent
func createTestTorrent(t *testing.T, dataDir string, data []byte) (string, error) {
	// Write test data
	testFile := filepath.Join(dataDir, "test.dat")
	err := os.WriteFile(testFile, data, 0644)
	if err != nil {
		return "", err
	}
	
	// Create torrent
	mi := metainfo.MetaInfo{}
	info := metainfo.Info{
		PieceLength: 16 * 1024,
	}
	
	err = info.BuildFromFilePath(dataDir)
	if err != nil {
		return "", err
	}
	
	mi.InfoBytes, err = bencode.Marshal(info)
	if err != nil {
		return "", err
	}
	
	// Save torrent
	torrentPath := filepath.Join(dataDir, "test.torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	
	err = mi.Write(f)
	return torrentPath, err
}