package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	torrentStorage "github.com/anacrolix/torrent/storage"
	"github.com/silmaril/silmaril/internal/storage"
	torrentCreator "github.com/silmaril/silmaril/internal/torrent"
	"github.com/silmaril/silmaril/pkg/types"
)

// CatalogTorrent manages the catalog as a torrent file
type CatalogTorrent struct {
	mu sync.RWMutex
	
	// Paths
	catalogDir  string
	catalogFile string
	torrentFile string
	
	// Current catalog
	catalog     *ModelCatalog
	infoHash    string
	
	// Torrent client for downloading/seeding
	client      *torrent.Client
	torrent     *torrent.Torrent
}

// CatalogReference is what gets stored in BEP44
type CatalogReference struct {
	InfoHash  string `json:"infohash"`
	Sequence  int64  `json:"seq"`
	Updated   int64  `json:"updated"`
	Size      int64  `json:"size,omitempty"`
	Seeders   int    `json:"seeders,omitempty"`
}

// NewCatalogTorrent creates a new catalog torrent manager
func NewCatalogTorrent(torrentClient *torrent.Client) (*CatalogTorrent, error) {
	paths, err := storage.NewPaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get paths: %w", err)
	}
	
	// Create catalog directory
	catalogDir := filepath.Join(paths.BaseDir(), "catalog")
	if err := os.MkdirAll(catalogDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create catalog dir: %w", err)
	}
	
	ct := &CatalogTorrent{
		catalogDir:  catalogDir,
		catalogFile: filepath.Join(catalogDir, "catalog.json"),
		torrentFile: filepath.Join(catalogDir, "catalog.torrent"),
		client:      torrentClient,
		catalog: &ModelCatalog{
			Version: 1,
			Models:  make(map[string]ModelEntry),
		},
	}
	
	// Try to load existing catalog
	if err := ct.loadCatalog(); err != nil {
		fmt.Printf("[CatalogTorrent] No existing catalog found: %v\n", err)
	} else {
		// If we have a catalog, check if we have a torrent file to seed
		// Look for the most recent catalog torrent file
		files, err := os.ReadDir(catalogDir)
		if err == nil {
			var latestTorrent string
			var latestSeq int64
			for _, file := range files {
				if filepath.Ext(file.Name()) == ".torrent" && file.Name() != "catalog.torrent" {
					// Extract sequence number from filename like "catalog_1.torrent"
					var seq int64
					if _, err := fmt.Sscanf(file.Name(), "catalog_%d.torrent", &seq); err == nil {
						if seq > latestSeq {
							latestSeq = seq
							latestTorrent = filepath.Join(catalogDir, file.Name())
						}
					}
				}
			}
			
			if latestTorrent != "" {
				fmt.Printf("[CatalogTorrent] Found existing catalog torrent: %s (seq: %d)\n", latestTorrent, latestSeq)
				ct.torrentFile = latestTorrent
				// Start seeding the existing catalog
				if err := ct.StartSeeding(); err != nil {
					fmt.Printf("[CatalogTorrent] Warning: failed to start seeding existing catalog: %v\n", err)
				} else {
					fmt.Printf("[CatalogTorrent] Successfully started seeding existing catalog\n")
				}
			}
		}
	}
	
	return ct, nil
}

// LoadOrFetchCatalog loads local catalog or fetches from torrent network
func (ct *CatalogTorrent) LoadOrFetchCatalog(infoHash string) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	// If we already have this catalog, we're done
	if ct.infoHash == infoHash {
		fmt.Printf("[CatalogTorrent] Already have catalog with infohash: %s\n", infoHash)
		return nil
	}
	
	fmt.Printf("[CatalogTorrent] Fetching catalog torrent: %s\n", infoHash)
	
	// Add magnet link
	magnetURI := fmt.Sprintf("magnet:?xt=urn:btih:%s", infoHash)
	t, err := ct.client.AddMagnet(magnetURI)
	if err != nil {
		return fmt.Errorf("failed to add catalog magnet: %w", err)
	}
	
	// First check if there are any peers for this torrent in DHT
	// Give it a few seconds to find peers
	fmt.Println("[CatalogTorrent] Searching for peers in DHT...")
	time.Sleep(5 * time.Second)
	
	stats := t.Stats()
	fmt.Printf("[CatalogTorrent] Initial peer check - Active peers: %d, Total peers: %d, Known swarm: %d\n", 
		stats.ActivePeers, stats.TotalPeers, len(t.KnownSwarm()))
	
	// If no peers found after initial search, the catalog is likely dead
	if stats.TotalPeers == 0 {
		fmt.Println("[CatalogTorrent] No peers found in DHT for catalog torrent")
		t.Drop()
		return fmt.Errorf("no seeders for catalog torrent")
	}
	
	// Wait for info with timeout
	metadataTimeout := time.After(30 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-t.GotInfo():
			fmt.Printf("[CatalogTorrent] Got catalog torrent info\n")
			return nil // Continue to download phase
			
		case <-ticker.C:
			stats := t.Stats()
			fmt.Printf("[CatalogTorrent] Waiting for metadata - Active: %d, Half-open: %d, Total peers: %d\n", 
				stats.ActivePeers, stats.HalfOpenPeers, stats.TotalPeers)
			
		case <-metadataTimeout:
			// Check peers again before giving up
			stats := t.Stats()
			fmt.Printf("[CatalogTorrent] Timeout waiting for metadata. Active: %d, Total peers: %d\n", 
				stats.ActivePeers, stats.TotalPeers)
			t.Drop()
			if stats.TotalPeers == 0 {
				return fmt.Errorf("no seeders for catalog torrent")
			}
			return fmt.Errorf("timeout waiting for catalog torrent metadata")
		}
	}
	
	// Download the catalog
	t.DownloadAll()
	
	// Wait for completion with timeout
	downloadTimeout := time.After(60 * time.Second)
	downloadTicker := time.NewTicker(1 * time.Second)
	defer downloadTicker.Stop()
	
	for {
		select {
		case <-downloadTimeout:
			stats := t.Stats()
			fmt.Printf("[CatalogTorrent] Download timeout. Peers: %d, Seeders: %d\n", 
				stats.ActivePeers, stats.ConnectedSeeders)
			t.Drop()
			return fmt.Errorf("timeout downloading catalog")
			
		case <-downloadTicker.C:
			if t.BytesCompleted() == t.Info().TotalLength() {
				fmt.Printf("[CatalogTorrent] Catalog download complete\n")
				
				// Find the catalog.json file in the downloaded torrent
				for _, file := range t.Files() {
					if filepath.Base(file.Path()) == "catalog.json" {
						// Read the catalog file
						reader := file.NewReader()
						reader.SetResponsive()
						data, err := io.ReadAll(reader)
						if err != nil {
							return fmt.Errorf("failed to read catalog from torrent: %w", err)
						}
						
						// Parse catalog
						var catalog ModelCatalog
						if err := json.Unmarshal(data, &catalog); err != nil {
							return fmt.Errorf("failed to parse catalog: %w", err)
						}
						
						// Update our catalog
						ct.catalog = &catalog
						ct.infoHash = infoHash
						ct.torrent = t
						
						// Save to local file
						if err := ct.saveCatalog(); err != nil {
							fmt.Printf("[CatalogTorrent] Warning: failed to save catalog locally: %v\n", err)
						}
						
						fmt.Printf("[CatalogTorrent] Loaded catalog with %d models\n", len(catalog.Models))
						return nil
					}
				}
				return fmt.Errorf("catalog.json not found in torrent")
			}
			
			// Progress update
			pct := float64(t.BytesCompleted()) / float64(t.Info().TotalLength()) * 100
			stats := t.Stats()
			fmt.Printf("[CatalogTorrent] Downloading: %.1f%% (peers: %d)\n", pct, stats.ActivePeers)
		}
	}
}

// AddModel adds a model to the catalog and creates a new torrent
func (ct *CatalogTorrent) AddModel(name, infoHash string, size int64) (string, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	fmt.Printf("[CatalogTorrent] Adding model to catalog: %s\n", name)
	
	// Check if model already exists with same infohash
	if existing, exists := ct.catalog.Models[name]; exists && existing.InfoHash == infoHash {
		fmt.Printf("[CatalogTorrent] Model %s already in catalog with same infohash, returning existing\n", name)
		return ct.infoHash, nil
	}
	
	// Add or update model in catalog
	ct.catalog.Models[name] = ModelEntry{
		InfoHash: infoHash,
		Size:     size,
		Tags:     extractTags(name),
		Added:    time.Now().Unix(),
	}
	
	// Update catalog metadata
	ct.catalog.Sequence++
	ct.catalog.Updated = time.Now().Unix()
	
	// Save catalog to file
	if err := ct.saveCatalog(); err != nil {
		return "", fmt.Errorf("failed to save catalog: %w", err)
	}
	
	// Create torrent of catalog directory
	catalogTorrentPath := filepath.Join(ct.catalogDir, fmt.Sprintf("catalog_%d.torrent", ct.catalog.Sequence))
	newInfoHash, err := torrentCreator.CreateTorrentFromDirectory(ct.catalogDir, catalogTorrentPath, 256*1024) // 256KB pieces for small catalog
	if err != nil {
		return "", fmt.Errorf("failed to create catalog torrent: %w", err)
	}
	
	// Add and seed the new catalog torrent
	if ct.torrent != nil {
		ct.torrent.Drop() // Stop seeding old version
	}
	
	// Load the torrent metainfo
	mi, err := metainfo.LoadFromFile(catalogTorrentPath)
	if err != nil {
		return "", fmt.Errorf("failed to load catalog torrent metainfo: %w", err)
	}
	
	// Create storage that puts files directly in the catalog directory
	// Since the torrent has no name, files will be placed directly in the base dir
	catalogStorage := torrentStorage.NewFileOpts(torrentStorage.NewFileClientOpts{
		ClientBaseDir: ct.catalogDir,
		TorrentDirMaker: func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
			// Return the base dir itself, not a subdirectory
			return baseDir
		},
	})
	
	// Add torrent with custom storage pointing to catalog directory
	newTorrent, isNew := ct.client.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash: mi.HashInfoBytes(),
		Storage:  catalogStorage,
		InfoBytes: mi.InfoBytes,
	})
	
	if newTorrent == nil {
		return "", fmt.Errorf("failed to add catalog torrent to client")
	}
	
	fmt.Printf("[CatalogTorrent] Added catalog torrent to client (new: %v)\n", isNew)
	
	// Make sure we download/seed all pieces
	// Since files already exist locally, this will verify and start seeding
	newTorrent.DownloadAll()
	
	// The torrent client will automatically announce to DHT
	
	// Check torrent status
	stats := newTorrent.Stats()
	fmt.Printf("[CatalogTorrent] Catalog torrent stats - Active peers: %d, Seeding: %v, DHT nodes: %d\n", 
		stats.ActivePeers, newTorrent.Seeding(), len(newTorrent.KnownSwarm()))
	
	ct.torrent = newTorrent
	ct.infoHash = newInfoHash
	ct.torrentFile = catalogTorrentPath
	
	fmt.Printf("[CatalogTorrent] Created new catalog torrent: %s\n", newInfoHash)
	fmt.Printf("[CatalogTorrent] Catalog now contains %d models\n", len(ct.catalog.Models))
	
	return newInfoHash, nil
}

// GetModels returns models matching the pattern
func (ct *CatalogTorrent) GetModels(pattern string) ([]*types.ModelAnnouncement, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	if ct.catalog == nil || len(ct.catalog.Models) == 0 {
		return nil, nil
	}
	
	var results []*types.ModelAnnouncement
	for name, model := range ct.catalog.Models {
		if pattern == "" || pattern == "*" || matchesPattern(name, pattern) {
			results = append(results, &types.ModelAnnouncement{
				Name:     name,
				InfoHash: model.InfoHash,
				Size:     model.Size,
				Time:     model.Added,
			})
		}
	}
	
	return results, nil
}

// GetCatalogReference returns the current catalog reference for BEP44
func (ct *CatalogTorrent) GetCatalogReference() *CatalogReference {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	
	if ct.infoHash == "" {
		return nil
	}
	
	ref := &CatalogReference{
		InfoHash: ct.infoHash,
		Sequence: ct.catalog.Sequence,
		Updated:  ct.catalog.Updated,
	}
	
	// Add optional metadata
	if ct.torrent != nil {
		ref.Seeders = ct.torrent.Stats().ActivePeers
		if ct.torrent.Info() != nil {
			ref.Size = ct.torrent.Info().TotalLength()
		}
	}
	
	return ref
}

// MergeCatalog merges another catalog with ours
func (ct *CatalogTorrent) MergeCatalog(other *ModelCatalog) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	changed := false
	for name, entry := range other.Models {
		if existing, exists := ct.catalog.Models[name]; !exists || entry.Added > existing.Added {
			ct.catalog.Models[name] = entry
			changed = true
			fmt.Printf("[CatalogTorrent] Merged model: %s\n", name)
		}
	}
	
	if changed {
		ct.catalog.Updated = time.Now().Unix()
		ct.saveCatalog()
	}
	
	return changed
}

// Helper functions

func (ct *CatalogTorrent) loadCatalog() error {
	data, err := os.ReadFile(ct.catalogFile)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, &ct.catalog)
}

func (ct *CatalogTorrent) saveCatalog() error {
	data, err := json.MarshalIndent(ct.catalog, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(ct.catalogFile, data, 0644)
}

// StartSeeding ensures we're seeding the catalog
func (ct *CatalogTorrent) StartSeeding() error {
	if ct.torrent == nil && ct.torrentFile != "" {
		// Load the torrent metainfo
		mi, err := metainfo.LoadFromFile(ct.torrentFile)
		if err != nil {
			return fmt.Errorf("failed to load catalog torrent metainfo: %w", err)
		}
		
		// Create storage that puts files directly in the catalog directory
		catalogStorage := torrentStorage.NewFileOpts(torrentStorage.NewFileClientOpts{
			ClientBaseDir: ct.catalogDir,
			TorrentDirMaker: func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
				// Return the base dir itself, not a subdirectory
				return baseDir
			},
		})
		
		// Re-add the torrent with correct storage location
		t, isNew := ct.client.AddTorrentOpt(torrent.AddTorrentOpts{
			InfoHash: mi.HashInfoBytes(),
			Storage:  catalogStorage,
			InfoBytes: mi.InfoBytes,
		})
		
		if t == nil {
			return fmt.Errorf("failed to add catalog torrent to client")
		}
		
		fmt.Printf("[CatalogTorrent] Re-added catalog torrent (new: %v)\n", isNew)
		
		// Start seeding
		t.DownloadAll()
		
		// Check status
		stats := t.Stats()
		fmt.Printf("[CatalogTorrent] Started seeding catalog: %s (peers: %d, seeding: %v)\n", 
			mi.HashInfoBytes().HexString(), stats.ActivePeers, t.Seeding())
		
		ct.torrent = t
	}
	return nil
}