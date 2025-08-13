package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonNew(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	// Create minimal config
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: true,
			ListenPort: 0,
		},
	}

	// Create daemon
	d, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, d)

	// Verify components are initialized
	assert.NotNil(t, d.torrentManager)
	assert.NotNil(t, d.dhtManager)
	assert.NotNil(t, d.transferManager)
	assert.NotNil(t, d.state)

	// Clean up
	d.Shutdown()
}

func TestDaemonLockFile(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
	}

	// Create first daemon
	d1, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, d1)

	// Try to create second daemon - should fail
	d2, err := New(cfg)
	assert.Error(t, err)
	assert.Nil(t, d2)
	assert.Contains(t, err.Error(), "already running")

	// Clean up first daemon
	d1.Shutdown()

	// Now second daemon should succeed
	d3, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, d3)
	d3.Shutdown()
}

func TestDaemonPIDFile(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
	}

	// Create daemon
	d, err := New(cfg)
	require.NoError(t, err)

	// Check PID file exists
	pidFile := filepath.Join(tmpDir, "daemon", "daemon.pid")
	assert.FileExists(t, pidFile)

	// Read PID file
	pidData, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	assert.NotEmpty(t, pidData)

	// Clean up
	d.Shutdown()

	// PID file should be removed
	assert.NoFileExists(t, pidFile)
}

func TestDaemonGetters(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
	}

	// Create daemon
	d, err := New(cfg)
	require.NoError(t, err)

	// Test getters
	assert.NotNil(t, d.GetTorrentManager())
	assert.NotNil(t, d.GetDHTManager())
	assert.NotNil(t, d.GetTransferManager())
	assert.NotNil(t, d.GetState())

	// Test status
	status := d.GetStatus()
	assert.NotNil(t, status)
	assert.Contains(t, status, "pid")
	assert.Contains(t, status, "uptime")
	assert.Contains(t, status, "active_transfers")
	assert.Contains(t, status, "total_peers")
	assert.Contains(t, status, "dht_nodes")

	// Clean up
	d.Shutdown()
}

func TestDaemonBackgroundWorkers(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
	}

	// Create daemon
	d, err := New(cfg)
	require.NoError(t, err)

	// Start workers in a controlled way
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Override daemon context for testing
	d.ctx = ctx
	d.cancel = cancel

	// Start workers
	d.startWorkers()

	// Wait a bit for workers to start
	time.Sleep(50 * time.Millisecond)

	// Workers should be running
	// We can't easily test the workers directly, but we can ensure they don't panic

	// Wait for context to expire
	<-ctx.Done()

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good, workers finished
	case <-time.After(1 * time.Second):
		t.Error("Workers did not finish in time")
	}

	// Clean up
	d.Shutdown()
}

func TestDaemonSetAPIHandler(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
	}

	// Create daemon
	d, err := New(cfg)
	require.NoError(t, err)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	// Set API handler
	d.SetAPIHandler(testHandler)

	// Clean up
	d.Shutdown()
}