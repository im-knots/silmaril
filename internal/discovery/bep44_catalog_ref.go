package discovery

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/dht/v2/exts/getput"
	"github.com/anacrolix/torrent"
	"github.com/silmaril/silmaril/pkg/types"
)

// BEP44CatalogRef manages the BEP44 reference to the catalog torrent
type BEP44CatalogRef struct {
	mu     sync.Mutex
	server *dht.Server
	
	// Deterministic key derived from well-known seed
	privateKey ed25519.PrivateKey
	publicKey  [32]byte
	
	// Current reference
	sequence int64
	ref      *CatalogReference
	
	// Catalog torrent manager
	catalogTorrent *CatalogTorrent
	
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBEP44CatalogRef creates a new BEP44 catalog reference manager
func NewBEP44CatalogRef(server *dht.Server, torrentClient *torrent.Client) (*BEP44CatalogRef, error) {
	fmt.Printf("[BEP44Ref] Creating catalog reference with well-known seed: %s\n", WellKnownSeed)
	
	// Generate deterministic key from well-known seed
	seed := sha256.Sum256([]byte(WellKnownSeed))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	
	var publicKey [32]byte
	copy(publicKey[:], privateKey.Public().(ed25519.PublicKey))
	
	fmt.Printf("[BEP44Ref] Catalog reference public key: %x\n", publicKey[:16])
	
	// Create catalog torrent manager
	catalogTorrent, err := NewCatalogTorrent(torrentClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog torrent: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	ref := &BEP44CatalogRef{
		server:         server,
		privateKey:     privateKey,
		publicKey:      publicKey,
		catalogTorrent: catalogTorrent,
		ctx:            ctx,
		cancel:         cancel,
	}
	
	// Try to fetch existing catalog reference
	if err := ref.fetchCatalogRef(); err != nil {
		fmt.Printf("[BEP44Ref] No existing catalog reference found: %v\n", err)
	}
	
	return ref, nil
}

// PublishCatalogRef publishes the catalog reference to BEP44 using proper traversal
func (ref *BEP44CatalogRef) PublishCatalogRef(catalogInfoHash string) error {
	fmt.Printf("[BEP44Ref] Publishing catalog reference: %s\n", catalogInfoHash)
	
	// Update sequence and reference
	ref.sequence++
	ref.ref = &CatalogReference{
		InfoHash: catalogInfoHash,
		Sequence: ref.sequence,
		Updated:  time.Now().Unix(),
	}
	
	// Serialize to JSON (compact)
	data, err := json.Marshal(ref.ref)
	if err != nil {
		return fmt.Errorf("failed to serialize reference: %w", err)
	}
	
	fmt.Printf("[BEP44Ref] Publishing reference (seq: %d, size: %d bytes)\n", ref.sequence, len(data))
	
	// Get target for this key
	target := bep44.MakeMutableTarget(ref.publicKey, nil)
	
	// Use traversal-based Put to find and store on the correct nodes
	ctx, cancel := context.WithTimeout(ref.ctx, 60*time.Second)
	defer cancel()
	
	// Create a function that generates the Put with the current sequence
	seqToPut := func(seq int64) bep44.Put {
		// If there's already a higher sequence number in the DHT, use that + 1
		if seq >= ref.sequence {
			ref.sequence = seq + 1
			ref.ref.Sequence = ref.sequence
			data, _ = json.Marshal(ref.ref)
		}
		
		// Create and sign the BEP44 item
		item, err := bep44.NewItem(data, nil, ref.sequence, 0, ref.privateKey)
		if err != nil {
			fmt.Printf("[BEP44Ref] Error creating BEP44 item: %v\n", err)
			return bep44.Put{}
		}
		return item.ToPut()
	}
	
	fmt.Printf("[BEP44Ref] Starting traversal to find nodes closest to target %x\n", target[:8])
	
	// Perform the traversal-based Put operation
	stats, err := getput.Put(ctx, target, ref.server, nil, seqToPut)
	if err != nil {
		return fmt.Errorf("traversal put failed: %w", err)
	}
	
	fmt.Printf("[BEP44Ref] Traversal complete - contacted %d nodes, got %d responses\n", 
		stats.NumAddrsTried, stats.NumResponses)
	
	// Give the value a moment to settle
	time.Sleep(1 * time.Second)
	
	// Verify we can fetch it back
	fmt.Println("[BEP44Ref] Verifying catalog reference was stored...")
	if err := ref.fetchCatalogRef(); err != nil {
		fmt.Printf("[BEP44Ref] Warning: Could not verify catalog storage: %v\n", err)
	} else {
		fmt.Println("[BEP44Ref] Catalog reference verified successfully")
	}
	
	return nil
}

// fetchCatalogRef fetches the catalog reference from BEP44 using proper traversal
func (ref *BEP44CatalogRef) fetchCatalogRef() error {
	target := bep44.MakeMutableTarget(ref.publicKey, nil)
	
	fmt.Printf("[BEP44Ref] Fetching catalog reference from DHT (target: %x)\n", target[:8])
	
	// Use traversal-based Get to find the value
	ctx, cancel := context.WithTimeout(ref.ctx, 30*time.Second)
	defer cancel()
	
	// Perform the traversal-based Get operation
	result, stats, err := getput.Get(ctx, target, ref.server, nil, nil)
	
	if err != nil {
		if stats != nil {
			fmt.Printf("[BEP44Ref] Get traversal failed after contacting %d nodes: %v\n", 
				stats.NumAddrsTried, err)
		}
		return fmt.Errorf("catalog reference not found in DHT: %w", err)
	}
	
	fmt.Printf("[BEP44Ref] Get traversal complete - contacted %d nodes, got %d responses\n",
		stats.NumAddrsTried, stats.NumResponses)
	
	// Parse the retrieved value
	if len(result.V) == 0 {
		return fmt.Errorf("empty catalog reference value")
	}
	
	// The value from BEP44 is the raw bytes we stored
	jsonData := []byte(result.V)
	
	// Debug: log what we got
	fmt.Printf("[BEP44Ref] Retrieved raw value (len=%d): %x\n", len(jsonData), jsonData)
	fmt.Printf("[BEP44Ref] Retrieved as string: %q\n", string(jsonData))
	
	// BEP44 values might have bencode length prefix (e.g., "84:" for 84 bytes)
	// Look for the colon that separates length from data
	colonIdx := -1
	for i, b := range jsonData {
		if b == ':' {
			colonIdx = i
			break
		}
	}
	
	// If we found a colon, extract the JSON after it
	if colonIdx > 0 && colonIdx < len(jsonData)-1 {
		// Everything after the colon should be our JSON
		jsonData = jsonData[colonIdx+1:]
		fmt.Printf("[BEP44Ref] Extracted JSON after bencode prefix: %q\n", string(jsonData))
	}
	
	// Parse the JSON catalog reference
	var catalogRef CatalogReference
	if err := json.Unmarshal(jsonData, &catalogRef); err != nil {
		return fmt.Errorf("failed to parse catalog reference from %q: %w", string(jsonData), err)
	}
	
	fmt.Printf("[BEP44Ref] Found catalog reference: %s (seq: %d)\n", 
		catalogRef.InfoHash, result.Seq)
	
	// Update our state if newer or equal (to refresh our knowledge)
	if result.Seq >= ref.sequence {
		ref.ref = &catalogRef
		ref.sequence = result.Seq
		
		// Fetch the catalog torrent
		if err := ref.catalogTorrent.LoadOrFetchCatalog(catalogRef.InfoHash); err != nil {
			fmt.Printf("[BEP44Ref] Warning: failed to fetch catalog torrent: %v\n", err)
			
			// Check if error is due to no seeders
			if err.Error() == "no seeders for catalog torrent" {
				fmt.Println("[BEP44Ref] No seeders for catalog, will create a new one when models are added")
				// Reset catalog to empty so we can rebuild it
				ref.catalogTorrent.catalog = &ModelCatalog{
					Version: 1,
					Models:  make(map[string]ModelEntry),
				}
				ref.catalogTorrent.infoHash = ""
				ref.catalogTorrent.torrent = nil
			}
			// If we can't get the catalog torrent, we'll create a new one when needed
			// This handles the case where the catalog reference exists but no one is seeding
		}
	}
	
	return nil
}

// RefreshCatalog checks for catalog updates from the DHT
func (ref *BEP44CatalogRef) RefreshCatalog() error {
	return ref.fetchCatalogRef()
}

// RepublishCatalog republishes the current catalog reference to keep it alive in DHT
func (ref *BEP44CatalogRef) RepublishCatalog() error {
	// If we don't have a catalog, nothing to republish
	if ref.ref == nil || ref.ref.InfoHash == "" {
		return fmt.Errorf("no catalog to republish")
	}
	
	// Republish the current catalog reference to keep it alive
	return ref.PublishCatalogRef(ref.ref.InfoHash)
}

// AddModel adds a model and publishes the new catalog
func (ref *BEP44CatalogRef) AddModel(name, infoHash string, size int64) error {
	// Lock to prevent concurrent catalog updates
	ref.mu.Lock()
	defer ref.mu.Unlock()
	
	fmt.Printf("[BEP44Ref] AddModel acquiring lock for: %s\n", name)
	
	// Check if model already exists in our local catalog
	models, _ := ref.catalogTorrent.GetModels("")
	for _, model := range models {
		if model.InfoHash == infoHash {
			fmt.Printf("[BEP44Ref] Model %s already in catalog, skipping add\n", name)
			return nil
		}
	}
	
	// First fetch latest catalog to avoid conflicts
	if err := ref.fetchCatalogRef(); err != nil {
		fmt.Printf("[BEP44Ref] Could not fetch latest catalog (will use local): %v\n", err)
	}
	
	// Add model to catalog torrent
	newCatalogHash, err := ref.catalogTorrent.AddModel(name, infoHash, size)
	if err != nil {
		return fmt.Errorf("failed to add model to catalog: %w", err)
	}
	
	// Publish new catalog reference
	if err := ref.PublishCatalogRef(newCatalogHash); err != nil {
		return fmt.Errorf("failed to publish catalog reference: %w", err)
	}
	
	// Give the value a moment to propagate before the next operation
	time.Sleep(2 * time.Second)
	
	// Start seeding the catalog
	if err := ref.catalogTorrent.StartSeeding(); err != nil {
		fmt.Printf("[BEP44Ref] Warning: failed to start seeding catalog: %v\n", err)
	}
	
	return nil
}

// GetModels searches for models
func (ref *BEP44CatalogRef) GetModels(pattern string) ([]*types.ModelAnnouncement, error) {
	// Try to fetch latest catalog
	if err := ref.fetchCatalogRef(); err != nil {
		fmt.Printf("[BEP44Ref] Could not fetch latest catalog: %v\n", err)
	}
	
	return ref.catalogTorrent.GetModels(pattern)
}

// Close shuts down the catalog reference manager
func (ref *BEP44CatalogRef) Close() {
	ref.cancel()
}