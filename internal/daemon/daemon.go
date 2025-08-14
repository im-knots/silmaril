package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/storage"
)

type Daemon struct {
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	config          *config.Config
	torrentManager  *TorrentManager
	dhtManager      *DHTManager
	transferManager *TransferManager
	state           *State
	server          *http.Server
	apiHandler      http.Handler  // Store the API handler
	workers         sync.WaitGroup
}

func New(cfg *config.Config) (*Daemon, error) {
	fmt.Println("[DEBUG] Creating new daemon instance...")
	ctx, cancel := context.WithCancel(context.Background())
	
	baseDir := storage.GetBaseDir()
	daemonDir := filepath.Join(baseDir, "daemon")
	fmt.Printf("[DEBUG] Daemon directory: %s\n", daemonDir)
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create daemon directory: %w", err)
	}

	d := &Daemon{
		ctx:      ctx,
		cancel:   cancel,
		config:   cfg,
	}

	// Initialize state
	d.state = NewState(filepath.Join(daemonDir, "state.json"))
	if err := d.state.Load(); err != nil {
		// Non-fatal: just log and continue with empty state
		fmt.Printf("Warning: could not load previous state: %v\n", err)
	}

	// Initialize managers
	var err error
	fmt.Println("[DEBUG] Initializing torrent manager...")
	d.torrentManager, err = NewTorrentManager(cfg, d.state)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize torrent manager: %w", err)
	}
	fmt.Println("[DEBUG] Torrent manager initialized")

	fmt.Println("[DEBUG] Initializing DHT manager...")
	d.dhtManager, err = NewDHTManager(cfg, d.torrentManager)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize DHT manager: %w", err)
	}
	fmt.Printf("[DEBUG] DHT manager initialized with %d nodes\n", d.dhtManager.GetNodeCount())

	d.transferManager = NewTransferManager(d.torrentManager, d.state)

	return d, nil
}

func (d *Daemon) Start(apiPort int) error {
	fmt.Printf("[DEBUG] Starting daemon on port %d...\n", apiPort)
	
	// Start background workers
	fmt.Println("[DEBUG] Starting background workers...")
	d.startWorkers()

	// Start HTTP API server
	fmt.Printf("[DEBUG] Starting API server on port %d...\n", apiPort)
	if err := d.startAPIServer(apiPort); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}

	// Setup signal handlers
	fmt.Println("[DEBUG] Setting up signal handlers...")
	d.setupSignalHandlers()

	fmt.Printf("Daemon started on port %d (PID: %d)\n", apiPort, os.Getpid())
	fmt.Printf("[DEBUG] Initial DHT nodes: %d\n", d.dhtManager.GetNodeCount())
	
	return nil
}

func (d *Daemon) startWorkers() {
	// DHT announcement worker
	d.workers.Add(1)
	go d.dhtAnnouncementWorker()

	// Catalog refresh worker
	d.workers.Add(1)
	go d.catalogRefreshWorker()

	// State persistence worker
	d.workers.Add(1)
	go d.statePersistenceWorker()

	// Cleanup worker
	d.workers.Add(1)
	go d.cleanupWorker()

	// Stats collection worker
	d.workers.Add(1)
	go d.statsWorker()
}

func (d *Daemon) dhtAnnouncementWorker() {
	defer d.workers.Done()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.dhtManager.RefreshAnnouncements(); err != nil {
				fmt.Printf("Error refreshing DHT announcements: %v\n", err)
			}
		}
	}
}

func (d *Daemon) catalogRefreshWorker() {
	defer d.workers.Done()
	
	// Make interval configurable, default to 30 minutes
	interval := 30 * time.Minute
	if d.config != nil {
		if configInterval := d.config.GetInt("network.catalog_refresh_interval_minutes"); configInterval > 0 {
			interval = time.Duration(configInterval) * time.Minute
			fmt.Printf("[Daemon] Using configured catalog refresh interval: %v\n", interval)
		} else {
			fmt.Printf("[Daemon] Using default catalog refresh interval: %v\n", interval)
		}
	}
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	// Do an initial refresh after a short delay to allow daemon to stabilize
	initialDelay := 2 * time.Minute
	fmt.Printf("[Daemon] Catalog refresh worker started, initial refresh in %v, then every %v\n", initialDelay, interval)
	
	select {
	case <-d.ctx.Done():
		return
	case <-time.After(initialDelay):
		fmt.Println("[Daemon] Running initial catalog refresh...")
		if err := d.dhtManager.RefreshSeedingModels(); err != nil {
			fmt.Printf("[Daemon] Error in initial catalog refresh: %v\n", err)
		}
	}
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			fmt.Println("[Daemon] Running periodic catalog refresh...")
			if err := d.dhtManager.RefreshSeedingModels(); err != nil {
				fmt.Printf("[Daemon] Error refreshing seeded models catalog: %v\n", err)
			}
		}
	}
}

func (d *Daemon) statePersistenceWorker() {
	defer d.workers.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.state.Save(); err != nil {
				fmt.Printf("Error saving state: %v\n", err)
			}
		}
	}
}

func (d *Daemon) cleanupWorker() {
	defer d.workers.Done()
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.cleanupIncompleteDownloads()
		}
	}
}

func (d *Daemon) statsWorker() {
	defer d.workers.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.transferManager.UpdateStats()
		}
	}
}

func (d *Daemon) cleanupIncompleteDownloads() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cleanup logic for incomplete downloads
	transfers := d.transferManager.GetIncompleteTransfers()
	for _, t := range transfers {
		if time.Since(t.LastActivity) > 24*time.Hour {
			fmt.Printf("Cleaning up stale transfer: %s\n", t.ID)
			d.transferManager.CancelTransfer(t.ID)
		}
	}
}

func (d *Daemon) setupSignalHandlers() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal, shutting down gracefully...")
		d.cancel()
	}()
}

func (d *Daemon) Shutdown() error {
	fmt.Println("Shutting down daemon...")

	// Stop accepting new requests
	if d.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := d.server.Shutdown(ctx); err != nil {
			fmt.Printf("Error shutting down API server: %v\n", err)
		}
	}

	// Save state
	if err := d.state.Save(); err != nil {
		fmt.Printf("Error saving final state: %v\n", err)
	}

	// Stop torrent client
	if d.torrentManager != nil {
		d.torrentManager.Stop()
	}

	// Stop DHT
	if d.dhtManager != nil {
		d.dhtManager.Stop()
	}

	// Wait for workers to finish
	d.workers.Wait()

	fmt.Println("Daemon shutdown complete")
	return nil
}

func (d *Daemon) startAPIServer(port int) error {
	// Use the handler set by SetAPIHandler if available, otherwise use basic routes
	d.mu.RLock()
	customHandler := d.apiHandler
	d.mu.RUnlock()
	
	var handler http.Handler
	if customHandler != nil {
		fmt.Println("[DEBUG] Using custom API handler")
		handler = customHandler
	} else {
		fmt.Println("[DEBUG] Using basic API routes")
		handler = d.setupAPIRoutes()
	}
	
	d.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	
	go func() {
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("API server error: %v\n", err)
		}
	}()
	
	return nil
}

func (d *Daemon) setupAPIRoutes() http.Handler {
	// Import the API package to set up proper routes
	// Note: This creates a circular dependency that needs to be resolved
	// For now, we'll use a basic implementation
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Basic routing
		switch {
		case r.URL.Path == "/api/v1/health" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"healthy"}`))
			
		case r.URL.Path == "/api/v1/status" && r.Method == "GET":
			status := d.GetStatus()
			data, _ := json.Marshal(status)
			w.Write(data)
			
		case r.URL.Path == "/api/v1/models" && r.Method == "GET":
			// List models - return empty for now
			response := map[string]interface{}{
				"models": []interface{}{},
				"count":  0,
			}
			data, _ := json.Marshal(response)
			w.Write(data)
			
		case r.URL.Path == "/api/v1/admin/shutdown" && r.Method == "POST":
			// Shutdown daemon
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"daemon shutting down"}`))
			go func() {
				time.Sleep(100 * time.Millisecond)
				d.cancel()
			}()
			
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"endpoint not found"}`))
		}
	})
}

// SetAPIHandler sets a custom API handler for the daemon
func (d *Daemon) SetAPIHandler(handler http.Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	d.apiHandler = handler
	if d.server != nil {
		d.server.Handler = handler
	}
}

// GetStatus returns the current daemon status
func (d *Daemon) GetStatus() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"pid":              os.Getpid(),
		"uptime":           time.Since(d.state.StartTime).String(),
		"active_transfers": d.transferManager.GetActiveCount(),
		"total_peers":      d.torrentManager.GetTotalPeers(),
		"dht_nodes":        d.dhtManager.GetNodeCount(),
	}
}

// GetTorrentManager returns the torrent manager
func (d *Daemon) GetTorrentManager() *TorrentManager {
	return d.torrentManager
}

// GetDHTManager returns the DHT manager
func (d *Daemon) GetDHTManager() *DHTManager {
	return d.dhtManager
}

// GetTransferManager returns the transfer manager
func (d *Daemon) GetTransferManager() *TransferManager {
	return d.transferManager
}

// GetState returns the daemon state
func (d *Daemon) GetState() *State {
	return d.state
}