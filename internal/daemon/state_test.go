package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateNewState(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)
	assert.NotNil(t, s)
	assert.Equal(t, stateFile, s.filePath)
	assert.NotNil(t, s.ActiveTorrents)
	assert.NotNil(t, s.Transfers)
}

func TestStateSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	// Create state with some data
	s := NewState(stateFile)
	s.AddTorrent("test-hash", "test-model", time.Now(), true)
	s.Statistics.TotalDownloaded = 1000
	s.Statistics.TotalUploaded = 500

	// Save state
	err := s.Save()
	require.NoError(t, err)
	
	// Check file exists
	_, err = os.Stat(stateFile)
	require.NoError(t, err)

	// Create new state and load
	s2 := NewState(stateFile)
	err = s2.Load()
	require.NoError(t, err)

	// Verify data was loaded
	assert.Len(t, s2.ActiveTorrents, 1)
	assert.Equal(t, "test-hash", s2.ActiveTorrents[0].InfoHash)
	assert.Equal(t, "test-model", s2.ActiveTorrents[0].Name)
	assert.True(t, s2.ActiveTorrents[0].Seeding)
	assert.Equal(t, int64(1000), s2.Statistics.TotalDownloaded)
	assert.Equal(t, int64(500), s2.Statistics.TotalUploaded)
}

func TestStateLoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "nonexistent.json")

	s := NewState(stateFile)
	err := s.Load()
	assert.NoError(t, err) // Should not error on missing file
	assert.Equal(t, 1, s.Statistics.DaemonStartCount)
}

func TestStateAddRemoveTorrent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)

	// Add torrent
	s.AddTorrent("hash1", "model1", time.Now(), false)
	assert.Len(t, s.ActiveTorrents, 1)

	// Add another torrent
	s.AddTorrent("hash2", "model2", time.Now(), true)
	assert.Len(t, s.ActiveTorrents, 2)

	// Update existing torrent
	s.AddTorrent("hash1", "model1-updated", time.Now(), true)
	assert.Len(t, s.ActiveTorrents, 2)
	assert.Equal(t, "model1-updated", s.ActiveTorrents[0].Name)
	assert.True(t, s.ActiveTorrents[0].Seeding)

	// Remove torrent
	s.RemoveTorrent("hash1")
	assert.Len(t, s.ActiveTorrents, 1)
	assert.Equal(t, "hash2", s.ActiveTorrents[0].InfoHash)

	// Remove non-existent torrent (should not panic)
	s.RemoveTorrent("nonexistent")
	assert.Len(t, s.ActiveTorrents, 1)
}

func TestStateSetTorrentSeeding(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)
	s.AddTorrent("hash1", "model1", time.Now(), false)

	// Set seeding
	s.SetTorrentSeeding("hash1", true)
	assert.True(t, s.ActiveTorrents[0].Seeding)

	// Set not seeding
	s.SetTorrentSeeding("hash1", false)
	assert.False(t, s.ActiveTorrents[0].Seeding)

	// Set for non-existent (should not panic)
	s.SetTorrentSeeding("nonexistent", true)
}

func TestStateUpdateTorrentStats(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)
	s.AddTorrent("hash1", "model1", time.Now(), false)

	// Update stats
	s.UpdateTorrentStats("hash1", 1000, 500)
	assert.Equal(t, int64(1000), s.ActiveTorrents[0].BytesDown)
	assert.Equal(t, int64(500), s.ActiveTorrents[0].BytesUp)
	assert.Equal(t, int64(1000), s.Statistics.TotalDownloaded)
	assert.Equal(t, int64(500), s.Statistics.TotalUploaded)

	// Update again (should track deltas)
	s.UpdateTorrentStats("hash1", 2000, 1000)
	assert.Equal(t, int64(2000), s.ActiveTorrents[0].BytesDown)
	assert.Equal(t, int64(1000), s.ActiveTorrents[0].BytesUp)
	assert.Equal(t, int64(2000), s.Statistics.TotalDownloaded)
	assert.Equal(t, int64(1000), s.Statistics.TotalUploaded)
}

func TestStateSetTorrentCompleted(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)
	s.AddTorrent("hash1", "model1", time.Now(), false)

	// Set completed
	s.SetTorrentCompleted("hash1")
	assert.NotNil(t, s.ActiveTorrents[0].CompletedAt)
	assert.Equal(t, 1, s.Statistics.TotalModelsDownloaded)

	// Set for non-existent (should not panic)
	s.SetTorrentCompleted("nonexistent")
	assert.Equal(t, 1, s.Statistics.TotalModelsDownloaded)
}

func TestStateTransfers(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)

	// Add transfer
	transfer := &Transfer{
		ID:       "transfer1",
		Type:     TransferTypeDownload,
		Status:   TransferStatusActive,
		ModelName: "test-model",
	}
	s.AddTransfer(transfer)
	assert.Len(t, s.Transfers, 1)

	// Update transfer status
	s.UpdateTransferStatus("transfer1", TransferStatusCompleted)
	assert.Equal(t, TransferStatusCompleted, s.Transfers["transfer1"].Status)
	assert.NotNil(t, s.Transfers["transfer1"].CompletedAt)

	// Update non-existent transfer (should not panic)
	s.UpdateTransferStatus("nonexistent", TransferStatusFailed)

	// Remove transfer
	s.RemoveTransfer("transfer1")
	assert.Len(t, s.Transfers, 0)
}

func TestStateCleanupOldTransfers(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)

	// Add old completed transfer
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	oldTransfer := &Transfer{
		ID:          "old",
		Status:      TransferStatusCompleted,
		CompletedAt: &oldTime,
	}
	s.AddTransfer(oldTransfer)

	// Add recent completed transfer
	recentTime := time.Now().Add(-1 * time.Hour)
	recentTransfer := &Transfer{
		ID:          "recent",
		Status:      TransferStatusCompleted,
		CompletedAt: &recentTime,
	}
	s.AddTransfer(recentTransfer)

	// Add active transfer
	activeTransfer := &Transfer{
		ID:     "active",
		Status: TransferStatusActive,
	}
	s.AddTransfer(activeTransfer)

	// Cleanup should only remove old completed transfer
	s.cleanupOldTransfers()
	assert.Len(t, s.Transfers, 2)
	assert.Contains(t, s.Transfers, "recent")
	assert.Contains(t, s.Transfers, "active")
	assert.NotContains(t, s.Transfers, "old")
}

func TestStateStatistics(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)

	// Get initial statistics
	stats := s.GetStatistics()
	assert.Equal(t, int64(0), stats.TotalDownloaded)
	assert.Equal(t, int64(0), stats.TotalUploaded)
	assert.Equal(t, 0, stats.TotalModelsShared)

	// Increment models shared
	s.IncrementModelsShared()
	stats = s.GetStatistics()
	assert.Equal(t, 1, stats.TotalModelsShared)
}

func TestStateConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	s := NewState(stateFile)

	// Concurrent operations should not panic
	done := make(chan bool)
	
	// Writer 1: Add torrents
	go func() {
		for i := 0; i < 10; i++ {
			s.AddTorrent(fmt.Sprintf("hash%d", i), fmt.Sprintf("model%d", i), time.Now(), false)
		}
		done <- true
	}()

	// Writer 2: Update stats
	go func() {
		for i := 0; i < 10; i++ {
			s.IncrementModelsShared()
		}
		done <- true
	}()

	// Reader: Get statistics
	go func() {
		for i := 0; i < 10; i++ {
			_ = s.GetStatistics()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify final state
	assert.Equal(t, 10, s.Statistics.TotalModelsShared)
	assert.Len(t, s.ActiveTorrents, 10)
}