package discovery

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/anacrolix/torrent"
	"github.com/silmaril/silmaril/pkg/types"
)

// BEP44CatalogRef manages the BEP44 reference to the catalog torrent
type BEP44CatalogRef struct {
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

// PublishCatalogRef publishes the catalog reference to BEP44
func (ref *BEP44CatalogRef) PublishCatalogRef(catalogInfoHash string) error {
	fmt.Printf("[BEP44Ref] Publishing catalog reference: %s\n", catalogInfoHash)
	
	// Create reference
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
	
	// Create BEP44 item
	item, err := bep44.NewItem(data, nil, ref.sequence, 0, ref.privateKey)
	if err != nil {
		return fmt.Errorf("failed to create BEP44 item: %w", err)
	}
	
	// Convert to Put for DHT operation
	put := item.ToPut()
	
	// Get target for this key
	target := bep44.MakeMutableTarget(ref.publicKey, nil)
	
	// Get nodes to publish to
	nodes := ref.server.Nodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no DHT nodes available")
	}
	
	// TODO: Ideally we'd use nodes closest to the target, but that method is unexported
	fmt.Printf("[BEP44Ref] Publishing to up to %d DHT nodes\n", min(10, len(nodes)))
	
	// Publish to multiple nodes for redundancy
	ctx, cancel := context.WithTimeout(ref.ctx, 30*time.Second)
	defer cancel()
	
	published := 0
	for i, node := range nodes {
		if i >= 10 { // Publish to more nodes for better redundancy
			break
		}
		
		addr := dht.NewAddr(node.Addr.UDP())
		
		// Get token first
		getCtx, getCancel := context.WithTimeout(ctx, 5*time.Second)
		defer getCancel()
		
		result := ref.server.Get(getCtx, addr, target, &ref.sequence, dht.QueryRateLimiting{})
		if result.Err != nil {
			continue
		}
		
		token := ""
		if result.Reply.R != nil && result.Reply.R.Token != nil {
			token = *result.Reply.R.Token
		}
		
		if token == "" {
			continue
		}
		
		// Put with token
		putCtx, putCancel := context.WithTimeout(ctx, 5*time.Second)
		defer putCancel()
		
		putResult := ref.server.Put(putCtx, addr, put, token, dht.QueryRateLimiting{})
		if putResult.Err != nil {
			fmt.Printf("[BEP44Ref] Error putting to %s: %v\n", addr, putResult.Err)
		} else {
			published++
			fmt.Printf("[BEP44Ref] Published to node %s\n", addr)
		}
	}
	
	if published == 0 {
		return fmt.Errorf("failed to publish to any DHT node")
	}
	
	fmt.Printf("[BEP44Ref] Successfully published to %d nodes\n", published)
	
	// Wait a moment for the value to propagate
	time.Sleep(2 * time.Second)
	
	// Verify we can fetch it back
	fmt.Println("[BEP44Ref] Verifying catalog reference was stored...")
	if err := ref.fetchCatalogRef(); err != nil {
		fmt.Printf("[BEP44Ref] Warning: Could not verify catalog storage: %v\n", err)
		// Don't fail here, as it might still propagate
	} else {
		fmt.Println("[BEP44Ref] Catalog reference verified successfully")
	}
	
	return nil
}

// fetchCatalogRef fetches the catalog reference from BEP44
func (ref *BEP44CatalogRef) fetchCatalogRef() error {
	target := bep44.MakeMutableTarget(ref.publicKey, nil)
	
	fmt.Printf("[BEP44Ref] Fetching catalog reference from DHT (target: %x)\n", target[:8])
	
	// Get all nodes we know about
	nodes := ref.server.Nodes()
	if len(nodes) == 0 {
		return fmt.Errorf("no DHT nodes available")
	}
	
	fmt.Printf("[BEP44Ref] Querying %d nodes for catalog reference\n", len(nodes))
	
	ctx, cancel := context.WithTimeout(ref.ctx, 30*time.Second)
	defer cancel()
	
	// Query nodes closest to the target
	queriedCount := 0
	for _, node := range nodes {
		if queriedCount >= 20 {
			break
		}
		
		addr := dht.NewAddr(node.Addr.UDP())
		
		getCtx, getCancel := context.WithTimeout(ctx, 5*time.Second)
		defer getCancel()
		
		result := ref.server.Get(getCtx, addr, target, nil, dht.QueryRateLimiting{})
		queriedCount++
		
		if result.Err != nil {
			fmt.Printf("[BEP44Ref] Error querying %s: %v\n", addr, result.Err)
			continue
		}
		
		if result.Reply.R == nil || result.Reply.R.V == nil {
			// Node doesn't have the value, but this is normal
			continue
		}
		
		res := result.Reply.R
		
		// Parse the value - extract JSON after bencode length prefix
		rawData := []byte(res.V)
		dataStr := string(rawData)
		colonIdx := 0
		for i, ch := range dataStr {
			if ch == ':' {
				colonIdx = i
				break
			}
		}
		
		if colonIdx == 0 {
			continue
		}
		
		data := []byte(dataStr[colonIdx+1:])
		
		// Parse the reference
		var catalogRef CatalogReference
		if err := json.Unmarshal(data, &catalogRef); err != nil {
			fmt.Printf("[BEP44Ref] Failed to parse catalog reference: %v\n", err)
			continue
		}
		
		fmt.Printf("[BEP44Ref] Found catalog reference: %s (seq: %d)\n", 
			catalogRef.InfoHash, catalogRef.Sequence)
		
		// Update our state if newer
		if catalogRef.Sequence >= ref.sequence {
			ref.ref = &catalogRef
			ref.sequence = catalogRef.Sequence
			
			// Fetch the catalog torrent
			if err := ref.catalogTorrent.LoadOrFetchCatalog(catalogRef.InfoHash); err != nil {
				fmt.Printf("[BEP44Ref] Warning: failed to fetch catalog torrent: %v\n", err)
			}
			
			return nil
		}
	}
	
	fmt.Printf("[BEP44Ref] Queried %d nodes but catalog reference not found\n", queriedCount)
	return fmt.Errorf("catalog reference not found in DHT")
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