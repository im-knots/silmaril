package daemon

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type TransferType string

const (
	TransferTypeDownload TransferType = "download"
	TransferTypeUpload   TransferType = "upload"
	TransferTypeSeed     TransferType = "seed"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "pending"
	TransferStatusActive    TransferStatus = "active"
	TransferStatusPaused    TransferStatus = "paused"
	TransferStatusCompleted TransferStatus = "completed"
	TransferStatusFailed    TransferStatus = "failed"
	TransferStatusCancelled TransferStatus = "cancelled"
)

type Transfer struct {
	ID           string         `json:"id"`
	Type         TransferType   `json:"type"`
	Status       TransferStatus `json:"status"`
	ModelName    string         `json:"model_name"`
	InfoHash     string         `json:"info_hash"`
	TotalBytes   int64          `json:"total_bytes"`
	BytesTransferred int64     `json:"bytes_transferred"`
	Progress     float64        `json:"progress"`
	DownloadRate int64          `json:"download_rate"`
	UploadRate   int64          `json:"upload_rate"`
	Peers        int            `json:"peers"`
	Seeders      int            `json:"seeders"`
	ETA          *time.Duration `json:"eta,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	LastActivity time.Time      `json:"last_activity"`
	Error        string         `json:"error,omitempty"`
}

type TransferManager struct {
	mu             sync.RWMutex
	torrentManager *TorrentManager
	state          *State
	transfers      map[string]*Transfer
}

func NewTransferManager(tm *TorrentManager, state *State) *TransferManager {
	return &TransferManager{
		torrentManager: tm,
		state:          state,
		transfers:      make(map[string]*Transfer),
	}
}

func (tm *TransferManager) CreateDownload(modelName, infoHash string, totalBytes int64) *Transfer {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer := &Transfer{
		ID:           uuid.New().String(),
		Type:         TransferTypeDownload,
		Status:       TransferStatusPending,
		ModelName:    modelName,
		InfoHash:     infoHash,
		TotalBytes:   totalBytes,
		StartedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	tm.transfers[transfer.ID] = transfer
	tm.state.AddTransfer(transfer)
	
	return transfer
}

func (tm *TransferManager) CreateUpload(modelName, infoHash string) *Transfer {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer := &Transfer{
		ID:           uuid.New().String(),
		Type:         TransferTypeUpload,
		Status:       TransferStatusActive,
		ModelName:    modelName,
		InfoHash:     infoHash,
		StartedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	tm.transfers[transfer.ID] = transfer
	tm.state.AddTransfer(transfer)
	
	return transfer
}

func (tm *TransferManager) CreateSeed(modelName, infoHash string) *Transfer {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer := &Transfer{
		ID:           uuid.New().String(),
		Type:         TransferTypeSeed,
		Status:       TransferStatusActive,
		ModelName:    modelName,
		InfoHash:     infoHash,
		StartedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	tm.transfers[transfer.ID] = transfer
	tm.state.AddTransfer(transfer)
	
	return transfer
}

func (tm *TransferManager) UpdateStats() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, transfer := range tm.transfers {
		if transfer.Status != TransferStatusActive {
			continue
		}

		// Get stats from torrent manager (if available)
		if tm.torrentManager == nil {
			continue
		}
		
		stats, err := tm.torrentManager.GetStats(transfer.InfoHash)
		if err != nil {
			continue
		}

		// Update transfer stats
		transfer.BytesTransferred = stats["bytes_downloaded"].(int64)
		if transfer.Type == TransferTypeUpload || transfer.Type == TransferTypeSeed {
			transfer.BytesTransferred = stats["bytes_uploaded"].(int64)
		}
		
		transfer.Progress = stats["progress"].(float64)
		transfer.DownloadRate = stats["download_rate"].(int64)
		transfer.UploadRate = stats["upload_rate"].(int64)
		transfer.Peers = stats["peers"].(int)
		transfer.Seeders = stats["seeders"].(int)
		transfer.LastActivity = time.Now()

		// Calculate ETA for downloads
		if transfer.Type == TransferTypeDownload && transfer.DownloadRate > 0 {
			remainingBytes := transfer.TotalBytes - transfer.BytesTransferred
			etaSeconds := remainingBytes / transfer.DownloadRate
			eta := time.Duration(etaSeconds) * time.Second
			transfer.ETA = &eta
		}

		// Check if download is complete
		if transfer.Type == TransferTypeDownload && transfer.Progress >= 100 {
			transfer.Status = TransferStatusCompleted
			now := time.Now()
			transfer.CompletedAt = &now
			transfer.ETA = nil
		}
	}

	// Update state
	tm.state.UpdateTransfers(tm.transfers)
}

func (tm *TransferManager) GetTransfer(id string) (*Transfer, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	transfer, exists := tm.transfers[id]
	return transfer, exists
}

func (tm *TransferManager) GetAllTransfers() []*Transfer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	transfers := make([]*Transfer, 0, len(tm.transfers))
	for _, t := range tm.transfers {
		transfers = append(transfers, t)
	}
	return transfers
}

func (tm *TransferManager) GetActiveTransfers() []*Transfer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	transfers := make([]*Transfer, 0)
	for _, t := range tm.transfers {
		if t.Status == TransferStatusActive {
			transfers = append(transfers, t)
		}
	}
	return transfers
}

func (tm *TransferManager) GetIncompleteTransfers() []*Transfer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	transfers := make([]*Transfer, 0)
	for _, t := range tm.transfers {
		if t.Status != TransferStatusCompleted && t.Status != TransferStatusCancelled {
			transfers = append(transfers, t)
		}
	}
	return transfers
}

func (tm *TransferManager) PauseTransfer(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, exists := tm.transfers[id]
	if !exists {
		return fmt.Errorf("transfer not found: %s", id)
	}

	if transfer.Status != TransferStatusActive {
		return fmt.Errorf("transfer is not active")
	}

	transfer.Status = TransferStatusPaused
	tm.state.UpdateTransferStatus(id, TransferStatusPaused)
	
	// Pause in torrent manager (if available)
	if tm.torrentManager != nil {
		if mt, exists := tm.torrentManager.GetTorrent(transfer.InfoHash); exists {
			mt.Torrent.DisallowDataDownload()
		}
	}
	
	return nil
}

func (tm *TransferManager) ResumeTransfer(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, exists := tm.transfers[id]
	if !exists {
		return fmt.Errorf("transfer not found: %s", id)
	}

	if transfer.Status != TransferStatusPaused {
		return fmt.Errorf("transfer is not paused")
	}

	transfer.Status = TransferStatusActive
	tm.state.UpdateTransferStatus(id, TransferStatusActive)
	
	// Resume in torrent manager (if available)
	if tm.torrentManager != nil {
		if mt, exists := tm.torrentManager.GetTorrent(transfer.InfoHash); exists {
			mt.Torrent.AllowDataDownload()
		}
	}
	
	return nil
}

func (tm *TransferManager) CancelTransfer(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	transfer, exists := tm.transfers[id]
	if !exists {
		return fmt.Errorf("transfer not found: %s", id)
	}

	transfer.Status = TransferStatusCancelled
	tm.state.UpdateTransferStatus(id, TransferStatusCancelled)
	
	// Remove from torrent manager (if available)
	if tm.torrentManager != nil {
		if err := tm.torrentManager.RemoveTorrent(transfer.InfoHash); err != nil {
			return fmt.Errorf("failed to remove torrent: %w", err)
		}
	}
	
	return nil
}

func (tm *TransferManager) GetActiveCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	count := 0
	for _, t := range tm.transfers {
		if t.Status == TransferStatusActive {
			count++
		}
	}
	return count
}

func (tm *TransferManager) GetTransferByInfoHash(infoHash string) (*Transfer, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	for _, t := range tm.transfers {
		if t.InfoHash == infoHash {
			return t, true
		}
	}
	return nil, false
}

func (tm *TransferManager) CleanupOldTransfers(olderThan time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	
	cutoff := time.Now().Add(-olderThan)
	
	for id, transfer := range tm.transfers {
		// Only clean up completed or cancelled transfers
		if transfer.Status == TransferStatusCompleted || transfer.Status == TransferStatusCancelled {
			if transfer.CompletedAt != nil && transfer.CompletedAt.Before(cutoff) {
				delete(tm.transfers, id)
				tm.state.RemoveTransfer(id)
			}
		}
	}
}