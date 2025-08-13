package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/silmaril/silmaril/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTransfers(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/transfers", h.ListTransfers)
	
	// Create request
	req, _ := http.NewRequest("GET", "/transfers", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response, "transfers")
	assert.Contains(t, response, "count")
	
	// Initially should be empty
	transfers, ok := response["transfers"].([]interface{})
	assert.True(t, ok)
	assert.NotNil(t, transfers)
	assert.Equal(t, float64(0), response["count"])
}

func TestListTransfersWithStatus(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/transfers", h.ListTransfers)
	
	// Create request with status filter
	req, _ := http.NewRequest("GET", "/transfers?status=active", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response, "transfers")
	assert.Contains(t, response, "count")
}

func TestGetTransfer(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create a transfer first
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	
	// Create test router
	router := gin.New()
	router.GET("/transfers/:id", h.GetTransfer)
	
	// Create request
	req, _ := http.NewRequest("GET", "/transfers/"+transfer.ID, nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Equal(t, transfer.ID, response["id"])
	assert.Equal(t, "download", response["type"])
}

func TestGetTransferNotFound(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/transfers/:id", h.GetTransfer)
	
	// Create request for non-existent transfer
	nonExistentID := uuid.New().String()
	req, _ := http.NewRequest("GET", "/transfers/"+nonExistentID, nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusNotFound, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response["error"], "not found")
}

func TestPauseTransfer(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create an active transfer
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	transfer.Status = daemon.TransferStatusActive
	
	// Create test router
	router := gin.New()
	router.PUT("/transfers/:id/pause", h.PauseTransfer)
	
	// Create request
	req, _ := http.NewRequest("PUT", "/transfers/"+transfer.ID+"/pause", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Equal(t, "transfer paused", response["message"])
	assert.Equal(t, transfer.ID, response["transfer_id"])
}

func TestPauseTransferInvalid(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create a paused transfer (can't pause again)
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	transfer.Status = daemon.TransferStatusPaused
	
	// Create test router
	router := gin.New()
	router.PUT("/transfers/:id/pause", h.PauseTransfer)
	
	// Create request
	req, _ := http.NewRequest("PUT", "/transfers/"+transfer.ID+"/pause", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response["error"], "not active")
}

func TestResumeTransfer(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create a paused transfer
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	transfer.Status = daemon.TransferStatusPaused
	
	// Create test router
	router := gin.New()
	router.PUT("/transfers/:id/resume", h.ResumeTransfer)
	
	// Create request
	req, _ := http.NewRequest("PUT", "/transfers/"+transfer.ID+"/resume", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Equal(t, "transfer resumed", response["message"])
	assert.Equal(t, transfer.ID, response["transfer_id"])
}

func TestResumeTransferInvalid(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create an active transfer (can't resume)
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	transfer.Status = daemon.TransferStatusActive
	
	// Create test router
	router := gin.New()
	router.PUT("/transfers/:id/resume", h.ResumeTransfer)
	
	// Create request
	req, _ := http.NewRequest("PUT", "/transfers/"+transfer.ID+"/resume", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response["error"], "not paused")
}

func TestCancelTransfer(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create a transfer
	tm := d.GetTransferManager()
	transfer := tm.CreateDownload("test-model", "test-hash", 1000000)
	
	// Create test router
	router := gin.New()
	router.DELETE("/transfers/:id", h.CancelTransfer)
	
	// Create request
	req, _ := http.NewRequest("DELETE", "/transfers/"+transfer.ID, nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Equal(t, "transfer cancelled", response["message"])
	assert.Equal(t, transfer.ID, response["transfer_id"])
	
	// Verify transfer was cancelled
	cancelledTransfer, exists := tm.GetTransfer(transfer.ID)
	assert.True(t, exists)
	assert.Equal(t, daemon.TransferStatusCancelled, cancelledTransfer.Status)
}

func TestCancelTransferNotFound(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.DELETE("/transfers/:id", h.CancelTransfer)
	
	// Create request for non-existent transfer
	nonExistentID := uuid.New().String()
	req, _ := http.NewRequest("DELETE", "/transfers/"+nonExistentID, nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	assert.Contains(t, response["error"], "not found")
}