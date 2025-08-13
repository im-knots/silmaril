package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverModels(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Create request with pattern
	req, _ := http.NewRequest("GET", "/discover?pattern=test", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	// Should have models array
	assert.Contains(t, response, "models")
	models, ok := response["models"].([]interface{})
	assert.True(t, ok)
	assert.NotNil(t, models)
	
	// Should have count
	assert.Contains(t, response, "count")
}

func TestDiscoverModelsNoPattern(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Create request without pattern (should use *)
	req, _ := http.NewRequest("GET", "/discover", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	// Should still work with default pattern
	assert.Contains(t, response, "models")
	assert.Contains(t, response, "count")
}

func TestDiscoverModelsEmptyPattern(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Create request with empty pattern
	req, _ := http.NewRequest("GET", "/discover?pattern=", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	// Should default to * pattern
	assert.Contains(t, response, "models")
	assert.Contains(t, response, "count")
	assert.Contains(t, response, "pattern")
	
	// Pattern should be *
	pattern, ok := response["pattern"].(string)
	assert.True(t, ok)
	assert.Equal(t, "*", pattern)
}

func TestDiscoverModelsWithSpecialCharacters(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Create request with special characters in pattern
	req, _ := http.NewRequest("GET", "/discover?pattern=test%2Fmodel%2A", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	// Should handle special characters
	assert.Contains(t, response, "models")
	assert.Contains(t, response, "pattern")
	
	pattern, _ := response["pattern"].(string)
	assert.Equal(t, "test/model*", pattern)
}

func TestDiscoverModelsTimeout(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Create request with timeout parameter
	req, _ := http.NewRequest("GET", "/discover?pattern=test&timeout=1", nil)
	w := httptest.NewRecorder()
	
	// Execute request
	router.ServeHTTP(w, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	
	// Should complete even with short timeout
	assert.Contains(t, response, "models")
}

func TestDiscoverModelsConcurrent(t *testing.T) {
	h, d := setupTestHandlers(t)
	defer d.Shutdown()
	
	// Create test router
	router := gin.New()
	router.GET("/discover", h.DiscoverModels)
	
	// Test concurrent requests
	done := make(chan bool, 3)
	
	for i := 0; i < 3; i++ {
		go func(id int) {
			req, _ := http.NewRequest("GET", "/discover?pattern=test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			
			assert.Equal(t, http.StatusOK, w.Code)
			done <- true
		}(i)
	}
	
	// Wait for all requests
	for i := 0; i < 3; i++ {
		<-done
	}
}