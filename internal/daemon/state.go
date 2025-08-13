package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type State struct {
	mu              sync.RWMutex
	filePath        string
	StartTime       time.Time                  `json:"start_time"`
	ActiveTorrents  []TorrentState             `json:"active_torrents"`
	Transfers       map[string]*Transfer       `json:"transfers"`
	Statistics      Statistics                 `json:"statistics"`
	LastSave        time.Time                  `json:"last_save"`
}

type TorrentState struct {
	InfoHash      string     `json:"info_hash"`
	Name          string     `json:"name"`
	AddedAt       time.Time  `json:"added_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Seeding       bool       `json:"seeding"`
	BytesDown     int64      `json:"bytes_downloaded"`
	BytesUp       int64      `json:"bytes_uploaded"`
}

type Statistics struct {
	TotalDownloaded   int64     `json:"total_downloaded"`
	TotalUploaded     int64     `json:"total_uploaded"`
	TotalModelsShared int       `json:"total_models_shared"`
	TotalModelsDownloaded int   `json:"total_models_downloaded"`
	DaemonStartCount  int       `json:"daemon_start_count"`
	LastStartTime     time.Time `json:"last_start_time"`
}

func NewState(filePath string) *State {
	return &State{
		filePath:       filePath,
		StartTime:      time.Now(),
		ActiveTorrents: make([]TorrentState, 0),
		Transfers:      make(map[string]*Transfer),
		Statistics:     Statistics{},
	}
}

func (s *State) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No previous state, start fresh
			s.Statistics.DaemonStartCount = 1
			s.Statistics.LastStartTime = time.Now()
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var loadedState State
	if err := json.Unmarshal(data, &loadedState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Preserve current start time
	currentStartTime := s.StartTime
	
	// Copy loaded state
	s.ActiveTorrents = loadedState.ActiveTorrents
	s.Transfers = loadedState.Transfers
	s.Statistics = loadedState.Statistics
	
	// Update statistics
	s.StartTime = currentStartTime
	s.Statistics.DaemonStartCount++
	s.Statistics.LastStartTime = currentStartTime
	
	// Clean up old completed transfers
	s.cleanupOldTransfers()
	
	return nil
}

func (s *State) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.LastSave = time.Now()
	
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temporary file first
	tempFile := s.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	// Rename to final location (atomic operation)
	if err := os.Rename(tempFile, s.filePath); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

func (s *State) AddTorrent(infoHash, name string, addedAt time.Time, seeding bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already exists
	for i, t := range s.ActiveTorrents {
		if t.InfoHash == infoHash {
			// Update existing
			s.ActiveTorrents[i].Name = name
			s.ActiveTorrents[i].Seeding = seeding
			return
		}
	}

	// Add new
	s.ActiveTorrents = append(s.ActiveTorrents, TorrentState{
		InfoHash: infoHash,
		Name:     name,
		AddedAt:  addedAt,
		Seeding:  seeding,
	})
}

func (s *State) RemoveTorrent(infoHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.ActiveTorrents {
		if t.InfoHash == infoHash {
			// Remove from slice
			s.ActiveTorrents = append(s.ActiveTorrents[:i], s.ActiveTorrents[i+1:]...)
			return
		}
	}
}

func (s *State) SetTorrentSeeding(infoHash string, seeding bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.ActiveTorrents {
		if t.InfoHash == infoHash {
			s.ActiveTorrents[i].Seeding = seeding
			return
		}
	}
}

func (s *State) UpdateTorrentStats(infoHash string, bytesDown, bytesUp int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.ActiveTorrents {
		if t.InfoHash == infoHash {
			s.ActiveTorrents[i].BytesDown = bytesDown
			s.ActiveTorrents[i].BytesUp = bytesUp
			
			// Update global statistics
			s.Statistics.TotalDownloaded += bytesDown - t.BytesDown
			s.Statistics.TotalUploaded += bytesUp - t.BytesUp
			return
		}
	}
}

func (s *State) SetTorrentCompleted(infoHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.ActiveTorrents {
		if t.InfoHash == infoHash {
			now := time.Now()
			s.ActiveTorrents[i].CompletedAt = &now
			s.Statistics.TotalModelsDownloaded++
			return
		}
	}
}

func (s *State) AddTransfer(transfer *Transfer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Transfers[transfer.ID] = transfer
}

func (s *State) UpdateTransfers(transfers map[string]*Transfer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Transfers = transfers
}

func (s *State) UpdateTransferStatus(id string, status TransferStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if transfer, exists := s.Transfers[id]; exists {
		transfer.Status = status
		if status == TransferStatusCompleted {
			now := time.Now()
			transfer.CompletedAt = &now
		}
	}
}

func (s *State) RemoveTransfer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Transfers, id)
}

func (s *State) cleanupOldTransfers() {
	cutoff := time.Now().Add(-7 * 24 * time.Hour) // Keep transfers for 7 days
	
	for id, transfer := range s.Transfers {
		if transfer.Status == TransferStatusCompleted || transfer.Status == TransferStatusCancelled {
			if transfer.CompletedAt != nil && transfer.CompletedAt.Before(cutoff) {
				delete(s.Transfers, id)
			}
		}
	}
}

func (s *State) GetStatistics() Statistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return s.Statistics
}

func (s *State) IncrementModelsShared() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.Statistics.TotalModelsShared++
}