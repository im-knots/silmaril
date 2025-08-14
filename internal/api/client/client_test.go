package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:8080")
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestClientHealth(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
			"time":   "2024-01-01T00:00:00Z",
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.Health()
	assert.NoError(t, err)
}

func TestClientHealthError(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.Health()
	assert.Error(t, err)
}

func TestClientGetStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/status", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"pid":              12345,
			"uptime":           "1h30m",
			"active_transfers": 5,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	status, err := client.GetStatus()
	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, float64(12345), status["pid"])
}

func TestClientListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "model1", "size": 1000},
				{"name": "model2", "size": 2000},
			},
			"count": 2,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	models, err := client.ListModels()
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Equal(t, "model1", models[0]["name"])
}

func TestClientGetModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models/test-model", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "test-model",
			"size": 1000,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	model, err := client.GetModel("test-model")
	require.NoError(t, err)
	assert.Equal(t, "test-model", model["name"])
}

func TestClientGetModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	_, err := client.GetModel("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClientDownloadModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models/download", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "test-model", req["model_name"])
		assert.Equal(t, "hash123", req["info_hash"])
		assert.Equal(t, true, req["seed"])
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transfer_id": "transfer-123",
			"model_name":  "test-model",
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	result, err := client.DownloadModel("test-model", "hash123", true)
	require.NoError(t, err)
	assert.Equal(t, "transfer-123", result["transfer_id"])
}

func TestClientShareModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models/share", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "test-model", req["model_name"])
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "started sharing",
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	result, err := client.ShareModel(ShareModelOptions{
		ModelName: "test-model",
		Path:      "",
		All:       false,
	})
	require.NoError(t, err)
	assert.Equal(t, "started sharing", result["message"])
}


func TestClientRemoveModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models/test-model", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "model removed",
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.RemoveModel("test-model")
	assert.NoError(t, err)
}

func TestClientDiscoverModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/discover", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "test-pattern", r.URL.Query().Get("pattern"))
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "discovered-model", "info_hash": "hash123"},
			},
			"count": 1,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	models, err := client.DiscoverModels("test-pattern")
	require.NoError(t, err)
	assert.Len(t, models, 1)
	assert.Equal(t, "discovered-model", models[0]["name"])
}

func TestClientGetTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/transfers/transfer-123", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "transfer-123",
			"status": "active",
			"progress": 50.5,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	transfer, err := client.GetTransfer("transfer-123")
	require.NoError(t, err)
	assert.Equal(t, "transfer-123", transfer["id"])
	assert.Equal(t, "active", transfer["status"])
}

func TestClientListTransfers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/transfers", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "active", r.URL.Query().Get("status"))
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transfers": []map[string]interface{}{
				{"id": "t1", "status": "active"},
				{"id": "t2", "status": "active"},
			},
			"count": 2,
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	transfers, err := client.ListTransfers("active")
	require.NoError(t, err)
	assert.Len(t, transfers, 2)
}

func TestClientPauseTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/transfers/transfer-123/pause", r.URL.Path)
		assert.Equal(t, "PUT", r.Method)
		
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.PauseTransfer("transfer-123")
	assert.NoError(t, err)
}

func TestClientResumeTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/transfers/transfer-123/resume", r.URL.Path)
		assert.Equal(t, "PUT", r.Method)
		
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.ResumeTransfer("transfer-123")
	assert.NoError(t, err)
}

func TestClientCancelTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/transfers/transfer-123", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.CancelTransfer("transfer-123")
	assert.NoError(t, err)
}

func TestClientShutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/admin/shutdown", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "daemon shutting down",
		})
	}))
	defer server.Close()
	
	client := NewClient(server.URL)
	err := client.Shutdown()
	assert.NoError(t, err)
}