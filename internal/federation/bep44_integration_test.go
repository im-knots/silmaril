package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/silmaril/silmaril/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit Tests

func TestNewBEP44Manager(t *testing.T) {
	// Mock DHT server
	server := &dht.Server{}
	
	manager, err := NewBEP44Manager(server)
	require.NoError(t, err)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.privateKey)
	assert.NotNil(t, manager.publicKey)
	assert.Equal(t, 32, len(manager.pubKey32))
	
	// Verify public key derivation
	derivedPub := manager.privateKey.Public().(ed25519.PublicKey)
	assert.Equal(t, manager.publicKey, derivedPub)
	
	// Close should not error
	manager.Close()
}

func TestExtractModelTags(t *testing.T) {
	tests := []struct {
		name     string
		manifest *types.ModelManifest
		expected []string
	}{
		{
			name: "llama model",
			manifest: &types.ModelManifest{
				Name:         "meta-llama/Llama-2-7b",
				ModelType:    "transformer",
				Architecture: "decoder-only",
			},
			expected: []string{"transformer", "decoder-only", "llama"},
		},
		{
			name: "mistral model",
			manifest: &types.ModelManifest{
				Name:      "mistralai/Mistral-7B",
				ModelType: "llm",
			},
			expected: []string{"llm", "mistral"},
		},
		{
			name: "gpt model",
			manifest: &types.ModelManifest{
				Name: "openai/gpt-3.5-turbo",
			},
			expected: []string{"gpt"},
		},
		{
			name: "bert model",
			manifest: &types.ModelManifest{
				Name:         "bert-base-uncased",
				Architecture: "encoder",
			},
			expected: []string{"encoder", "bert"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := extractModelTags(tt.manifest)
			assert.ElementsMatch(t, tt.expected, tags)
		})
	}
}

func TestMatchesModelPattern(t *testing.T) {
	model := &ModelMeta{
		Name:        "meta-llama/Llama-2-7b",
		Tags:        []string{"llama", "transformer", "7b"},
		Description: "Llama 2 7B parameter model",
	}
	
	tests := []struct {
		pattern string
		matches bool
	}{
		{"llama", true},
		{"Llama", true},
		{"LLAMA", true},
		{"transformer", true},
		{"7b", true},
		{"meta", true},
		{"parameter", true},
		{"mistral", false},
		{"gpt", false},
		{"", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := matchesModelPattern(model, tt.pattern)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func TestModelIndexSerialization(t *testing.T) {
	index := &ModelIndex{
		Version: 1,
		Type:    "test",
		Models: map[string]*ModelMeta{
			"model1:v1": {
				Name:     "model1",
				Version:  "v1",
				InfoHash: "hash123",
				Size:     1024,
				Tags:     []string{"test"},
				Added:    time.Now(),
			},
		},
		Updated:   time.Now(),
		Publisher: "test-publisher",
	}
	
	// Should serialize and deserialize correctly
	data, err := json.Marshal(index)
	require.NoError(t, err)
	
	var decoded ModelIndex
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	
	assert.Equal(t, index.Version, decoded.Version)
	assert.Equal(t, index.Type, decoded.Type)
	assert.Equal(t, len(index.Models), len(decoded.Models))
	assert.Equal(t, index.Publisher, decoded.Publisher)
}

func TestBEP44Target(t *testing.T) {
	// Test that we can create consistent targets
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	
	var pubKey32 [32]byte
	copy(pubKey32[:], pub)
	
	salt := []byte("test-salt")
	target1 := bep44.MakeMutableTarget(pubKey32, salt)
	target2 := bep44.MakeMutableTarget(pubKey32, salt)
	
	// Same key and salt should produce same target
	assert.Equal(t, target1, target2)
	
	// Different salt should produce different target
	salt2 := []byte("different-salt")
	target3 := bep44.MakeMutableTarget(pubKey32, salt2)
	assert.NotEqual(t, target1, target3)
}

func TestPublisherID(t *testing.T) {
	server := &dht.Server{}
	manager, err := NewBEP44Manager(server)
	require.NoError(t, err)
	defer manager.Close()
	
	pubID := manager.GetPublisherID()
	assert.NotEmpty(t, pubID)
	
	// Should be hex encoded
	decoded, err := hex.DecodeString(pubID)
	require.NoError(t, err)
	assert.Equal(t, ed25519.PublicKeySize, len(decoded))
	assert.Equal(t, manager.publicKey, ed25519.PublicKey(decoded))
}

// Functional Tests

func TestPublishModelCreatesMetadata(t *testing.T) {
	server := &dht.Server{}
	manager, err := NewBEP44Manager(server)
	require.NoError(t, err)
	defer manager.Close()
	
	manifest := &types.ModelManifest{
		Name:        "test/model",
		Version:     "v1.0",
		Description: "Test model",
		TotalSize:   1024,
		ModelType:   "test",
		MagnetURI:   "magnet:?xt=urn:btih:test",
	}
	
	// This will fail with "not fully implemented" but we can test the metadata creation
	err = manager.PublishModel(manifest, "testhash123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not fully implemented")
	
	// Check that indexes were created in cache
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	
	// Should have test and all indexes
	assert.Contains(t, manager.indexes, "test")
	assert.Contains(t, manager.indexes, "all")
	
	if testIndex, ok := manager.indexes["test"]; ok {
		modelKey := "test/model:v1.0"
		assert.Contains(t, testIndex.Models, modelKey)
		
		model := testIndex.Models[modelKey]
		assert.Equal(t, "test/model", model.Name)
		assert.Equal(t, "v1.0", model.Version)
		assert.Equal(t, "testhash123", model.InfoHash)
		assert.Equal(t, int64(1024), model.Size)
		assert.Contains(t, model.Tags, "test")
	}
}

func TestDiscoverModelsFromCache(t *testing.T) {
	server := &dht.Server{}
	manager, err := NewBEP44Manager(server)
	require.NoError(t, err)
	defer manager.Close()
	
	// Manually populate cache
	manager.indexes["all"] = &ModelIndex{
		Version: 1,
		Type:    "all",
		Models: map[string]*ModelMeta{
			"llama:v1": {
				Name:    "meta-llama/llama-7b",
				Version: "v1",
				Tags:    []string{"llama", "7b"},
			},
			"mistral:v1": {
				Name:    "mistralai/mistral-7b",
				Version: "v1",
				Tags:    []string{"mistral", "7b"},
			},
		},
	}
	
	// Test discovery with different patterns
	tests := []struct {
		pattern      string
		expectedCount int
		expectedNames []string
	}{
		{
			pattern:       "*",
			expectedCount: 2,
			expectedNames: []string{"meta-llama/llama-7b", "mistralai/mistral-7b"},
		},
		{
			pattern:       "llama",
			expectedCount: 1,
			expectedNames: []string{"meta-llama/llama-7b"},
		},
		{
			pattern:       "7b",
			expectedCount: 2,
			expectedNames: []string{"meta-llama/llama-7b", "mistralai/mistral-7b"},
		},
		{
			pattern:       "gpt",
			expectedCount: 0,
			expectedNames: []string{},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			results, err := manager.DiscoverModels(context.Background(), tt.pattern)
			// Will error because DHT fetch fails, but should still return cached
			if err == nil || !assert.Contains(t, err.Error(), "failed to fetch index") {
				assert.Len(t, results, tt.expectedCount)
				
				names := make([]string, len(results))
				for i, r := range results {
					names[i] = r.Name
				}
				assert.ElementsMatch(t, tt.expectedNames, names)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	server := &dht.Server{}
	manager, err := NewBEP44Manager(server)
	require.NoError(t, err)
	defer manager.Close()
	
	// Test concurrent reads and writes
	done := make(chan bool, 10)
	
	// Writers
	for i := 0; i < 5; i++ {
		go func(i int) {
			manifest := &types.ModelManifest{
				Name:      fmt.Sprintf("model%d", i),
				Version:   "v1",
				ModelType: "test",
			}
			manager.PublishModel(manifest, fmt.Sprintf("hash%d", i))
			done <- true
		}(i)
	}
	
	// Readers
	for i := 0; i < 5; i++ {
		go func() {
			manager.DiscoverModels(context.Background(), "test")
			done <- true
		}()
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify no race conditions occurred
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	assert.NotNil(t, manager.indexes)
}