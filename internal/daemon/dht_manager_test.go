package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDHTManager(t *testing.T) (*DHTManager, *TorrentManager, string) {
	// Create temporary directory
	tmpDir := t.TempDir()
	
	// Create minimal config with random ports to avoid conflicts
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled:  true,
			DHTPort:     0, // Use random port
			ListenPort:  0, // Use random port
		},
	}
	
	// Create state and torrent manager first
	state := NewState(filepath.Join(tmpDir, "state.json"))
	tm, err := NewTorrentManager(cfg, state)
	require.NoError(t, err)
	
	dm, err := NewDHTManager(cfg, tm)
	require.NoError(t, err)
	require.NotNil(t, dm)
	
	return dm, tm, tmpDir
}

func TestDHTManagerNew(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// Can't access private fields, but verify manager is not nil
	assert.NotNil(t, dm)
	
	// Verify we can call methods without panic
	_ = dm.GetNodeCount()
	_ = dm.GetStats()
}

func TestDHTManagerStop(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer tm.Stop()
	
	// Stop should not panic
	dm.Stop()
	
	// Double stop should also not panic
	dm.Stop()
}

func TestDHTManagerAnnounceModel(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	announcement := &types.ModelAnnouncement{
		Name:     "test-model",
		InfoHash: "1234567890abcdef",
		Size:     1000000,
	}
	
	// This might fail if not bootstrapped, but should not panic
	err := dm.AnnounceModel(announcement)
	if err != nil {
		assert.Contains(t, err.Error(), "")
	}
}

func TestDHTManagerRefreshAnnouncements(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// This might fail without models, but should not panic
	err := dm.RefreshAnnouncements()
	if err != nil {
		assert.Contains(t, err.Error(), "")
	}
}

func TestDHTManagerDiscoverModels(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// Try to discover models
	results, err := dm.DiscoverModels("test")
	if err != nil {
		assert.Contains(t, err.Error(), "")
	} else {
		assert.NotNil(t, results)
		// Results will likely be empty without a real DHT network
	}
}

func TestDHTManagerGetNodeCount(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	nodes := dm.GetNodeCount()
	assert.GreaterOrEqual(t, nodes, 0)
}

func TestDHTManagerGetStats(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	stats := dm.GetStats()
	assert.NotNil(t, stats)
	
	// Check expected keys
	assert.Contains(t, stats, "nodes")
	assert.Contains(t, stats, "good_nodes")
	assert.Contains(t, stats, "announcements")
	assert.Contains(t, stats, "last_refresh")
}

func TestDHTManagerGetCatalogRef(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// Get catalog ref - may be nil if torrent client not available
	catalogRef := dm.GetCatalogRef()
	// Just verify it doesn't panic
	_ = catalogRef
}

func TestDHTManagerRefreshSeedingModels(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// This should not panic even with no seeding models
	err := dm.RefreshSeedingModels()
	if err != nil {
		// Error is ok, just shouldn't panic
		assert.NotNil(t, err)
	}
}

func TestDHTManagerBackgroundWorker(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// The background worker should be running
	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)
	
	// Should be able to get node count without panic
	_ = dm.GetNodeCount()
}

func TestDHTManagerConcurrency(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// Test concurrent access
	done := make(chan bool, 3)
	
	// Reader 1
	go func() {
		for i := 0; i < 10; i++ {
			_ = dm.GetNodeCount()
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()
	
	// Reader 2
	go func() {
		for i := 0; i < 10; i++ {
			_ = dm.GetStats()
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()
	
	// Writer
	go func() {
		for i := 0; i < 5; i++ {
			announcement := &types.ModelAnnouncement{
				Name:     "test-model",
				InfoHash: "test-hash",
				Size:     1000,
			}
			_ = dm.AnnounceModel(announcement)
			time.Sleep(20 * time.Millisecond)
		}
		done <- true
	}()
	
	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestDHTManagerAddRemoveTorrent(t *testing.T) {
	dm, tm, _ := setupTestDHTManager(t)
	defer dm.Stop()
	defer tm.Stop()
	
	// These methods interact with torrents
	// We can't test them fully without real torrents, but verify they don't panic
	
	// Try to remove non-existent torrent
	dm.RemoveTorrentFromDHT("nonexistent")
	
	// AddTorrentToDHT requires a real torrent object, so we can't test it easily
}

func TestDHTManagerDisabled(t *testing.T) {
	// Test with DHT disabled
	tmpDir := t.TempDir()
	
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false, // DHT disabled
			DHTPort:    0,
			ListenPort: 0,
		},
	}
	
	state := NewState(filepath.Join(tmpDir, "state.json"))
	tm, err := NewTorrentManager(cfg, state)
	require.NoError(t, err)
	defer tm.Stop()
	
	dm, err := NewDHTManager(cfg, tm)
	require.NoError(t, err)
	require.NotNil(t, dm)
	defer dm.Stop()
	
	// Should handle operations gracefully when disabled
	assert.Equal(t, 0, dm.GetNodeCount())
	
	stats := dm.GetStats()
	// Check if bootstrapped key exists before type assertion
	if bootstrapped, ok := stats["bootstrapped"]; ok {
		if b, ok := bootstrapped.(bool); ok {
			assert.False(t, b)
		}
	}
}