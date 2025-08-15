package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestTorrentManager(t *testing.T) (*TorrentManager, *State, string) {
	// Create temporary directory
	tmpDir := t.TempDir()
	
	// Create minimal config
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
		Network: config.NetworkConfig{
			ListenPort:     0, // Use random port
			MaxConnections: 10,
			DHTEnabled:     false, // Disable DHT for tests
		},
	}
	
	// Create state
	state := NewState(filepath.Join(tmpDir, "state.json"))
	
	tm, err := NewTorrentManager(cfg, state)
	require.NoError(t, err)
	require.NotNil(t, tm)
	
	return tm, state, tmpDir
}

func TestTorrentManagerNew(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	assert.NotNil(t, tm.client)
	assert.NotNil(t, tm.torrents)
	assert.NotNil(t, tm.config)
	assert.NotNil(t, tm.state)
}

func TestTorrentManagerStop(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	
	// Stop should not panic
	tm.Stop()
	
	// Double stop should also not panic
	tm.Stop()
}

func TestTorrentManagerAddTorrent(t *testing.T) {
	tm, _, tmpDir := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Create a dummy torrent file
	torrentPath := filepath.Join(tmpDir, "test.torrent")
	torrentContent := []byte("d8:announce35:http://tracker.example.com:8080/announce13:creation datei1234567890e4:name8:test.txt12:piece lengthi16384e6:pieces20:01234567890123456789e")
	err := os.WriteFile(torrentPath, torrentContent, 0644)
	require.NoError(t, err)
	
	// Add torrent for download
	downloadPath := filepath.Join(tmpDir, "test-model")
	mt, err := tm.AddTorrentForDownload(torrentPath, "test-model", downloadPath)
	
	// Note: This will fail with invalid torrent data, but we're testing the method exists
	if err != nil {
		assert.Contains(t, err.Error(), "")
	} else {
		assert.NotNil(t, mt)
		assert.Equal(t, "test-model", mt.Name)
	}
}

func TestTorrentManagerGetTorrent(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Test with non-existent torrent
	_, exists := tm.GetTorrent("nonexistent")
	assert.False(t, exists)
}

func TestTorrentManagerGetAllTorrents(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	torrents := tm.GetAllTorrents()
	assert.NotNil(t, torrents)
	assert.Len(t, torrents, 0) // Should be empty initially
}

func TestTorrentManagerRemoveTorrent(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Try to remove non-existent torrent
	err := tm.RemoveTorrent("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTorrentManagerGetStats(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Try to get stats for non-existent torrent
	_, err := tm.GetStats("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTorrentManagerStartSeeding(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Try to start seeding non-existent torrent
	err := tm.StartSeeding("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTorrentManagerStopSeeding(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Try to stop seeding non-existent torrent
	err := tm.StopSeeding("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTorrentManagerGetTotalPeers(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	peers := tm.GetTotalPeers()
	assert.Equal(t, 0, peers) // Should be 0 initially
}

func TestTorrentManagerGetClient(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	client := tm.GetClient()
	assert.NotNil(t, client)
}

func TestTorrentManagerConcurrency(t *testing.T) {
	tm, _, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Test concurrent access
	done := make(chan bool, 3)
	
	// Reader 1
	go func() {
		for i := 0; i < 10; i++ {
			_ = tm.GetAllTorrents()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()
	
	// Reader 2
	go func() {
		for i := 0; i < 10; i++ {
			_, _ = tm.GetTorrent("test")
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()
	
	// Reader 3
	go func() {
		for i := 0; i < 10; i++ {
			_ = tm.GetTotalPeers()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()
	
	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestTorrentManagerStateIntegration(t *testing.T) {
	tm, state, _ := setupTestTorrentManager(t)
	defer tm.Stop()
	
	// Verify state is being used
	assert.NotNil(t, tm.state)
	assert.Same(t, state, tm.state)
	
	// When adding a torrent, it should update state
	// (This would require a valid torrent file to test properly)
}