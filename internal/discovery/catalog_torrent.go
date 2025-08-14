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
	
	// Wait for info with timeout
	select {
	case <-t.GotInfo():
		fmt.Printf("[CatalogTorrent] Got catalog torrent info\n")
	case <-time.After(30 * time.Second):
		t.Drop()
		return fmt.Errorf("timeout waiting for catalog torrent metadata")
	}
	
	// Download the catalog
	t.DownloadAll()
	
	// Wait for completion with timeout
	downloadTimeout := time.After(60 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-downloadTimeout:
			return fmt.Errorf("timeout downloading catalog")
		case <-ticker.C:
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
			fmt.Printf("[CatalogTorrent] Downloading catalog: %.1f%%\n", pct)
		}
	}
}

// AddModel adds a model to the catalog and creates a new torrent
func (ct *CatalogTorrent) AddModel(name, infoHash string, size int64) (string, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	
	fmt.Printf("[CatalogTorrent] Adding model to catalog: %s\n", name)
	
	// Add model to catalog
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
	
	newTorrent, err := ct.client.AddTorrentFromFile(catalogTorrentPath)
	if err != nil {
		return "", fmt.Errorf("failed to add catalog torrent: %w", err)
	}
	
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
		// Re-add the torrent if we have one
		t, err := ct.client.AddTorrentFromFile(ct.torrentFile)
		if err != nil {
			return fmt.Errorf("failed to start seeding catalog: %w", err)
		}
		ct.torrent = t
	}
	return nil
}