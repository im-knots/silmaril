package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/silmaril/silmaril/pkg/types"
)

// BEP44Manager handles BEP 44 mutable item operations for model discovery
type BEP44Manager struct {
	server     *dht.Server
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	pubKey32   [32]byte
	
	mu         sync.RWMutex
	indexes    map[string]*ModelIndex
	
	ctx        context.Context
	cancel     context.CancelFunc
}

// ModelIndex represents a collection of models under a specific key
type ModelIndex struct {
	Version   int                    `json:"version"`
	Type      string                 `json:"type"`
	Models    map[string]*ModelMeta  `json:"models"`
	Updated   time.Time              `json:"updated"`
	Publisher string                 `json:"publisher"`
}

// ModelMeta represents metadata for a model in the index
type ModelMeta struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	InfoHash    string    `json:"info_hash"`
	MagnetURI   string    `json:"magnet"`
	Size        int64     `json:"size"`
	Tags        []string  `json:"tags"`
	Description string    `json:"description"`
	Added       time.Time `json:"added"`
}

// NewBEP44Manager creates a new BEP 44 manager
func NewBEP44Manager(server *dht.Server) (*BEP44Manager, error) {
	// Generate identity
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate identity: %w", err)
	}
	
	// Convert to 32-byte array
	var pubKey32 [32]byte
	copy(pubKey32[:], pub)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &BEP44Manager{
		server:     server,
		privateKey: priv,
		publicKey:  pub,
		pubKey32:   pubKey32,
		indexes:    make(map[string]*ModelIndex),
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Close shuts down the manager
func (m *BEP44Manager) Close() {
	m.cancel()
}

// PublishModel publishes a model to the DHT index
func (m *BEP44Manager) PublishModel(manifest *types.ModelManifest, infoHash string) error {
	meta := &ModelMeta{
		Name:        manifest.Name,
		Version:     manifest.Version,
		InfoHash:    infoHash,
		MagnetURI:   manifest.MagnetURI,
		Size:        manifest.TotalSize,
		Tags:        extractModelTags(manifest),
		Description: manifest.Description,
		Added:       time.Now(),
	}
	
	// Publish to category indexes
	for _, tag := range meta.Tags {
		if err := m.publishToIndex(tag, meta); err != nil {
			return fmt.Errorf("failed to publish to %s index: %w", tag, err)
		}
	}
	
	// Publish to main index
	return m.publishToIndex("all", meta)
}

// DiscoverModels searches for models by pattern
func (m *BEP44Manager) DiscoverModels(ctx context.Context, pattern string) ([]*ModelMeta, error) {
	// Determine which index to fetch
	indexKey := "all"
	if pattern != "" && pattern != "*" {
		// Use pattern as tag/category
		indexKey = strings.ToLower(pattern)
	}
	
	// Fetch the index
	index, err := m.fetchIndex(ctx, indexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch index: %w", err)
	}
	
	// Convert to slice
	var results []*ModelMeta
	for _, model := range index.Models {
		if pattern == "" || pattern == "*" || matchesModelPattern(model, pattern) {
			results = append(results, model)
		}
	}
	
	return results, nil
}

// publishToIndex publishes a model to a specific index
func (m *BEP44Manager) publishToIndex(indexKey string, meta *ModelMeta) error {
	// Fetch or create index
	index, _ := m.fetchIndex(context.Background(), indexKey)
	if index == nil {
		index = &ModelIndex{
			Version:   1,
			Type:      indexKey,
			Models:    make(map[string]*ModelMeta),
			Publisher: hex.EncodeToString(m.publicKey),
		}
	}
	
	// Add model
	modelKey := fmt.Sprintf("%s:%s", meta.Name, meta.Version)
	index.Models[modelKey] = meta
	index.Updated = time.Now()
	
	// Serialize
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}
	
	// Create salt for this index
	salt := []byte("silmaril:" + indexKey)
	
	// Get current sequence number
	target := bep44.MakeMutableTarget(m.pubKey32, salt)
	var seq int64
	
	// Try to get current item to find sequence
	result := m.server.Get(context.Background(), nil, target, nil, dht.QueryRateLimiting{})
	if result.Err == nil && result.Reply.R != nil && result.Reply.R.Seq != nil {
		seq = *result.Reply.R.Seq + 1
	}
	
	// Create Put item
	put := bep44.Put{
		V:    data,
		K:    &m.pubKey32,
		Salt: salt,
		Seq:  seq,
	}
	
	// Sign it
	put.Sign(m.privateKey)
	
	// Store in DHT
	// Note: We need to get a token first by doing a Get query
	getResult := m.server.Get(context.Background(), nil, target, nil, dht.QueryRateLimiting{})
	if getResult.Err == nil && getResult.Reply.R != nil && getResult.Reply.R.Token != nil {
		// Now we can Put with the token
		// Note: This is simplified - in production we'd need to iterate through nodes
		// For now, we'll just return an error indicating this needs full implementation
		_ = put
		return fmt.Errorf("BEP44 Put not fully implemented - needs node iteration")
	}
	
	// Cache locally
	m.mu.Lock()
	m.indexes[indexKey] = index
	m.mu.Unlock()
	
	return nil
}

// fetchIndex retrieves an index from the DHT
func (m *BEP44Manager) fetchIndex(ctx context.Context, indexKey string) (*ModelIndex, error) {
	// Check cache
	m.mu.RLock()
	if cached, ok := m.indexes[indexKey]; ok {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()
	
	// Create salt and target
	salt := []byte("silmaril:" + indexKey)
	target := bep44.MakeMutableTarget(m.pubKey32, salt)
	
	// Get from DHT
	result := m.server.Get(ctx, nil, target, nil, dht.QueryRateLimiting{})
	if result.Err != nil {
		return nil, fmt.Errorf("failed to get from DHT: %w", result.Err)
	}
	
	if result.Reply.R == nil || result.Reply.R.V == nil {
		return nil, fmt.Errorf("no data found for index: %s", indexKey)
	}
	
	// Parse the data - R.V is bencode.Bytes
	data := []byte(result.Reply.R.V)
	
	var index ModelIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}
	
	// Cache
	m.mu.Lock()
	m.indexes[indexKey] = &index
	m.mu.Unlock()
	
	return &index, nil
}

// GetPublisherID returns the publisher's public key as hex
func (m *BEP44Manager) GetPublisherID() string {
	return hex.EncodeToString(m.publicKey)
}

// extractModelTags extracts tags from a model manifest
func extractModelTags(manifest *types.ModelManifest) []string {
	tags := []string{}
	
	// Add model type
	if manifest.ModelType != "" {
		tags = append(tags, strings.ToLower(manifest.ModelType))
	}
	
	// Add architecture
	if manifest.Architecture != "" {
		tags = append(tags, strings.ToLower(manifest.Architecture))
	}
	
	// Extract from name
	name := strings.ToLower(manifest.Name)
	if strings.Contains(name, "llama") {
		tags = append(tags, "llama")
	}
	if strings.Contains(name, "mistral") {
		tags = append(tags, "mistral")
	}
	if strings.Contains(name, "gpt") {
		tags = append(tags, "gpt")
	}
	if strings.Contains(name, "bert") {
		tags = append(tags, "bert")
	}
	
	return tags
}

// matchesModelPattern checks if a model matches a search pattern
func matchesModelPattern(model *ModelMeta, pattern string) bool {
	pattern = strings.ToLower(pattern)
	
	// Check name
	if strings.Contains(strings.ToLower(model.Name), pattern) {
		return true
	}
	
	// Check tags
	for _, tag := range model.Tags {
		if strings.Contains(strings.ToLower(tag), pattern) {
			return true
		}
	}
	
	// Check description
	if strings.Contains(strings.ToLower(model.Description), pattern) {
		return true
	}
	
	return false
}