package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ModelManifest represents a complete model manifest
type ModelManifest struct {
	// Core model identification
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	License     string    `json:"license"`
	CreatedAt   time.Time `json:"created_at"`
	
	// Model architecture and parameters
	Architecture   string                 `json:"architecture"`
	ModelType      string                 `json:"model_type"` // llm, diffusion, etc
	Parameters     int64                  `json:"parameters"` // number of parameters
	Quantization   string                 `json:"quantization,omitempty"` // fp16, int8, etc
	
	// Inference hints
	InferenceHints InferenceHints        `json:"inference_hints"`
	
	// Distribution info
	TotalSize      int64                 `json:"total_size"`
	Size           int64                 `json:"size,omitempty"` // Alternative to TotalSize for discovered models
	Files          []ModelFile           `json:"files"`
	MagnetURI      string                `json:"magnet_uri"` // BitTorrent v2 only
	IPFSCIDs       map[string]string     `json:"ipfs_cids,omitempty"` // filename -> CID
	
	// Signature for verification
	Signature      string                `json:"signature,omitempty"`
}

// InferenceHints provides hints for running inference
type InferenceHints struct {
	MinRAM          int64    `json:"min_ram_gb"`
	MinVRAM         int64    `json:"min_vram_gb,omitempty"`
	RecommendedGPU  []string `json:"recommended_gpu,omitempty"`
	ContextLength   int      `json:"context_length,omitempty"`
	TokenizerType   string   `json:"tokenizer_type,omitempty"`
}

// ModelFile represents a single file in a model
type ModelFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// ComputeHash returns the SHA256 hash of the manifest (excluding signature)
func (m *ModelManifest) ComputeHash() (string, error) {
	// Create a copy without the signature
	manifestCopy := *m
	manifestCopy.Signature = ""
	
	data, err := json.Marshal(manifestCopy)
	if err != nil {
		return "", err
	}
	
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// ModelAnnouncement represents a model announcement in DHT
type ModelAnnouncement struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Magnet  string `json:"magnet"`
	Size    int64  `json:"size"`
	Time    int64  `json:"time"`
}

// ProgressUpdate represents download/upload progress
type ProgressUpdate struct {
	BytesCompleted int64
	BytesTotal     int64
	DownloadRate   float64
	UploadRate     float64
	NumPeers       int
	NumSeeders     int
}

// Download represents an active torrent download
type Download struct {
	Torrent  interface{} // *torrent.Torrent
	Progress chan ProgressUpdate
	Done     chan error
}

// TorrentConfig configures the torrent client
type TorrentConfig struct {
	DataDir           string
	SeedRatio         float64
	DownloadTimeout   time.Duration
	MaxConnections    int
	UploadRateLimit   int64
	DownloadRateLimit int64
}

// DiskUsage represents disk space usage
type DiskUsage struct {
	Total     int64
	Models    int64
	Torrents  int64
	Registry  int64
	Database  int64
}

// HFConfig represents HuggingFace model config.json
type HFConfig struct {
	ModelType             string                 `json:"model_type"`
	Architectures         []string              `json:"architectures"`
	NumParameters         int64                 `json:"num_parameters"`
	HiddenSize            int                   `json:"hidden_size"`
	NumHiddenLayers       int                   `json:"num_hidden_layers"`
	NumAttentionHeads     int                   `json:"num_attention_heads"`
	MaxPositionEmbeddings int                   `json:"max_position_embeddings"`
	Quantization          map[string]interface{} `json:"quantization_config"`
}