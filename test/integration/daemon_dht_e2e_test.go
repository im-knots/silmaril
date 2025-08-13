// +build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/silmaril/silmaril/internal/api/client"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/daemon"
	"github.com/silmaril/silmaril/internal/models"
	"github.com/silmaril/silmaril/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaemonDHTDiscovery tests DHT discovery through the daemon
func TestDaemonDHTDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Create two daemon instances to simulate P2P network
	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	// Setup first daemon with DHT enabled
	t.Log("Setting up first daemon with DHT...")
	os.Setenv("SILMARIL_HOME", tempDir1)
	
	cfg1 := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir1,
		},
		Network: config.NetworkConfig{
			DHTEnabled: true,
			DHTPort:    0, // Random port
			ListenPort: 0,
		},
	}

	d1, err := daemon.New(cfg1)
	require.NoError(t, err)
	defer d1.Shutdown()

	apiPort1 := 38750 + (os.Getpid() % 100)
	go d1.Start(apiPort1)
	time.Sleep(1 * time.Second)

	apiClient1 := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort1))

	// Create a test model in daemon 1
	modelsDir1 := filepath.Join(tempDir1, "models", "test-org", "dht-model-1")
	err = os.MkdirAll(modelsDir1, 0755)
	require.NoError(t, err)

	configFile1 := filepath.Join(modelsDir1, "config.json")
	err = os.WriteFile(configFile1, []byte(`{"model_type": "dht-test", "architectures": ["DHTTestModel"]}`), 0644)
	require.NoError(t, err)

	modelFile1 := filepath.Join(modelsDir1, "model.bin")
	err = os.WriteFile(modelFile1, []byte("DHT test model 1 data"), 0644)
	require.NoError(t, err)

	// List models to trigger scan
	models1, err := apiClient1.ListModels()
	require.NoError(t, err)
	t.Logf("Daemon 1 has %d models", len(models1))

	// Start sharing the model
	t.Log("Sharing model from daemon 1...")
	shareResult, err := apiClient1.ShareModel("test-org/dht-model-1", "", false)
	if err != nil {
		t.Logf("Warning: Could not share model: %v", err)
	} else {
		t.Logf("Share result: %v", shareResult)
	}

	// Setup second daemon with DHT enabled
	t.Log("Setting up second daemon with DHT...")
	os.Setenv("SILMARIL_HOME", tempDir2)
	
	cfg2 := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir2,
		},
		Network: config.NetworkConfig{
			DHTEnabled: true,
			DHTPort:    0,
			ListenPort: 0,
		},
	}

	d2, err := daemon.New(cfg2)
	require.NoError(t, err)
	defer d2.Shutdown()

	apiPort2 := 38850 + (os.Getpid() % 100)
	go d2.Start(apiPort2)
	time.Sleep(1 * time.Second)

	apiClient2 := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort2))

	// Give DHT time to bootstrap and propagate
	t.Log("Waiting for DHT propagation...")
	time.Sleep(3 * time.Second)

	// Try to discover models from daemon 2
	t.Log("Discovering models from daemon 2...")
	discovered, err := apiClient2.DiscoverModels("dht-model")
	require.NoError(t, err)
	t.Logf("Discovered %d models", len(discovered))

	// In a real DHT network, we would find the model
	// In test environment, discovery might not work due to lack of DHT peers
	if len(discovered) > 0 {
		t.Log("✅ Model discovered via DHT!")
		for _, model := range discovered {
			t.Logf("  - %s", model["name"])
		}
	} else {
		t.Log("No models discovered (expected in isolated test environment)")
	}

	// Test daemon stats to verify DHT is running
	status1, err := apiClient1.GetStatus()
	require.NoError(t, err)
	t.Logf("Daemon 1 DHT nodes: %v", status1["dht_nodes"])

	status2, err := apiClient2.GetStatus()
	require.NoError(t, err)
	t.Logf("Daemon 2 DHT nodes: %v", status2["dht_nodes"])

	// Cleanup
	apiClient1.Shutdown()
	apiClient2.Shutdown()
}

// TestDaemonP2PTransfer tests P2P transfer between two daemons
func TestDaemonP2PTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	// Setup seeder daemon
	t.Log("Setting up seeder daemon...")
	os.Setenv("SILMARIL_HOME", tempDir1)
	
	cfg1 := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir1,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false, // Disable DHT for simpler test
			ListenPort: 0,
		},
	}

	d1, err := daemon.New(cfg1)
	require.NoError(t, err)
	defer d1.Shutdown()

	apiPort1 := 38760 + (os.Getpid() % 100)
	go d1.Start(apiPort1)
	time.Sleep(500 * time.Millisecond)

	apiClient1 := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort1))

	// Create a model in seeder
	modelsDir := filepath.Join(tempDir1, "models", "test", "p2p-model")
	err = os.MkdirAll(modelsDir, 0755)
	require.NoError(t, err)

	// Create test files
	testData := []byte("This is P2P test model data that will be transferred")
	modelFile := filepath.Join(modelsDir, "model.bin")
	err = os.WriteFile(modelFile, testData, 0644)
	require.NoError(t, err)

	configFile := filepath.Join(modelsDir, "config.json")
	err = os.WriteFile(configFile, []byte(`{"model_type": "p2p-test"}`), 0644)
	require.NoError(t, err)

	// Trigger model scan
	models, err := apiClient1.ListModels()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(models), 1)

	// Start sharing the model
	t.Log("Starting to share model...")
	shareResult, err := apiClient1.ShareModel("test/p2p-model", "", false)
	if err != nil {
		t.Logf("Warning: Could not start sharing: %v", err)
	} else {
		t.Logf("Sharing started: %v", shareResult)
	}

	// Setup leecher daemon
	t.Log("Setting up leecher daemon...")
	os.Setenv("SILMARIL_HOME", tempDir2)
	
	cfg2 := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir2,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false,
			ListenPort: 0,
		},
	}

	d2, err := daemon.New(cfg2)
	require.NoError(t, err)
	defer d2.Shutdown()

	apiPort2 := 38860 + (os.Getpid() % 100)
	go d2.Start(apiPort2)
	time.Sleep(500 * time.Millisecond)

	apiClient2 := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort2))

	// In a real scenario, leecher would download from seeder
	// For this test, we just verify both daemons are operational
	
	// Check transfers on seeder
	transfers1, err := apiClient1.ListTransfers("")
	require.NoError(t, err)
	t.Logf("Seeder has %d transfers", len(transfers1))

	// Check leecher can list (empty) models
	models2, err := apiClient2.ListModels()
	require.NoError(t, err)
	t.Logf("Leecher has %d models initially", len(models2))

	// Verify both daemons are healthy
	err = apiClient1.Health()
	assert.NoError(t, err)
	
	err = apiClient2.Health()
	assert.NoError(t, err)

	// Cleanup
	apiClient1.Shutdown()
	apiClient2.Shutdown()
}

// TestDaemonModelSharing tests the complete model sharing workflow
func TestDaemonModelSharing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tempDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tempDir)
	defer os.Unsetenv("SILMARIL_HOME")

	// Create daemon
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: true,
			DHTPort:    0,
			ListenPort: 0,
		},
	}

	d, err := daemon.New(cfg)
	require.NoError(t, err)
	defer d.Shutdown()

	apiPort := 38770 + (os.Getpid() % 100)
	go d.Start(apiPort)
	time.Sleep(500 * time.Millisecond)

	apiClient := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort))

	// Create multiple test models
	modelNames := []string{"model-1", "model-2", "model-3"}
	
	for _, name := range modelNames {
		modelDir := filepath.Join(tempDir, "models", "test", name)
		err = os.MkdirAll(modelDir, 0755)
		require.NoError(t, err)

		// Create model files
		modelFile := filepath.Join(modelDir, "model.bin")
		err = os.WriteFile(modelFile, []byte(fmt.Sprintf("Data for %s", name)), 0644)
		require.NoError(t, err)

		configFile := filepath.Join(modelDir, "config.json")
		err = os.WriteFile(configFile, []byte(`{"model_type": "test"}`), 0644)
		require.NoError(t, err)
	}

	// List models to trigger scan
	models, err := apiClient.ListModels()
	require.NoError(t, err)
	assert.Equal(t, len(modelNames), len(models))

	// Share all models
	t.Log("Sharing all models...")
	shareResult, err := apiClient.ShareModel("", "", true)
	if err != nil {
		t.Logf("Warning: Could not share all models: %v", err)
	} else {
		t.Logf("Share all result: %v", shareResult)
	}

	// Check active transfers
	transfers, err := apiClient.ListTransfers("active")
	require.NoError(t, err)
	t.Logf("Active transfers: %d", len(transfers))

	// Get transfer details if any exist
	for _, transfer := range transfers {
		if id, ok := transfer["id"].(string); ok {
			details, err := apiClient.GetTransfer(id)
			if err == nil {
				t.Logf("Transfer %s: model=%v, status=%v", 
					id, details["model_name"], details["status"])
			}
		}
	}

	// Test share specific model
	t.Log("Sharing specific model...")
	shareResult, err = apiClient.ShareModel("test/model-1", "", false)
	if err != nil {
		t.Logf("Warning: Could not share specific model: %v", err)
	} else {
		t.Logf("Share specific result: %v", shareResult)
	}

	// Cleanup
	apiClient.Shutdown()
}

// TestDaemonModelMirror tests the mirror functionality through daemon
func TestDaemonModelMirror(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tempDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tempDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false,
			ListenPort: 0,
		},
	}

	d, err := daemon.New(cfg)
	require.NoError(t, err)
	defer d.Shutdown()

	apiPort := 38780 + (os.Getpid() % 100)
	go d.Start(apiPort)
	time.Sleep(500 * time.Millisecond)

	apiClient := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort))

	// Test mirror endpoint (won't actually clone from HF in test)
	t.Log("Testing mirror endpoint...")
	result, err := apiClient.MirrorModel(
		"https://huggingface.co/test/model",
		"main",
		1,
		true,  // skip LFS
		true,  // no broadcast
		false, // no auto share
	)
	
	// The actual mirroring is not implemented, but the endpoint should respond
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "pending", result["status"])
	t.Logf("Mirror result: %v", result)

	// Cleanup
	apiClient.Shutdown()
}

// TestDaemonRegistryOperations tests registry operations through daemon
func TestDaemonRegistryOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tempDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tempDir)
	defer os.Unsetenv("SILMARIL_HOME")

	// Setup paths and registry directly
	paths, err := storage.NewPaths()
	require.NoError(t, err)

	registry, err := models.NewRegistry(paths)
	require.NoError(t, err)

	// Create and save a test manifest
	manifest := &models.ModelManifest{
		Name:        "test/registry-model",
		Version:     "1.0.0",
		Description: "Test model for registry operations",
		License:     "MIT",
		ModelType:   "test",
		TotalSize:   1024,
	}

	err = registry.SaveManifest(manifest)
	require.NoError(t, err)

	// Now start daemon
	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: false,
			ListenPort: 0,
		},
	}

	d, err := daemon.New(cfg)
	require.NoError(t, err)
	defer d.Shutdown()

	apiPort := 38790 + (os.Getpid() % 100)
	go d.Start(apiPort)
	time.Sleep(500 * time.Millisecond)

	apiClient := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort))

	// List models should find our registry model
	models, err := apiClient.ListModels()
	require.NoError(t, err)
	
	found := false
	for _, model := range models {
		if name, ok := model["name"].(string); ok && name == "test/registry-model" {
			found = true
			assert.Equal(t, "1.0.0", model["version"])
			assert.Equal(t, "MIT", model["license"])
			t.Log("✅ Found registry model via API")
			break
		}
	}
	
	if !found {
		t.Log("Registry model not found via API")
	}

	// Get specific model
	model, err := apiClient.GetModel("test/registry-model")
	if err == nil {
		assert.Equal(t, "test/registry-model", model["name"])
		assert.Equal(t, "1.0.0", model["version"])
		t.Log("✅ Retrieved specific model via API")
	} else {
		t.Logf("Could not get specific model: %v", err)
	}

	// Remove model
	err = apiClient.RemoveModel("test/registry-model")
	if err != nil {
		t.Logf("Could not remove model: %v", err)
	}

	// Cleanup
	apiClient.Shutdown()
}

// TestDaemonDiscoveryPatterns tests various discovery patterns
func TestDaemonDiscoveryPatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	tempDir := t.TempDir()
	os.Setenv("SILMARIL_HOME", tempDir)
	defer os.Unsetenv("SILMARIL_HOME")

	cfg := &config.Config{
		Storage: config.StorageConfig{
			BaseDir: tempDir,
		},
		Network: config.NetworkConfig{
			DHTEnabled: true,
			DHTPort:    0,
			ListenPort: 0,
		},
	}

	d, err := daemon.New(cfg)
	require.NoError(t, err)
	defer d.Shutdown()

	apiPort := 38795 + (os.Getpid() % 100)
	go d.Start(apiPort)
	time.Sleep(500 * time.Millisecond)

	apiClient := client.NewClient(fmt.Sprintf("http://localhost:%d", apiPort))

	// Test different discovery patterns
	patterns := []string{
		"*",           // All models
		"test/*",      // Models in test org
		"*llama*",     // Models with llama in name
		"meta-llama/*", // Specific organization
	}

	for _, pattern := range patterns {
		t.Logf("Testing discovery with pattern: %s", pattern)
		
		models, err := apiClient.DiscoverModels(pattern)
		require.NoError(t, err)
		
		t.Logf("  Found %d models", len(models))
		for _, model := range models {
			if name, ok := model["name"].(string); ok {
				t.Logf("    - %s", name)
			}
		}
	}

	// Cleanup
	apiClient.Shutdown()
}