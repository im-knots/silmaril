package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandlers(t *testing.T) (*Handlers, *daemon.Daemon) {
	// Set test mode for Gin
	gin.SetMode(gin.TestMode)

	// Create temporary directory for test
	tmpDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tmpDir)
	t.Cleanup(func() {
		os.Unsetenv("SILMARIL_HOME")
	})

	// Create minimal config
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tmpDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false, // Disable for tests
			ListenPort: 0,
		},
	}

	// Create daemon
	d, err := daemon.New(cfg)
	require.NoError(t, err)

	// Create handlers
	h := NewHandlers(d)

	return h, d
}

func TestHealthEndpoint(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()

	// Create test router
	router := gin.New()
	router.GET("/health", h.Health)

	// Create request
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Check response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "healthy", response["status"])
	assert.Contains(t, response, "time")
}

func TestStatusEndpoint(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()

	// Create test router
	router := gin.New()
	router.GET("/status", h.Status)

	// Create request
	req, _ := http.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Check response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify status fields
	assert.Contains(t, response, "pid")
	assert.Contains(t, response, "uptime")
	assert.Contains(t, response, "active_transfers")
	assert.Contains(t, response, "total_peers")
	assert.Contains(t, response, "dht_nodes")
}

func TestShutdownEndpoint(t *testing.T) {
	h, _ := setupTestHandlers(t)
	// Don't defer shutdown since the endpoint will trigger it

	// Create test router
	router := gin.New()
	router.POST("/shutdown", h.Shutdown)

	// Create request
	req, _ := http.NewRequest("POST", "/shutdown", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Check response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "daemon shutting down", response["message"])
}