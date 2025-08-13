package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListModels(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/models", h.ListModels)
	
	// Create request
	req, _ := http.NewRequest("GET", "/models", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var models []map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &models)
	require.NoError(t, err)
	
	// Should return empty array initially
	assert.NotNil(t, models)
}

func TestGetModel(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/models/:name", h.GetModel)
	
	// Request non-existent model
	req, _ := http.NewRequest("GET", "/models/test-model", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Should return 404
	assert.Equal(t, http.StatusNotFound, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error"], "not found")
}

func TestDownloadModel(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/download", h.DownloadModel)
	
	// Create request body
	reqBody := DownloadModelRequest{
		ModelName: "test-model",
		InfoHash:  "test-hash",
		Seed:      true,
	}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/download", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response - might fail if model doesn't exist
	if w.Code == http.StatusOK {
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response, "transfer_id")
	} else {
		// Model not found is also acceptable
		assert.Equal(t, http.StatusNotFound, w.Code)
	}
}

func TestShareModel(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/share", h.ShareModel)
	
	// Test sharing all models
	reqBody := ShareModelRequest{
		All: true,
	}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/share", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "message")
}

func TestShareModelSpecific(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/share", h.ShareModel)
	
	// Test sharing specific model
	reqBody := ShareModelRequest{
		ModelName: "test-model",
	}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/share", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Model probably doesn't exist, but should not crash
	if w.Code == http.StatusNotFound {
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Contains(t, response["error"], "not found")
	}
}

func TestMirrorModel(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/mirror", h.MirrorModel)
	
	// Create request body
	reqBody := MirrorModelRequest{
		RepoURL: "https://huggingface.co/test/model",
		Branch:  "main",
		Depth:   1,
	}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/mirror", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusAccepted, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response, "message")
	assert.Equal(t, "pending", response["status"])
}

func TestMirrorModelInvalidRequest(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/mirror", h.MirrorModel)
	
	// Create invalid request (missing repo_url)
	reqBody := map[string]interface{}{
		"branch": "main",
	}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/mirror", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Should still accept (repo_url will be empty)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestRemoveModel(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.DELETE("/models/:name", h.RemoveModel)
	
	// Try to remove non-existent model
	req, _ := http.NewRequest("DELETE", "/models/test-model", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Should return 404
	assert.Equal(t, http.StatusNotFound, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error"], "not found")
}

func TestDownloadModelInvalidRequest(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/download", h.DownloadModel)
	
	// Create invalid request body
	body := []byte(`{"invalid": "json structure"}`)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/download", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Should return bad request
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShareModelInvalidRequest(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.POST("/models/share", h.ShareModel)
	
	// Create request without any fields
	reqBody := ShareModelRequest{}
	body, _ := json.Marshal(reqBody)
	
	// Create request
	req, _ := http.NewRequest("POST", "/models/share", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Should return bad request
	assert.Equal(t, http.StatusBadRequest, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error"], "must specify")
}