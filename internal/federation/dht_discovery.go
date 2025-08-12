package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/silmaril/silmaril/pkg/types"
)

const (
	// Prefix for Silmaril model magnet URIs
	SilmarilPrefix = "silmaril:"
	
	// DHT refresh interval
	dhtRefreshInterval = 30 * time.Minute
	
	// DHT bootstrap timeout
	dhtBootstrapTimeout = 30 * time.Second
	
	// Model info embedded in torrent comment field
	ModelInfoField = "silmaril_model_info"
)

// DHTDiscovery handles DHT-based model discovery using standard BitTorrent DHT
type DHTDiscovery struct {
	client    *torrent.Client
	ctx       context.Context
	cancel    context.CancelFunc
	
	// Manifest storage
	mu        sync.RWMutex
	models    map[string]*ModelEntry  // infoHash -> model entry
	byName    map[string]string       // modelName -> infoHash
}

// ModelEntry represents a model in the DHT
type ModelEntry struct {
	Manifest     *types.ModelManifest
	Torrent      *torrent.Torrent
	InfoHash     metainfo.Hash
	LastAnnounce time.Time
	Seeders      int
	Leechers     int
}

// NewDHTDiscovery creates a new DHT discovery service
func NewDHTDiscovery(dataDir string, port int) (*DHTDiscovery, error) {
	// Create torrent client config with DHT enabled
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.NoDHT = false
	cfg.DisableTrackers = true
	cfg.DisableWebtorrent = true
	cfg.DisablePEX = false
	cfg.Seed = true
	
	if port > 0 {
		cfg.ListenPort = port
	}
	
	// DHT will use default bootstrap nodes automatically
	
	// Create torrent client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	d := &DHTDiscovery{
		client: client,
		ctx:    ctx,
		cancel: cancel,
		models: make(map[string]*ModelEntry),
		byName: make(map[string]string),
	}
	
	// Start background refresh
	go d.refreshLoop()
	
	return d, nil
}

// Close shuts down the DHT discovery service
func (d *DHTDiscovery) Close() error {
	d.cancel()
	d.client.Close()
	return nil
}

// Bootstrap connects to the DHT network
func (d *DHTDiscovery) Bootstrap(ctx context.Context) error {
	// The torrent client bootstraps automatically
	// We just wait to ensure we have DHT connectivity
	
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	deadline := time.Now().Add(dhtBootstrapTimeout)
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stats := d.client.Stats()
			// Check if we have DHT nodes
			if stats.ActiveHalfOpenAttempts > 0 || len(d.client.Torrents()) > 0 {
				return nil // Connected
			}
			if time.Now().After(deadline) {
				// Not really an error, DHT will connect eventually
				return nil
			}
		}
	}
}

// AnnounceModel announces a model to the DHT
func (d *DHTDiscovery) AnnounceModel(manifest *types.ModelManifest) error {
	// Create a deterministic info hash based on model name
	// This allows other peers to find it by name
	infoHash := d.modelInfoHash(manifest.Name, manifest.Version)
	
	// Create torrent spec with model metadata
	spec := &torrent.TorrentSpec{
		InfoHash:    infoHash,
		DisplayName: d.modelDisplayName(manifest.Name, manifest.Version),
		// We don't need actual files, just use DHT for discovery
		InfoBytes: d.createMinimalInfo(manifest),
	}
	
	// Add torrent to client (this announces to DHT)
	t, _, err := d.client.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to add torrent spec: %w", err)
	}
	
	// Store model entry
	d.mu.Lock()
	entry := &ModelEntry{
		Manifest:     manifest,
		Torrent:      t,
		InfoHash:     infoHash,
		LastAnnounce: time.Now(),
	}
	d.models[infoHash.HexString()] = entry
	d.byName[manifest.Name] = infoHash.HexString()
	d.mu.Unlock()
	
	return nil
}

// DiscoverModels discovers all available models from the DHT
func (d *DHTDiscovery) DiscoverModels(ctx context.Context) ([]*types.ModelManifest, error) {
	// Try common model names with Silmaril prefix
	var models []*types.ModelManifest
	var mu sync.Mutex
	var wg sync.WaitGroup
	
	// Common model patterns to search
	modelPatterns := []string{
		"meta-llama/Llama-3.1-70B",
		"meta-llama/Llama-3.1-8B",
		"mistralai/Mistral-7B-v0.1",
		"mistralai/Mixtral-8x7B-v0.1",
		"google/gemma-7b",
		"stabilityai/stablelm-2-12b",
	}
	
	for _, pattern := range modelPatterns {
		wg.Add(1)
		go func(modelName string) {
			defer wg.Done()
			
			// Try to find this model
			if manifest := d.searchForModel(ctx, modelName); manifest != nil {
				mu.Lock()
				models = append(models, manifest)
				mu.Unlock()
			}
		}(pattern)
	}
	
	wg.Wait()
	
	// Also return any models we're currently tracking
	d.mu.RLock()
	for _, entry := range d.models {
		models = append(models, entry.Manifest)
	}
	d.mu.RUnlock()
	
	return models, nil
}

// SearchForModel searches for a specific model by name
func (d *DHTDiscovery) SearchForModel(ctx context.Context, modelName string) (*types.ModelManifest, error) {
	// Check if we already have it
	d.mu.RLock()
	if ihStr, ok := d.byName[modelName]; ok {
		if entry, ok := d.models[ihStr]; ok {
			d.mu.RUnlock()
			return entry.Manifest, nil
		}
	}
	d.mu.RUnlock()
	
	// Search DHT
	manifest := d.searchForModel(ctx, modelName)
	return manifest, nil
}

// searchForModel internal helper to search for a model
func (d *DHTDiscovery) searchForModel(ctx context.Context, modelName string) *types.ModelManifest {
	// Try different version patterns
	versions := []string{"", "-v1", "-v2", "-v3"}
	
	for _, ver := range versions {
		// Generate the info hash this model would have
		infoHash := d.modelInfoHash(modelName, ver)
		
		// Try to add this torrent and see if we get peers
		spec := &torrent.TorrentSpec{
			InfoHash:    infoHash,
			DisplayName: d.modelDisplayName(modelName, ver),
		}
		
		t, _, err := d.client.AddTorrentSpec(spec)
		if err != nil {
			continue
		}
		
		// Wait briefly for peers
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		
		select {
		case <-ctx.Done():
			continue
		case <-time.After(3 * time.Second):
			// Check if we found peers
			stats := t.Stats()
			if stats.TotalPeers > 0 {
				// Found peers! This model exists
				// Try to get metadata from peers
				manifest := &types.ModelManifest{
					Name:      modelName,
					Version:   ver,
					MagnetURI: fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash.HexString(), d.modelDisplayName(modelName, ver)),
				}
				
				// Store in cache
				d.mu.Lock()
				entry := &ModelEntry{
					Manifest:     manifest,
					Torrent:      t,
					InfoHash:     infoHash,
					LastAnnounce: time.Now(),
					Seeders:      stats.ConnectedSeeders,
					Leechers:     stats.ActivePeers,
				}
				d.models[infoHash.HexString()] = entry
				d.byName[modelName] = infoHash.HexString()
				d.mu.Unlock()
				
				return manifest
			}
		}
	}
	
	return nil
}

// GetPeers gets peers for a specific model
func (d *DHTDiscovery) GetPeers(ctx context.Context, modelName string) ([]string, error) {
	d.mu.RLock()
	ihStr, ok := d.byName[modelName]
	if !ok {
		d.mu.RUnlock()
		// Return empty list if model not found locally
		return []string{}, nil
	}
	
	entry, ok := d.models[ihStr]
	if !ok {
		d.mu.RUnlock()
		return []string{}, nil
	}
	d.mu.RUnlock()
	
	// Get peers from torrent
	var peers []string
	for _, conn := range entry.Torrent.PeerConns() {
		peers = append(peers, conn.RemoteAddr.String())
	}
	
	// Also try to get more peers from swarm
	knownSwarm := entry.Torrent.KnownSwarm()
	for _, peer := range knownSwarm {
		peers = append(peers, peer.Addr.String())
	}
	
	// Return empty array if no peers found
	if peers == nil {
		peers = []string{}
	}
	
	return peers, nil
}

// modelInfoHash generates a deterministic info hash for a model
func (d *DHTDiscovery) modelInfoHash(name, version string) metainfo.Hash {
	// Create a deterministic string that includes our prefix
	data := fmt.Sprintf("%s%s:%s", SilmarilPrefix, name, version)
	hash := sha256.Sum256([]byte(data))
	var ih metainfo.Hash
	copy(ih[:], hash[:20]) // Use first 20 bytes for info hash
	return ih
}

// modelDisplayName creates a display name for a model
func (d *DHTDiscovery) modelDisplayName(name, version string) string {
	if version != "" {
		return fmt.Sprintf("Silmaril: %s (%s)", name, version)
	}
	return fmt.Sprintf("Silmaril: %s", name)
}

// encodeModelInfo encodes model info for torrent comment field
func (d *DHTDiscovery) encodeModelInfo(manifest *types.ModelManifest) string {
	// Simple JSON encoding in comment field
	data, _ := json.Marshal(map[string]interface{}{
		"silmaril": true,
		"name":     manifest.Name,
		"version":  manifest.Version,
		"size":     manifest.TotalSize,
	})
	return string(data)
}

// createMinimalInfo creates minimal torrent info for DHT announcement
func (d *DHTDiscovery) createMinimalInfo(manifest *types.ModelManifest) []byte {
	// Create a minimal but valid info dictionary
	size := manifest.TotalSize
	if size == 0 {
		size = 1024 // Default to 1KB if no size specified
	}
	
	info := metainfo.Info{
		Name:        d.modelDisplayName(manifest.Name, manifest.Version),
		Length:      size,
		PieceLength: 16384, // 16KB pieces for metadata only
	}
	
	// Generate fake pieces just for validity
	numPieces := (size + info.PieceLength - 1) / info.PieceLength
	if numPieces == 0 {
		numPieces = 1
	}
	
	pieces := make([]byte, numPieces*20)
	for i := range pieces {
		pieces[i] = byte(i % 256)
	}
	info.Pieces = pieces
	
	data, _ := bencode.Marshal(info)
	return data
}

// refreshLoop periodically refreshes DHT announcements
func (d *DHTDiscovery) refreshLoop() {
	ticker := time.NewTicker(dhtRefreshInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.refresh()
		}
	}
}

// refresh re-announces all models
func (d *DHTDiscovery) refresh() {
	d.mu.RLock()
	entries := make([]*ModelEntry, 0, len(d.models))
	for _, entry := range d.models {
		entries = append(entries, entry)
	}
	d.mu.RUnlock()
	
	for _, entry := range entries {
		// DHT re-announce happens automatically via the torrent client
		// Just update stats
		stats := entry.Torrent.Stats()
		
		d.mu.Lock()
		entry.Seeders = stats.ConnectedSeeders
		entry.Leechers = stats.ActivePeers
		entry.LastAnnounce = time.Now()
		d.mu.Unlock()
	}
}

// Stats returns DHT statistics
func (d *DHTDiscovery) Stats() map[string]interface{} {
	clientStats := d.client.Stats()
	
	d.mu.RLock()
	modelCount := len(d.models)
	d.mu.RUnlock()
	
	return map[string]interface{}{
		"models":           modelCount,
		"torrents":         len(d.client.Torrents()),
		"active_halfopen":  clientStats.ActiveHalfOpenAttempts,
	}
}

// GetModelStatus returns the status of a specific model
func (d *DHTDiscovery) GetModelStatus(modelName string) (*ModelEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	
	ihStr, ok := d.byName[modelName]
	if !ok {
		return nil, fmt.Errorf("model not found: %s", modelName)
	}
	
	entry, ok := d.models[ihStr]
	if !ok {
		return nil, fmt.Errorf("model entry not found")
	}
	
	// Update current stats
	stats := entry.Torrent.Stats()
	entry.Seeders = stats.ConnectedSeeders
	entry.Leechers = stats.ActivePeers
	
	return entry, nil
}

// Helper functions for magnet URIs

// ParseSilmarilMagnet checks if a magnet URI is a Silmaril model
func ParseSilmarilMagnet(magnetURI string) (modelName string, version string, ok bool) {
	if !strings.Contains(magnetURI, "dn=Silmaril") {
		return "", "", false
	}
	
	// Parse display name to extract model info
	parts := strings.Split(magnetURI, "&")
	for _, part := range parts {
		if strings.HasPrefix(part, "dn=") {
			dn := strings.TrimPrefix(part, "dn=")
			if strings.HasPrefix(dn, "Silmaril:") {
				// Extract model name and version
				info := strings.TrimPrefix(dn, "Silmaril:")
				info = strings.TrimSpace(info)
				
				// Check for version in parentheses
				if idx := strings.Index(info, "("); idx > 0 {
					modelName = strings.TrimSpace(info[:idx])
					version = strings.Trim(info[idx:], "()")
					return modelName, version, true
				}
				
				return info, "", true
			}
		}
	}
	
	return "", "", false
}

// CreateSilmarilMagnet creates a Silmaril-compatible magnet URI
func CreateSilmarilMagnet(modelName, version string, infoHash string) string {
	dn := fmt.Sprintf("Silmaril: %s", modelName)
	if version != "" {
		dn = fmt.Sprintf("Silmaril: %s (%s)", modelName, version)
	}
	
	return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, dn)
}

// InfoHashToHex converts info hash to hex string
func InfoHashToHex(ih metainfo.Hash) string {
	return hex.EncodeToString(ih[:])
}

// InfoHashFromHex parses info hash from hex string
func InfoHashFromHex(s string) (metainfo.Hash, error) {
	var ih metainfo.Hash
	b, err := hex.DecodeString(s)
	if err != nil {
		return ih, err
	}
	if len(b) != 20 {
		return ih, fmt.Errorf("invalid info hash length: %d", len(b))
	}
	copy(ih[:], b)
	return ih, nil
}