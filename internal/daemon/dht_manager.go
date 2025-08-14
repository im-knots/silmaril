package daemon

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/discovery"
	"github.com/silmaril/silmaril/pkg/types"
)

type DHTManager struct {
	mu              sync.RWMutex
	config          *config.Config
	torrentManager  *TorrentManager
	torrentClient   *torrent.Client
	dhtServer       *dht.Server
	dhtConn         net.PacketConn
	announcements   map[string]*types.ModelAnnouncement
	lastAnnounce    map[string]time.Time
	catalogRef      *discovery.BEP44CatalogRef
	ctx             context.Context
	cancel          context.CancelFunc
}

func NewDHTManager(cfg *config.Config, tm *TorrentManager) (*DHTManager, error) {
	fmt.Println("[DHT] Creating DHT manager...")
	ctx, cancel := context.WithCancel(context.Background())
	
	dm := &DHTManager{
		config:         cfg,
		torrentManager: tm,
		announcements:  make(map[string]*types.ModelAnnouncement),
		lastAnnounce:   make(map[string]time.Time),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Initialize DHT server with bootstrap nodes
	fmt.Println("[DHT] Creating DHT server configuration...")
	dhtCfg := dht.NewDefaultServerConfig()
	
	// Use custom bootstrap nodes if configured, otherwise use defaults
	if cfg != nil && len(cfg.Network.DHTBootstrapNodes) > 0 {
		fmt.Printf("[DHT] Using custom bootstrap nodes: %v\n", cfg.Network.DHTBootstrapNodes)
		bootstrapNodes := cfg.Network.DHTBootstrapNodes
		dhtCfg.StartingNodes = func() ([]dht.Addr, error) {
			addrs := make([]dht.Addr, 0, len(bootstrapNodes))
			for _, node := range bootstrapNodes {
				fmt.Printf("[DHT] Resolving bootstrap node: %s\n", node)
				udpAddr, err := net.ResolveUDPAddr("udp", node)
				if err != nil {
					fmt.Printf("[DHT] Warning: failed to resolve bootstrap node %s: %v\n", node, err)
					continue
				}
				fmt.Printf("[DHT] Resolved %s to %s\n", node, udpAddr.String())
				addrs = append(addrs, dht.NewAddr(udpAddr))
			}
			if len(addrs) == 0 {
				fmt.Println("[DHT] All custom nodes failed, falling back to default bootstrap nodes")
				// Fall back to defaults if all custom nodes failed
				return dht.GlobalBootstrapAddrs("udp")
			}
			fmt.Printf("[DHT] Using %d bootstrap nodes\n", len(addrs))
			return addrs, nil
		}
	} else {
		fmt.Println("[DHT] Using default DHT bootstrap nodes")
	}
	// Otherwise dhtCfg.StartingNodes already points to GlobalBootstrapAddrs
	
	// Create UDP connection for DHT
	fmt.Println("[DHT] Creating UDP listener...")
	// Try standard DHT port first, fall back to random if unavailable
	dhtPort := ":6881"
	if cfg != nil && cfg.Network.DHTPort > 0 {
		dhtPort = fmt.Sprintf(":%d", cfg.Network.DHTPort)
	}
	conn, err := net.ListenPacket("udp", dhtPort)
	if err != nil {
		fmt.Printf("[DHT] Failed to bind to port %s, trying random port: %v\n", dhtPort, err)
		conn, err = net.ListenPacket("udp", ":0") // Fall back to random port
	}
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create UDP listener: %w", err)
	}
	fmt.Printf("[DHT] UDP listener created on %s\n", conn.LocalAddr())
	dhtCfg.Conn = conn
	
	fmt.Println("[DHT] Creating DHT server...")
	srv, err := dht.NewServer(dhtCfg)
	if err != nil {
		conn.Close()
		cancel()
		return nil, fmt.Errorf("failed to create DHT server: %w", err)
	}
	dm.dhtServer = srv
	dm.dhtConn = conn
	
	fmt.Printf("[DHT] DHT server created and listening on %s\n", conn.LocalAddr())

	// Get torrent client from torrent manager
	if tm != nil && tm.client != nil {
		dm.torrentClient = tm.client
	}
	
	// Create BEP44 catalog reference for model discovery
	fmt.Println("[DHT] Creating BEP44 catalog reference for model discovery...")
	if dm.torrentClient != nil {
		dm.catalogRef, err = discovery.NewBEP44CatalogRef(srv, dm.torrentClient)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create BEP44 catalog reference: %w", err)
		}
		fmt.Println("[DHT] BEP44 catalog reference created with well-known key")
	} else {
		fmt.Println("[DHT] Warning: No torrent client available, catalog discovery disabled")
	}

	// Bootstrap DHT
	fmt.Println("[DHT] Starting DHT bootstrap process in background...")
	go dm.bootstrap()

	return dm, nil
}

func (dm *DHTManager) bootstrap() {
	fmt.Println("[DHT Bootstrap] Starting DHT network bootstrap...")
	
	// Use the DHT server's built-in bootstrap method
	fmt.Println("[DHT Bootstrap] Creating context with 30s timeout...")
	ctx, cancel := context.WithTimeout(dm.ctx, 30*time.Second)
	defer cancel()
	
	fmt.Println("[DHT Bootstrap] Calling BootstrapContext...")
	stats, err := dm.dhtServer.BootstrapContext(ctx)
	if err != nil {
		fmt.Printf("[DHT Bootstrap] Bootstrap error: %v\n", err)
		// Continue anyway, might still work
	} else {
		fmt.Printf("[DHT Bootstrap] Bootstrap completed successfully\n")
		fmt.Printf("[DHT Bootstrap] Stats: %+v\n", stats)
		if stats.NumResponses == 0 {
			fmt.Println("[DHT Bootstrap] WARNING: No responses from bootstrap nodes!")
			fmt.Println("[DHT Bootstrap] Possible causes:")
			fmt.Println("[DHT Bootstrap]   - Firewall blocking UDP port (try: sudo pfctl -d to disable macOS firewall temporarily)")
			fmt.Println("[DHT Bootstrap]   - Network connectivity issues")
			fmt.Println("[DHT Bootstrap]   - Bootstrap nodes may be down")
		}
	}
	
	// Give it a moment to stabilize
	fmt.Println("[DHT Bootstrap] Waiting 2 seconds for stabilization...")
	time.Sleep(2 * time.Second)
	
	// Do some random announces to populate the routing table
	fmt.Println("[DHT Bootstrap] Performing random announces to populate routing table...")
	for i := 0; i < 3; i++ {
		var randomHash [20]byte
		for j := range randomHash {
			randomHash[j] = byte(i * 20 + j)
		}
		fmt.Printf("[DHT Bootstrap] Announcing random hash %d\n", i+1)
		dm.dhtServer.Announce(randomHash, 0, true)
	}
	
	// Report final stats
	nodeCount := dm.GetNodeCount()
	fmt.Printf("[DHT Bootstrap] DHT initialized with %d nodes\n", nodeCount)
	if nodeCount == 0 {
		fmt.Println("[DHT Bootstrap] WARNING: No DHT nodes found after bootstrap!")
		fmt.Println("[DHT Bootstrap] This may indicate:")
		fmt.Println("[DHT Bootstrap]   - Network connectivity issues")
		fmt.Println("[DHT Bootstrap]   - Firewall blocking UDP traffic")
		fmt.Println("[DHT Bootstrap]   - Bootstrap nodes are unreachable")
	}
	
	// Continue to periodically refresh
	go dm.periodicBootstrap()
}

func (dm *DHTManager) periodicBootstrap() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-dm.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(dm.ctx, 30*time.Second)
			_, err := dm.dhtServer.BootstrapContext(ctx)
			if err != nil {
				fmt.Printf("Periodic DHT bootstrap error: %v\n", err)
			}
			cancel()
		}
	}
}

func (dm *DHTManager) AnnounceModel(announcement *types.ModelAnnouncement) error {
	fmt.Printf("[DHTManager] AnnounceModel called for: %s (InfoHash: %s)\n", announcement.Name, announcement.InfoHash)
	
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Store announcement for refresh
	dm.announcements[announcement.InfoHash] = announcement
	dm.lastAnnounce[announcement.InfoHash] = time.Now()
	fmt.Printf("[DHTManager] Stored announcement for periodic refresh\n")

	// Add to catalog if available
	if dm.catalogRef != nil {
		fmt.Printf("[DHTManager] Adding model to catalog torrent...\n")
		if err := dm.catalogRef.AddModel(announcement.Name, announcement.InfoHash, announcement.Size); err != nil {
			fmt.Printf("[DHTManager] Catalog update failed: %v\n", err)
			return fmt.Errorf("failed to add model to catalog: %w", err)
		}
		fmt.Printf("[DHTManager] Successfully added model %s to catalog\n", announcement.Name)
	}
	return nil
}

func (dm *DHTManager) RefreshAnnouncements() error {
	dm.mu.RLock()
	announcements := make([]*types.ModelAnnouncement, 0, len(dm.announcements))
	for _, ann := range dm.announcements {
		// Only refresh if it's been more than 25 minutes since last announce
		if time.Since(dm.lastAnnounce[ann.InfoHash]) > 25*time.Minute {
			announcements = append(announcements, ann)
		}
	}
	dm.mu.RUnlock()

	for _, ann := range announcements {
		if dm.catalogRef != nil {
			if err := dm.catalogRef.AddModel(ann.Name, ann.InfoHash, ann.Size); err != nil {
				fmt.Printf("Failed to refresh announcement for %s: %v\n", ann.Name, err)
				continue
			}
		}
		
		dm.mu.Lock()
		dm.lastAnnounce[ann.InfoHash] = time.Now()
		dm.mu.Unlock()
		
		fmt.Printf("Refreshed DHT announcement for %s\n", ann.Name)
	}

	return nil
}

// RefreshSeedingModels refreshes the BEP44 catalog entries for currently seeded models
func (dm *DHTManager) RefreshSeedingModels() error {
	fmt.Println("[DHT] Starting periodic catalog refresh for seeded models...")
	
	// Get currently seeded models from torrent manager
	seedingTorrents := dm.torrentManager.GetSeedingModels()
	if len(seedingTorrents) == 0 {
		fmt.Println("[DHT] No models currently seeding, skipping catalog refresh")
		return nil
	}
	
	fmt.Printf("[DHT] Found %d seeding models to refresh in catalog\n", len(seedingTorrents))
	
	// Refresh each seeded model in the catalog
	refreshed := 0
	for _, mt := range seedingTorrents {
		fmt.Printf("[DHT] Refreshing catalog entry for seeded model: %s\n", mt.Name)
		
		// Re-add model to keep the catalog entry alive
		if err := dm.catalogRef.AddModel(mt.Name, mt.InfoHash, 0); err != nil {
			fmt.Printf("[DHT] Failed to refresh catalog entry for %s: %v\n", mt.Name, err)
			continue
		}
		
		refreshed++
		fmt.Printf("[DHT] Successfully refreshed catalog entry for %s\n", mt.Name)
	}
	
	fmt.Printf("[DHT] Catalog refresh complete: %d/%d models refreshed\n", refreshed, len(seedingTorrents))
	return nil
}

func (dm *DHTManager) DiscoverModels(pattern string) ([]*types.ModelAnnouncement, error) {
	if dm.catalogRef == nil {
		return nil, fmt.Errorf("catalog not available")
	}
	
	// Use catalog for discovery
	results, err := dm.catalogRef.GetModels(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to discover models: %w", err)
	}

	return results, nil
}

func (dm *DHTManager) GetNodeCount() int {
	if dm.dhtServer == nil {
		fmt.Println("[DHT] GetNodeCount: DHT server is nil")
		return 0
	}
	
	stats := dm.dhtServer.Stats()
	fmt.Printf("[DHT] GetNodeCount: Nodes=%d, GoodNodes=%d\n", stats.Nodes, stats.GoodNodes)
	return stats.Nodes
}

// GetCatalogRef returns the BEP44 catalog reference manager
func (dm *DHTManager) GetCatalogRef() *discovery.BEP44CatalogRef {
	return dm.catalogRef
}

func (dm *DHTManager) GetStats() map[string]interface{} {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	stats := make(map[string]interface{})
	
	if dm.dhtServer != nil {
		dhtStats := dm.dhtServer.Stats()
		stats["nodes"] = dhtStats.Nodes
		stats["good_nodes"] = dhtStats.GoodNodes
		// These fields may not exist in the DHT stats
		stats["torrents"] = 0
		stats["peers"] = 0
	}
	
	stats["announcements"] = len(dm.announcements)
	stats["last_refresh"] = dm.getLastRefreshTime()
	
	return stats
}

func (dm *DHTManager) getLastRefreshTime() *time.Time {
	var lastTime *time.Time
	for _, t := range dm.lastAnnounce {
		if lastTime == nil || t.After(*lastTime) {
			lastTime = &t
		}
	}
	return lastTime
}

func (dm *DHTManager) Stop() {
	dm.cancel()
	
	// Final announcement to ensure peers know we're going offline
	if dm.catalogRef != nil {
		for _, ann := range dm.announcements {
			// Best effort - don't worry about errors during shutdown
			_ = dm.catalogRef.AddModel(ann.Name, ann.InfoHash, ann.Size)
		}
	}
	
	// Close the DHT connection
	if dm.dhtConn != nil {
		dm.dhtConn.Close()
	}
}

// AddTorrentToDHT adds a torrent to DHT for peer discovery
func (dm *DHTManager) AddTorrentToDHT(t *torrent.Torrent) {
	if dm.dhtServer == nil {
		return
	}
	
	// The torrent client handles DHT announce automatically
	// This is just for tracking
	fmt.Printf("Added torrent %s to DHT for peer discovery\n", t.Name())
}

// RemoveTorrentFromDHT removes a torrent from DHT
func (dm *DHTManager) RemoveTorrentFromDHT(infoHash string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	delete(dm.announcements, infoHash)
	delete(dm.lastAnnounce, infoHash)
}