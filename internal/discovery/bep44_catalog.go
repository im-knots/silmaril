package discovery

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/torrent/bencode"
	"github.com/silmaril/silmaril/pkg/types"
)

const (
	// Well-known seed for the Silmaril discovery catalog
	WellKnownSeed = "silmaril-discovery-v1"
	
	// Maximum size for BEP 44 value (1000 bytes)
	MaxValueSize = 1000
)

// BEP44Catalog manages the distributed model catalog using BEP 44 mutable items
type BEP44Catalog struct {
	server *dht.Server
	
	// Deterministic key derived from well-known seed
	privateKey ed25519.PrivateKey
	publicKey  [32]byte
	
	mu       sync.RWMutex
	sequence int64
	catalog  *ModelCatalog
	
	ctx    context.Context
	cancel context.CancelFunc
}

// ModelCatalog is the catalog of all discoverable models
type ModelCatalog struct {
	Version  int                    `json:"v"`
	Sequence int64                  `json:"seq"`
	Updated  int64                  `json:"t"`
	Models   map[string]ModelEntry  `json:"m"`
}

// ModelEntry is a single model in the catalog
type ModelEntry struct {
	InfoHash string   `json:"h"`
	Size     int64    `json:"s,omitempty"`
	Tags     []string `json:"t,omitempty"`
	Added    int64    `json:"a"`
}

// NewBEP44Catalog creates a new BEP 44 catalog manager
func NewBEP44Catalog(server *dht.Server) (*BEP44Catalog, error) {
	fmt.Printf("[BEP44] Creating catalog with well-known seed: %s\n", WellKnownSeed)
	
	// Generate deterministic key from well-known seed
	seed := sha256.Sum256([]byte(WellKnownSeed))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	
	var publicKey [32]byte
	copy(publicKey[:], privateKey.Public().(ed25519.PublicKey))
	
	fmt.Printf("[BEP44] Catalog public key: %x\n", publicKey[:16])
	
	ctx, cancel := context.WithCancel(context.Background())
	
	cat := &BEP44Catalog{
		server:     server,
		privateKey: privateKey,
		publicKey:  publicKey,
		ctx:        ctx,
		cancel:     cancel,
	}
	
	// Try to fetch existing catalog
	if err := cat.fetchCatalog(); err != nil {
		fmt.Printf("[BEP44] No existing catalog found (will create new): %v\n", err)
		cat.catalog = &ModelCatalog{
			Version: 1,
			Models:  make(map[string]ModelEntry),
		}
	}
	
	return cat, nil
}

// AddModel adds a model to the catalog
func (cat *BEP44Catalog) AddModel(name, infoHash string, size int64) error {
	cat.mu.Lock()
	defer cat.mu.Unlock()
	
	fmt.Printf("[BEP44] Adding model to catalog: %s (infohash: %s)\n", name, infoHash)
	
	// Ensure catalog exists
	if cat.catalog == nil {
		cat.catalog = &ModelCatalog{
			Version: 1,
			Models:  make(map[string]ModelEntry),
		}
	}
	
	// Add/update model entry
	cat.catalog.Models[name] = ModelEntry{
		InfoHash: infoHash,
		Size:     size,
		Tags:     extractTags(name),
		Added:    time.Now().Unix(),
	}
	
	// Update catalog metadata
	cat.sequence++
	cat.catalog.Sequence = cat.sequence
	cat.catalog.Updated = time.Now().Unix()
	
	// Publish to DHT
	if err := cat.publishCatalog(); err != nil {
		return fmt.Errorf("failed to publish catalog: %w", err)
	}
	
	fmt.Printf("[BEP44] Catalog updated with %d models\n", len(cat.catalog.Models))
	return nil
}

// GetModels retrieves models from the catalog
func (cat *BEP44Catalog) GetModels(pattern string) ([]*types.ModelAnnouncement, error) {
	cat.mu.RLock()
	defer cat.mu.RUnlock()
	
	fmt.Printf("[BEP44] Searching catalog for pattern: %s\n", pattern)
	
	// Try to fetch latest catalog
	if err := cat.fetchCatalog(); err != nil {
		fmt.Printf("[BEP44] Could not refresh catalog: %v\n", err)
		// Continue with cached version
	}
	
	if cat.catalog == nil || len(cat.catalog.Models) == 0 {
		fmt.Printf("[BEP44] No models in catalog\n")
		return nil, nil
	}
	
	var results []*types.ModelAnnouncement
	for name, model := range cat.catalog.Models {
		if pattern == "" || matchesPattern(name, pattern) {
			results = append(results, &types.ModelAnnouncement{
				Name:     name,
				InfoHash: model.InfoHash,
				Size:     model.Size,
				Time:     model.Added,
			})
		}
	}
	
	fmt.Printf("[BEP44] Found %d matching models\n", len(results))
	return results, nil
}

// publishCatalog publishes the catalog to DHT using BEP 44
func (cat *BEP44Catalog) publishCatalog() error {
	// Serialize catalog to JSON (compact)
	jsonData, err := json.Marshal(cat.catalog)
	if err != nil {
		return fmt.Errorf("failed to serialize catalog: %w", err)
	}
	
	// BEP44 values must be bencode-encoded strings
	var buf bytes.Buffer
	encoder := bencode.NewEncoder(&buf)
	if err := encoder.Encode(jsonData); err != nil {
		return fmt.Errorf("failed to bencode data: %w", err)
	}
	data := buf.Bytes()
	
	// Check size limit
	if len(data) > MaxValueSize {
		fmt.Printf("[BEP44] Warning: Catalog size %d exceeds limit, truncating\n", len(data))
		// In production, implement pagination or use multiple keys
		// For now, we'll just warn
	}
	
	fmt.Printf("[BEP44] Publishing catalog (seq: %d, JSON size: %d, bencode size: %d bytes)\n", 
		cat.sequence, len(jsonData), len(data))
	
	// Create BEP 44 item with bencode data
	item, err := bep44.NewItem(data, nil, cat.sequence, 0, cat.privateKey)
	if err != nil {
		return fmt.Errorf("failed to create BEP44 item: %w", err)
	}
	
	// Convert to Put for DHT operation
	put := item.ToPut()
	
	// Get target for this key
	target := bep44.MakeMutableTarget(cat.publicKey, nil)
	
	// Get nodes to publish to
	nodes := cat.server.Nodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no DHT nodes available")
	}
	
	fmt.Printf("[BEP44] Publishing to %d DHT nodes\n", min(5, len(nodes)))
	
	// Publish to multiple nodes for redundancy
	ctx, cancel := context.WithTimeout(cat.ctx, 30*time.Second)
	defer cancel()
	
	published := 0
	for i, node := range nodes {
		if i >= 5 { // Limit to 5 nodes
			break
		}
		
		// First, get a write token from the node
		addr := dht.NewAddr(node.Addr.UDP())
		
		// Get token
		getCtx, getCancel := context.WithTimeout(ctx, 5*time.Second)
		defer getCancel()
		
		result := cat.server.Get(getCtx, addr, target, &cat.sequence, dht.QueryRateLimiting{})
		
		if result.Err != nil {
			fmt.Printf("[BEP44] Error getting token from %s: %v\n", addr, result.Err)
			continue
		}
		
		token := ""
		if result.Reply.R != nil && result.Reply.R.Token != nil && *result.Reply.R.Token != "" {
			token = *result.Reply.R.Token
		}
		
		if token == "" {
			fmt.Printf("[BEP44] No token from %s\n", addr)
			continue
		}
		
		// Now put with the token
		putCtx, putCancel := context.WithTimeout(ctx, 5*time.Second)
		defer putCancel()
		
		putResult := cat.server.Put(putCtx, addr, put, token, dht.QueryRateLimiting{})
		
		if putResult.Err != nil {
			fmt.Printf("[BEP44] Error putting to %s: %v\n", addr, putResult.Err)
		} else {
			published++
			fmt.Printf("[BEP44] Published to node %s\n", addr)
		}
	}
	
	if published == 0 {
		return fmt.Errorf("failed to publish to any DHT node")
	}
	
	fmt.Printf("[BEP44] Successfully published to %d nodes\n", published)
	return nil
}

// fetchCatalog retrieves the catalog from DHT
func (cat *BEP44Catalog) fetchCatalog() error {
	// Get target for this key
	target := bep44.MakeMutableTarget(cat.publicKey, nil)
	
	fmt.Printf("[BEP44] Fetching catalog from DHT (target: %x)\n", target[:8])
	
	// Get nodes to query
	nodes := cat.server.Nodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no DHT nodes available")
	}
	
	ctx, cancel := context.WithTimeout(cat.ctx, 30*time.Second)
	defer cancel()
	
	// Query multiple nodes
	for i, node := range nodes {
		if i >= 10 { // Try up to 10 nodes
			break
		}
		
		addr := dht.NewAddr(node.Addr.UDP())
		
		// Get from node
		getCtx, getCancel := context.WithTimeout(ctx, 5*time.Second)
		defer getCancel()
		
		result := cat.server.Get(getCtx, addr, target, nil, dht.QueryRateLimiting{})
		
		if result.Err != nil {
			continue
		}
		
		// Check if we got a value
		if result.Reply.R == nil || result.Reply.R.V == nil {
			continue
		}
		
		res := result.Reply.R
		
		// Parse the value (res.V is bencode.Bytes)
		rawData := []byte(res.V)
		
		// The data from DHT is bencode-encoded string, we need to decode it
		// BEP44 values are stored as bencode strings in the format "length:content"
		decoder := bencode.NewDecoder(bytes.NewReader(rawData))
		var jsonData []byte
		if err := decoder.Decode(&jsonData); err != nil {
			fmt.Printf("[BEP44] Failed to decode bencode from %s: %v\n", addr, err)
			continue
		}
		
		fmt.Printf("[BEP44] Received data from %s (len: %d)\n", addr, len(jsonData))
		
		// Verify signature if we have one (use the original bencode data for verification)
		if res.Seq != nil {
			// Verify using the public key
			if !bep44.Verify(cat.privateKey.Public().(ed25519.PublicKey), nil, *res.Seq, rawData, res.Sig[:]) {
				fmt.Printf("[BEP44] Invalid signature from %s\n", addr)
				continue
			}
		}
		
		// Now parse the JSON data
		var catalog ModelCatalog
		if err := json.Unmarshal(jsonData, &catalog); err != nil {
			fmt.Printf("[BEP44] Failed to parse catalog from %s: %v\n", addr, err)
			// Debug: show first 200 bytes of JSON data to diagnose
			preview := jsonData
			if len(preview) > 200 {
				preview = preview[:200]
			}
			fmt.Printf("[BEP44] JSON data preview: %s\n", string(preview))
			continue
		}
		
		// Update our state if newer
		if catalog.Sequence > cat.sequence {
			cat.catalog = &catalog
			cat.sequence = catalog.Sequence
			fmt.Printf("[BEP44] Fetched catalog with %d models (seq: %d) from %s\n", 
				len(catalog.Models), catalog.Sequence, addr)
			return nil
		}
	}
	
	return fmt.Errorf("catalog not found in DHT")
}

// extractTags extracts searchable tags from a model name
func extractTags(name string) []string {
	var tags []string
	lower := strings.ToLower(name)
	
	// Extract org/model parts
	if parts := strings.Split(lower, "/"); len(parts) > 0 {
		tags = append(tags, parts[0])
		if len(parts) > 1 {
			for _, part := range strings.Split(parts[1], "-") {
				if len(part) > 2 {
					tags = append(tags, part)
				}
			}
		}
	}
	
	// Common sizes
	for _, size := range []string{"3b", "7b", "8b", "13b", "70b"} {
		if strings.Contains(lower, size) {
			tags = append(tags, size)
		}
	}
	
	// Model families
	for _, family := range []string{"llama", "mistral", "gpt", "gemma", "phi"} {
		if strings.Contains(lower, family) {
			tags = append(tags, family)
		}
	}
	
	return tags
}

// matchesPattern checks if a name matches a search pattern
func matchesPattern(name, pattern string) bool {
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Close shuts down the catalog
func (cat *BEP44Catalog) Close() {
	cat.cancel()
}