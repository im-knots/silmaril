package daemon

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	torrentStorage "github.com/anacrolix/torrent/storage"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/storage"
	torrentclient "github.com/silmaril/silmaril/internal/torrent"
)

type TorrentManager struct {
	mu       sync.RWMutex
	client   *torrent.Client
	config   *config.Config
	state    *State
	torrents map[string]*ManagedTorrent
}

type ManagedTorrent struct {
	InfoHash    string
	Name        string
	Torrent     *torrent.Torrent
	AddedAt     time.Time
	CompletedAt *time.Time
	BytesDown   int64
	BytesUp     int64
	Seeding     bool
}

func NewTorrentManager(cfg *config.Config, state *State) (*TorrentManager, error) {
	// Create a persistent torrent client
	clientCfg := torrent.NewDefaultClientConfig()
	// Don't set a global DataDir - we'll use custom storage for each torrent
	clientCfg.DisableTrackers = cfg.GetBool("network.disable_trackers")
	clientCfg.DisableWebtorrent = cfg.GetBool("network.disable_webtorrent")
	clientCfg.DisablePEX = cfg.GetBool("network.disable_pex")
	clientCfg.ListenPort = cfg.GetInt("network.listen_port")
	clientCfg.Seed = true
	
	// Set rate limits
	if uploadLimit := cfg.GetInt("network.upload_rate_limit"); uploadLimit > 0 {
		clientCfg.UploadRateLimiter = torrentclient.NewRateLimiter(int64(uploadLimit))
	}
	if downloadLimit := cfg.GetInt("network.download_rate_limit"); downloadLimit > 0 {
		clientCfg.DownloadRateLimiter = torrentclient.NewRateLimiter(int64(downloadLimit))
	}

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}

	tm := &TorrentManager{
		client:   client,
		config:   cfg,
		state:    state,
		torrents: make(map[string]*ManagedTorrent),
	}

	// Restore previous torrents from state
	if err := tm.restoreTorrents(); err != nil {
		fmt.Printf("Warning: could not restore torrents: %v\n", err)
	}

	return tm, nil
}

func (tm *TorrentManager) restoreTorrents() error {
	torrentsDir := storage.GetTorrentsDir()
	modelsDir := storage.GetModelsDir()
	
	// Load all torrents that were active in the previous session
	for _, torrentInfo := range tm.state.ActiveTorrents {
		torrentPath := filepath.Join(torrentsDir, torrentInfo.InfoHash+".torrent")
		
		// Load torrent metainfo
		mi, err := metainfo.LoadFromFile(torrentPath)
		if err != nil {
			fmt.Printf("Failed to load torrent metainfo %s: %v\n", torrentInfo.Name, err)
			continue
		}

		// Determine storage path based on torrent name
		storagePath := filepath.Join(modelsDir, torrentInfo.Name)
		
		// Create custom storage pointing to the specific directory
		customStorage := torrentStorage.NewFileOpts(torrentStorage.NewFileClientOpts{
			ClientBaseDir: storagePath,
			TorrentDirMaker: func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
				// Return the base dir itself
				return baseDir
			},
		})

		// Add torrent with custom storage
		t, _ := tm.client.AddTorrentOpt(torrent.AddTorrentOpts{
			InfoHash:  mi.HashInfoBytes(),
			Storage:   customStorage,
			InfoBytes: mi.InfoBytes,
		})

		if t == nil {
			fmt.Printf("Failed to restore torrent %s\n", torrentInfo.Name)
			continue
		}

		// Start downloading/seeding
		t.DownloadAll()
		
		mt := &ManagedTorrent{
			InfoHash: torrentInfo.InfoHash,
			Name:     torrentInfo.Name,
			Torrent:  t,
			AddedAt:  torrentInfo.AddedAt,
			Seeding:  torrentInfo.Seeding,
		}
		
		if torrentInfo.CompletedAt != nil {
			mt.CompletedAt = torrentInfo.CompletedAt
		}
		
		tm.torrents[torrentInfo.InfoHash] = mt
		fmt.Printf("Restored torrent: %s (seeding: %v)\n", torrentInfo.Name, torrentInfo.Seeding)
	}
	
	return nil
}

// AddTorrentForSeeding adds a torrent specifically for seeding (sharing models)
// storagePath is the directory where the torrent's files are located
func (tm *TorrentManager) AddTorrentForSeeding(torrentPath string, name string, storagePath string) (*ManagedTorrent, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	fmt.Printf("[TorrentManager] Adding torrent for seeding: %s from %s\n", name, storagePath)

	// Load torrent metainfo
	mi, err := metainfo.LoadFromFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load torrent metainfo: %w", err)
	}

	// Create custom storage pointing to the specific directory
	customStorage := torrentStorage.NewFileOpts(torrentStorage.NewFileClientOpts{
		ClientBaseDir: storagePath,
		TorrentDirMaker: func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
			// Return the base dir itself since files are already in the right place
			return baseDir
		},
	})

	// Add torrent with custom storage
	t, isNew := tm.client.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash:  mi.HashInfoBytes(),
		Storage:   customStorage,
		InfoBytes: mi.InfoBytes,
	})

	if t == nil {
		return nil, fmt.Errorf("failed to add torrent to client")
	}

	fmt.Printf("[TorrentManager] Torrent added to client (new: %v)\n", isNew)

	// For seeding, we call DownloadAll() to verify existing pieces
	// The torrent client will automatically start seeding once verification is complete
	t.DownloadAll()

	mt := &ManagedTorrent{
		InfoHash: t.InfoHash().String(),
		Name:     name,
		Torrent:  t,
		AddedAt:  time.Now(),
		Seeding:  true, // Explicitly mark as seeding
	}

	tm.torrents[mt.InfoHash] = mt
	
	// Update state
	tm.state.AddTorrent(mt.InfoHash, name, mt.AddedAt, true)
	
	fmt.Printf("[TorrentManager] Torrent added for seeding: %s (InfoHash: %s)\n", name, mt.InfoHash)
	return mt, nil
}

// AddTorrentForDownload adds a torrent specifically for downloading (getting models)
// storagePath is the directory where the torrent's files will be downloaded to
func (tm *TorrentManager) AddTorrentForDownload(torrentPath string, name string, storagePath string) (*ManagedTorrent, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	fmt.Printf("[TorrentManager] Adding torrent for download: %s to %s\n", name, storagePath)

	// Load torrent metainfo
	mi, err := metainfo.LoadFromFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load torrent metainfo: %w", err)
	}

	// Create custom storage pointing to the specific directory
	customStorage := torrentStorage.NewFileOpts(torrentStorage.NewFileClientOpts{
		ClientBaseDir: storagePath,
		TorrentDirMaker: func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
			// Return the base dir itself to download files directly there
			return baseDir
		},
	})

	// Add torrent with custom storage
	t, isNew := tm.client.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash:  mi.HashInfoBytes(),
		Storage:   customStorage,
		InfoBytes: mi.InfoBytes,
	})

	if t == nil {
		return nil, fmt.Errorf("failed to add torrent to client")
	}

	fmt.Printf("[TorrentManager] Torrent added to client (new: %v)\n", isNew)

	// Start downloading
	t.DownloadAll()

	mt := &ManagedTorrent{
		InfoHash: t.InfoHash().String(),
		Name:     name,
		Torrent:  t,
		AddedAt:  time.Now(),
		Seeding:  false, // Explicitly mark as downloading
	}

	tm.torrents[mt.InfoHash] = mt
	
	// Update state
	tm.state.AddTorrent(mt.InfoHash, name, mt.AddedAt, false)
	
	fmt.Printf("[TorrentManager] Torrent added for download: %s (InfoHash: %s)\n", name, mt.InfoHash)
	return mt, nil
}


func (tm *TorrentManager) RemoveTorrent(infoHash string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	mt, exists := tm.torrents[infoHash]
	if !exists {
		return fmt.Errorf("torrent not found: %s", infoHash)
	}

	mt.Torrent.Drop()
	delete(tm.torrents, infoHash)
	
	// Update state
	tm.state.RemoveTorrent(infoHash)
	
	return nil
}

func (tm *TorrentManager) GetTorrent(infoHash string) (*ManagedTorrent, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	mt, exists := tm.torrents[infoHash]
	return mt, exists
}

func (tm *TorrentManager) GetAllTorrents() []*ManagedTorrent {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	torrents := make([]*ManagedTorrent, 0, len(tm.torrents))
	for _, mt := range tm.torrents {
		torrents = append(torrents, mt)
	}
	return torrents
}

func (tm *TorrentManager) StartSeeding(infoHash string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	mt, exists := tm.torrents[infoHash]
	if !exists {
		return fmt.Errorf("torrent not found: %s", infoHash)
	}

	mt.Seeding = true
	tm.state.SetTorrentSeeding(infoHash, true)
	
	return nil
}

func (tm *TorrentManager) StopSeeding(infoHash string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	mt, exists := tm.torrents[infoHash]
	if !exists {
		return fmt.Errorf("torrent not found: %s", infoHash)
	}

	mt.Seeding = false
	tm.state.SetTorrentSeeding(infoHash, false)
	
	// Pause the torrent
	mt.Torrent.DisallowDataDownload()
	mt.Torrent.DisallowDataUpload()
	
	return nil
}

func (tm *TorrentManager) GetManagedTorrent(infoHash string) *ManagedTorrent {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.torrents[infoHash]
}

func (tm *TorrentManager) GetStats(infoHash string) (map[string]interface{}, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	mt, exists := tm.torrents[infoHash]
	if !exists {
		return nil, fmt.Errorf("torrent not found: %s", infoHash)
	}

	stats := mt.Torrent.Stats()
	peers := mt.Torrent.KnownSwarm()
	
	return map[string]interface{}{
		"name":             mt.Name,
		"info_hash":        mt.InfoHash,
		"seeding":          mt.Seeding,
		"bytes_downloaded": stats.BytesReadData.Int64(),
		"bytes_uploaded":   stats.BytesWrittenData.Int64(),
		"peers":            len(peers),
		"seeders":          stats.ConnectedSeeders,
		"leechers":         len(peers) - stats.ConnectedSeeders,
		"progress":         mt.Torrent.BytesCompleted() * 100 / mt.Torrent.Length(),
		"download_rate":    stats.BytesReadData.Int64() / int64(time.Since(mt.AddedAt).Seconds()),
		"upload_rate":      stats.BytesWrittenData.Int64() / int64(time.Since(mt.AddedAt).Seconds()),
	}, nil
}

func (tm *TorrentManager) GetTotalPeers() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	totalPeers := 0
	for _, mt := range tm.torrents {
		peers := mt.Torrent.KnownSwarm()
		totalPeers += len(peers)
	}
	return totalPeers
}

func (tm *TorrentManager) Stop() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Save final stats for all torrents
	for _, mt := range tm.torrents {
		stats := mt.Torrent.Stats()
		mt.BytesDown = stats.BytesReadData.Int64()
		mt.BytesUp = stats.BytesWrittenData.Int64()
		
		// Update state with final stats
		tm.state.UpdateTorrentStats(mt.InfoHash, mt.BytesDown, mt.BytesUp)
	}

	// Close the torrent client
	tm.client.Close()
}

// GetClient returns the underlying torrent client (for DHT manager)
func (tm *TorrentManager) GetClient() *torrent.Client {
	return tm.client
}

// GetSeedingModels returns a list of currently seeded models
func (tm *TorrentManager) GetSeedingModels() []*ManagedTorrent {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	var seedingTorrents []*ManagedTorrent
	for _, mt := range tm.torrents {
		if mt.Seeding {
			seedingTorrents = append(seedingTorrents, mt)
		}
	}
	return seedingTorrents
}