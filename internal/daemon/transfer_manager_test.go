package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferManagerCreate(t *testing.T) {
	// Create mock state
	state := NewState("")
	
	// Create mock torrent manager (nil is ok for these tests)
	tm := NewTransferManager(nil, state)
	assert.NotNil(t, tm)
	assert.NotNil(t, tm.transfers)
}

func TestTransferManagerCreateDownload(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create download transfer
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	assert.NotNil(t, transfer)
	assert.NotEmpty(t, transfer.ID)
	assert.Equal(t, TransferTypeDownload, transfer.Type)
	assert.Equal(t, TransferStatusPending, transfer.Status)
	assert.Equal(t, "test-model", transfer.ModelName)
	assert.Equal(t, "test-hash", transfer.InfoHash)
	assert.Equal(t, int64(1000000), transfer.TotalBytes)

	// Verify it was added to manager
	retrieved, exists := tm.GetTransfer(transfer.ID)
	assert.True(t, exists)
	assert.Equal(t, transfer, retrieved)
}

func TestTransferManagerCreateUpload(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create upload transfer
	transfer := tm.CreateUpload("test-model", "test-hash")
	assert.NotNil(t, transfer)
	assert.NotEmpty(t, transfer.ID)
	assert.Equal(t, TransferTypeUpload, transfer.Type)
	assert.Equal(t, TransferStatusActive, transfer.Status)
	assert.Equal(t, "test-model", transfer.ModelName)
	assert.Equal(t, "test-hash", transfer.InfoHash)
}

func TestTransferManagerCreateSeed(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create seed transfer
	transfer := tm.CreateSeed("test-model", "test-hash")
	assert.NotNil(t, transfer)
	assert.NotEmpty(t, transfer.ID)
	assert.Equal(t, TransferTypeSeed, transfer.Type)
	assert.Equal(t, TransferStatusActive, transfer.Status)
	assert.Equal(t, "test-model", transfer.ModelName)
	assert.Equal(t, "test-hash", transfer.InfoHash)
}

func TestTransferManagerGetTransfer(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create transfer
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)

	// Get existing transfer
	retrieved, exists := tm.GetTransfer(transfer.ID)
	assert.True(t, exists)
	assert.Equal(t, transfer, retrieved)

	// Get non-existent transfer
	_, exists = tm.GetTransfer("non-existent")
	assert.False(t, exists)
}

func TestTransferManagerGetAllTransfers(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create multiple transfers
	t1 := tm.CreateDownload("model1", "hash1", 1000)
	t2 := tm.CreateUpload("model2", "hash2")
	t3 := tm.CreateSeed("model3", "hash3")

	// Get all transfers
	all := tm.GetAllTransfers()
	assert.Len(t, all, 3)

	// Verify all transfers are included
	ids := make(map[string]bool)
	for _, transfer := range all {
		ids[transfer.ID] = true
	}
	assert.True(t, ids[t1.ID])
	assert.True(t, ids[t2.ID])
	assert.True(t, ids[t3.ID])
}

func TestTransferManagerGetActiveTransfers(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create transfers with different statuses
	t1 := tm.CreateDownload("model1", "hash1", 1000)
	t1.Status = TransferStatusActive
	
	t2 := tm.CreateUpload("model2", "hash2") // Already active
	
	t3 := tm.CreateDownload("model3", "hash3", 1000)
	t3.Status = TransferStatusPaused
	
	t4 := tm.CreateDownload("model4", "hash4", 1000)
	t4.Status = TransferStatusCompleted

	// Get active transfers
	active := tm.GetActiveTransfers()
	assert.Len(t, active, 2)

	// Verify only active transfers are included
	activeIDs := make(map[string]bool)
	for _, transfer := range active {
		activeIDs[transfer.ID] = true
	}
	assert.True(t, activeIDs[t1.ID])
	assert.True(t, activeIDs[t2.ID])
	assert.False(t, activeIDs[t3.ID])
	assert.False(t, activeIDs[t4.ID])
}

func TestTransferManagerGetIncompleteTransfers(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create transfers with different statuses
	t1 := tm.CreateDownload("model1", "hash1", 1000)
	t1.Status = TransferStatusActive
	
	t2 := tm.CreateDownload("model2", "hash2", 1000)
	t2.Status = TransferStatusPaused
	
	t3 := tm.CreateDownload("model3", "hash3", 1000)
	t3.Status = TransferStatusCompleted
	
	t4 := tm.CreateDownload("model4", "hash4", 1000)
	t4.Status = TransferStatusCancelled

	// Get incomplete transfers
	incomplete := tm.GetIncompleteTransfers()
	assert.Len(t, incomplete, 2)

	// Verify only incomplete transfers are included
	incompleteIDs := make(map[string]bool)
	for _, transfer := range incomplete {
		incompleteIDs[transfer.ID] = true
	}
	assert.True(t, incompleteIDs[t1.ID])
	assert.True(t, incompleteIDs[t2.ID])
	assert.False(t, incompleteIDs[t3.ID])
	assert.False(t, incompleteIDs[t4.ID])
}

func TestTransferManagerPauseResume(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state) // nil torrent manager will cause pause/resume to skip torrent operations

	// Create active transfer
	transfer := tm.CreateDownload("model", "hash", 1000)
	transfer.Status = TransferStatusActive

	// Pause transfer
	err := tm.PauseTransfer(transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, TransferStatusPaused, transfer.Status)

	// Try to pause already paused transfer
	err = tm.PauseTransfer(transfer.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not active")

	// Resume transfer
	err = tm.ResumeTransfer(transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, TransferStatusActive, transfer.Status)

	// Try to resume already active transfer
	err = tm.ResumeTransfer(transfer.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not paused")

	// Try to pause non-existent transfer
	err = tm.PauseTransfer("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Try to resume non-existent transfer
	err = tm.ResumeTransfer("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTransferManagerCancelTransfer(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state) // nil torrent manager will cause cancel to skip torrent operations

	// Create transfer
	transfer := tm.CreateDownload("model", "hash", 1000)

	// Cancel transfer
	err := tm.CancelTransfer(transfer.ID)
	require.NoError(t, err)
	assert.Equal(t, TransferStatusCancelled, transfer.Status)

	// Try to cancel non-existent transfer
	err = tm.CancelTransfer("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTransferManagerGetActiveCount(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Initially no active transfers
	assert.Equal(t, 0, tm.GetActiveCount())

	// Create active transfer
	t1 := tm.CreateUpload("model1", "hash1") // Upload starts active
	assert.Equal(t, 1, tm.GetActiveCount())

	// Create inactive transfer
	t2 := tm.CreateDownload("model2", "hash2", 1000) // Download starts pending
	assert.Equal(t, 1, tm.GetActiveCount())

	// Activate the download
	t2.Status = TransferStatusActive
	assert.Equal(t, 2, tm.GetActiveCount())

	// Complete one transfer
	t1.Status = TransferStatusCompleted
	assert.Equal(t, 1, tm.GetActiveCount())
}

func TestTransferManagerGetTransferByInfoHash(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create transfers
	t1 := tm.CreateDownload("model1", "hash1", 1000)
	t2 := tm.CreateDownload("model2", "hash2", 2000)

	// Get by info hash
	found, exists := tm.GetTransferByInfoHash("hash1")
	assert.True(t, exists)
	assert.Equal(t, t1, found)

	found, exists = tm.GetTransferByInfoHash("hash2")
	assert.True(t, exists)
	assert.Equal(t, t2, found)

	// Non-existent hash
	_, exists = tm.GetTransferByInfoHash("non-existent")
	assert.False(t, exists)
}

func TestTransferManagerCleanupOldTransfers(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Create old completed transfer
	oldTransfer := tm.CreateDownload("old", "hash1", 1000)
	oldTransfer.Status = TransferStatusCompleted
	oldTime := time.Now().Add(-25 * time.Hour)
	oldTransfer.CompletedAt = &oldTime

	// Create recent completed transfer
	recentTransfer := tm.CreateDownload("recent", "hash2", 1000)
	recentTransfer.Status = TransferStatusCompleted
	recentTime := time.Now().Add(-1 * time.Hour)
	recentTransfer.CompletedAt = &recentTime

	// Create active transfer
	activeTransfer := tm.CreateDownload("active", "hash3", 1000)
	activeTransfer.Status = TransferStatusActive

	// Cleanup old transfers (older than 24 hours)
	tm.CleanupOldTransfers(24 * time.Hour)

	// Old completed transfer should be removed
	_, exists := tm.GetTransfer(oldTransfer.ID)
	assert.False(t, exists)

	// Recent and active transfers should remain
	_, exists = tm.GetTransfer(recentTransfer.ID)
	assert.True(t, exists)

	_, exists = tm.GetTransfer(activeTransfer.ID)
	assert.True(t, exists)
}

func TestTransferManagerUpdateStats(t *testing.T) {
	state := NewState("")
	
	// We need a mock torrent manager for this test
	// Since we can't easily mock the real torrent manager,
	// we'll just test that UpdateStats doesn't panic with nil
	tm := NewTransferManager(nil, state)

	// Create transfer
	transfer := tm.CreateDownload("model", "hash", 1000000)
	transfer.Status = TransferStatusActive

	// UpdateStats should handle nil torrent manager gracefully
	tm.UpdateStats()

	// Transfer should still exist
	_, exists := tm.GetTransfer(transfer.ID)
	assert.True(t, exists)
}

func TestTransferManagerConcurrency(t *testing.T) {
	state := NewState("")
	tm := NewTransferManager(nil, state)

	// Concurrent operations should not panic or deadlock
	done := make(chan bool)

	// Writer 1: Create downloads
	go func() {
		for i := 0; i < 10; i++ {
			tm.CreateDownload(fmt.Sprintf("model%d", i), fmt.Sprintf("hash%d", i), int64(i*1000))
		}
		done <- true
	}()

	// Writer 2: Create uploads
	go func() {
		for i := 10; i < 20; i++ {
			tm.CreateUpload(fmt.Sprintf("model%d", i), fmt.Sprintf("hash%d", i))
		}
		done <- true
	}()

	// Reader 1: Get all transfers
	go func() {
		for i := 0; i < 10; i++ {
			_ = tm.GetAllTransfers()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Reader 2: Get active count
	go func() {
		for i := 0; i < 10; i++ {
			_ = tm.GetActiveCount()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify we have all transfers
	all := tm.GetAllTransfers()
	assert.Len(t, all, 20)
}