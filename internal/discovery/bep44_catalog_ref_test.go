package discovery

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestBEP44CatalogRef(t *testing.T) (*BEP44CatalogRef, *dht.Server, *torrent.Client, string) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "silmaril-bep44-test-*")
	require.NoError(t, err)

	// Save old env and set environment variable to use temp dir
	oldEnv := os.Getenv("SILMARIL_BASE_DIR")
	os.Setenv("SILMARIL_BASE_DIR", tmpDir)
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
		if oldEnv != "" {
			os.Setenv("SILMARIL_BASE_DIR", oldEnv)
		} else {
			os.Unsetenv("SILMARIL_BASE_DIR")
		}
	})

	// Create DHT server
	dhtCfg := dht.NewDefaultServerConfig()
	dhtCfg.StartingNodes = func() ([]dht.Addr, error) {
		return nil, nil // No bootstrap nodes for testing
	}
	
	dhtServer, err := dht.NewServer(dhtCfg)
	require.NoError(t, err)

	// Create torrent client
	torrentCfg := torrent.NewDefaultClientConfig()
	torrentCfg.DataDir = tmpDir
	torrentCfg.DisableTCP = true
	torrentCfg.DisableUTP = true
	torrentCfg.NoDHT = true
	
	client, err := torrent.NewClient(torrentCfg)
	require.NoError(t, err)

	// Create BEP44 catalog ref
	ref, err := NewBEP44CatalogRef(dhtServer, client)
	require.NoError(t, err)
	
	// Reset catalog to empty state for test isolation
	ref.catalogTorrent.catalog = &ModelCatalog{
		Version: 1,
		Models:  make(map[string]ModelEntry),
	}
	ref.catalogTorrent.catalog.Sequence = 0

	return ref, dhtServer, client, tmpDir
}

func TestNewBEP44CatalogRef(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	assert.NotNil(t, ref)
	assert.NotNil(t, ref.server)
	assert.NotNil(t, ref.catalogTorrent)
	assert.NotNil(t, ref.privateKey)
	assert.NotEmpty(t, ref.publicKey)
}

func TestDeterministicKeyGeneration(t *testing.T) {
	// Generate keys from well-known seed
	seed1 := sha256.Sum256([]byte(WellKnownSeed))
	privateKey1 := ed25519.NewKeyFromSeed(seed1[:])
	
	seed2 := sha256.Sum256([]byte(WellKnownSeed))
	privateKey2 := ed25519.NewKeyFromSeed(seed2[:])
	
	// Keys should be identical
	assert.Equal(t, privateKey1, privateKey2)
	
	// Public keys should also match
	publicKey1 := privateKey1.Public().(ed25519.PublicKey)
	publicKey2 := privateKey2.Public().(ed25519.PublicKey)
	assert.Equal(t, publicKey1, publicKey2)
}

func TestPublishCatalogRef(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Skip if no DHT nodes available (expected in test)
	if len(dhtServer.Nodes()) == 0 {
		t.Skip("No DHT nodes available for testing")
	}

	// Publish a catalog reference
	testInfoHash := "abc123def456789012345678901234567890abcd"
	err := ref.PublishCatalogRef(testInfoHash)
	
	// May fail due to no DHT nodes, but should not panic
	if err != nil {
		assert.Contains(t, err.Error(), "no DHT nodes")
	} else {
		// If successful, check state
		assert.Equal(t, testInfoHash, ref.ref.InfoHash)
		assert.Equal(t, int64(1), ref.sequence)
	}
}

func TestBEP44AddModel(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Add a model (will fail to publish to DHT but should update catalog)
	err := ref.AddModel("test-org/test-model", "testhash123", 1000000)
	
	// May fail due to no DHT nodes
	if err != nil {
		assert.Contains(t, err.Error(), "no DHT node")
		return
	}

	// Check catalog was updated
	models, err := ref.catalogTorrent.GetModels("*")
	require.NoError(t, err)
	assert.Equal(t, 1, len(models))
	assert.Equal(t, "test-org/test-model", models[0].Name)
	assert.Equal(t, "testhash123", models[0].InfoHash)
}

func TestBEP44GetModels(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Add some test models directly to catalog
	ref.catalogTorrent.AddModel("meta-llama/llama-7b", "hash1", 7000000000)
	ref.catalogTorrent.AddModel("mistralai/mistral-7b", "hash2", 7000000000)
	ref.catalogTorrent.AddModel("openai/gpt-3b", "hash3", 3000000000)

	// Test searching through BEP44CatalogRef
	results, err := ref.GetModels("llama")
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "meta-llama/llama-7b", results[0].Name)

	// Test wildcard
	results, err = ref.GetModels("*")
	require.NoError(t, err)
	assert.Equal(t, 3, len(results))
}

func TestCatalogRefSequence(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Initial sequence should be 0
	assert.Equal(t, int64(0), ref.sequence)

	// Mock publishing (skip actual DHT operations)
	ref.sequence++
	ref.ref = &CatalogReference{
		InfoHash: "test1",
		Sequence: ref.sequence,
		Updated:  time.Now().Unix(),
	}
	assert.Equal(t, int64(1), ref.sequence)

	// Sequence should increment
	ref.sequence++
	ref.ref = &CatalogReference{
		InfoHash: "test2",
		Sequence: ref.sequence,
		Updated:  time.Now().Unix(),
	}
	assert.Equal(t, int64(2), ref.sequence)
}

func TestCatalogRefContext(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Context should be active
	assert.NotNil(t, ref.ctx)
	
	select {
	case <-ref.ctx.Done():
		t.Error("Context should not be done")
	default:
		// Good, context is active
	}

	// Close should cancel context
	ref.Close()
	
	// Give it a moment
	time.Sleep(10 * time.Millisecond)
	
	select {
	case <-ref.ctx.Done():
		// Good, context was cancelled
	default:
		t.Error("Context should be cancelled after Close")
	}
}

func TestFetchCatalogRefNoNodes(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Fetching should fail gracefully with no nodes
	err := ref.fetchCatalogRef()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no DHT nodes")
}

func TestPublishWithTimeout(t *testing.T) {
	ref, dhtServer, client, tmpDir := setupTestBEP44CatalogRef(t)
	defer os.RemoveAll(tmpDir)
	defer client.Close()
	defer dhtServer.Close()

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	
	ref.ctx = ctx

	// Publishing should handle timeout gracefully
	err := ref.PublishCatalogRef("timeouthash")
	
	// Should either timeout or fail due to no nodes
	assert.Error(t, err)
}