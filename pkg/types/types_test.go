package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelManifestComputeHash(t *testing.T) {
	manifest := &ModelManifest{
		Name:        "test-model",
		Version:     "1.0.0",
		Description: "Test model",
		License:     "MIT",
		CreatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Architecture: "transformer",
		ModelType:    "llm",
		Parameters:   7000000000,
		TotalSize:    1024 * 1024 * 1024,
		Files: []ModelFile{
			{
				Path:   "model.bin",
				Size:   1024 * 1024 * 1024,
				SHA256: "abc123",
			},
		},
		Signature: "somesignature",
	}

	// Compute hash
	hash1, err := manifest.ComputeHash()
	require.NoError(t, err)
	assert.NotEmpty(t, hash1)

	// Hash should be deterministic
	hash2, err := manifest.ComputeHash()
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// Hash should ignore signature field
	manifest.Signature = "differentsignature"
	hash3, err := manifest.ComputeHash()
	require.NoError(t, err)
	assert.Equal(t, hash1, hash3)

	// Hash should change when other fields change
	manifest.Name = "different-model"
	hash4, err := manifest.ComputeHash()
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash4)
}

func TestModelManifestJSON(t *testing.T) {
	manifest := &ModelManifest{
		Name:         "test-model",
		Version:      "1.0.0",
		Description:  "Test model",
		License:      "Apache-2.0",
		CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Architecture: "transformer",
		ModelType:    "llm",
		Parameters:   13000000000,
		Quantization: "fp16",
		InferenceHints: InferenceHints{
			MinRAM:        16,
			MinVRAM:       8,
			RecommendedGPU: []string{"RTX 3090", "RTX 4090"},
			ContextLength:  4096,
			TokenizerType:  "sentencepiece",
		},
		TotalSize: 26 * 1024 * 1024 * 1024,
		Files: []ModelFile{
			{
				Path:   "model-00001-of-00002.safetensors",
				Size:   13 * 1024 * 1024 * 1024,
				SHA256: "hash1",
			},
			{
				Path:   "model-00002-of-00002.safetensors",
				Size:   13 * 1024 * 1024 * 1024,
				SHA256: "hash2",
			},
		},
		MagnetURI: "magnet:?xt=urn:btih:abc123",
		IPFSCIDs: map[string]string{
			"model-00001-of-00002.safetensors": "QmXxx",
			"model-00002-of-00002.safetensors": "QmYyy",
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Unmarshal back
	var decoded ModelManifest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Check key fields
	assert.Equal(t, manifest.Name, decoded.Name)
	assert.Equal(t, manifest.Version, decoded.Version)
	assert.Equal(t, manifest.Parameters, decoded.Parameters)
	assert.Equal(t, manifest.TotalSize, decoded.TotalSize)
	assert.Equal(t, len(manifest.Files), len(decoded.Files))
	assert.Equal(t, manifest.InferenceHints.MinRAM, decoded.InferenceHints.MinRAM)
	assert.Equal(t, len(manifest.InferenceHints.RecommendedGPU), len(decoded.InferenceHints.RecommendedGPU))
}

func TestModelAnnouncement(t *testing.T) {
	announcement := &ModelAnnouncement{
		Name:     "test-org/test-model",
		Version:  "v1.0",
		Magnet:   "magnet:?xt=urn:btih:abc123",
		InfoHash: "abc123",
		Size:     1024 * 1024 * 1024,
		Time:     time.Now().Unix(),
	}

	// Test JSON marshaling
	data, err := json.Marshal(announcement)
	require.NoError(t, err)

	var decoded ModelAnnouncement
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, announcement.Name, decoded.Name)
	assert.Equal(t, announcement.InfoHash, decoded.InfoHash)
	assert.Equal(t, announcement.Size, decoded.Size)
}

func TestProgressUpdate(t *testing.T) {
	progress := ProgressUpdate{
		BytesCompleted: 512 * 1024 * 1024,  // Exactly half of 1GB
		BytesTotal:     1024 * 1024 * 1024,
		DownloadRate:   10 * 1024 * 1024,
		UploadRate:     2 * 1024 * 1024,
		NumPeers:       15,
		NumSeeders:     5,
	}

	// Calculate percentage
	percentage := float64(progress.BytesCompleted) / float64(progress.BytesTotal) * 100
	assert.Equal(t, float64(50), percentage)

	// Check rates are positive
	assert.Greater(t, progress.DownloadRate, float64(0))
	assert.Greater(t, progress.UploadRate, float64(0))
}

func TestTorrentConfig(t *testing.T) {
	config := TorrentConfig{
		DataDir:           "/tmp/torrents",
		SeedRatio:         2.0,
		DownloadTimeout:   30 * time.Minute,
		MaxConnections:    50,
		UploadRateLimit:   1024 * 1024 * 10, // 10 MB/s
		DownloadRateLimit: 0,                 // unlimited
	}

	assert.NotEmpty(t, config.DataDir)
	assert.Greater(t, config.SeedRatio, float64(0))
	assert.Greater(t, config.DownloadTimeout, time.Duration(0))
	assert.Greater(t, config.MaxConnections, 0)
}

func TestDiskUsage(t *testing.T) {
	usage := DiskUsage{
		Total:    100 * 1024 * 1024 * 1024, // 100 GB
		Models:   80 * 1024 * 1024 * 1024,  // 80 GB
		Torrents: 10 * 1024 * 1024 * 1024,  // 10 GB
		Registry: 5 * 1024 * 1024 * 1024,   // 5 GB
		Database: 5 * 1024 * 1024 * 1024,   // 5 GB
	}

	// Check total equals sum of parts
	sum := usage.Models + usage.Torrents + usage.Registry + usage.Database
	assert.Equal(t, usage.Total, sum)

	// Calculate percentage used by models
	modelsPercentage := float64(usage.Models) / float64(usage.Total) * 100
	assert.Equal(t, float64(80), modelsPercentage)
}

func TestHFConfig(t *testing.T) {
	config := HFConfig{
		ModelType:             "llama",
		Architectures:         []string{"LlamaForCausalLM"},
		NumParameters:         7000000000,
		HiddenSize:            4096,
		NumHiddenLayers:       32,
		NumAttentionHeads:     32,
		MaxPositionEmbeddings: 4096,
		Quantization: map[string]interface{}{
			"bits":         4,
			"group_size":   128,
			"quant_method": "awq",
		},
	}

	// Test JSON marshaling
	data, err := json.Marshal(config)
	require.NoError(t, err)

	var decoded HFConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, config.ModelType, decoded.ModelType)
	assert.Equal(t, config.NumParameters, decoded.NumParameters)
	assert.Equal(t, len(config.Architectures), len(decoded.Architectures))
	assert.Equal(t, config.Architectures[0], decoded.Architectures[0])
	assert.NotNil(t, decoded.Quantization)
}

func TestInferenceHints(t *testing.T) {
	hints := InferenceHints{
		MinRAM:         32,
		MinVRAM:        16,
		RecommendedGPU: []string{"RTX 3090", "RTX 4090", "A100"},
		ContextLength:  8192,
		TokenizerType:  "tiktoken",
	}

	// Test JSON marshaling
	data, err := json.Marshal(hints)
	require.NoError(t, err)

	var decoded InferenceHints
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, hints.MinRAM, decoded.MinRAM)
	assert.Equal(t, hints.MinVRAM, decoded.MinVRAM)
	assert.Equal(t, hints.ContextLength, decoded.ContextLength)
	assert.Equal(t, len(hints.RecommendedGPU), len(decoded.RecommendedGPU))
}

func TestModelFileValidation(t *testing.T) {
	file := ModelFile{
		Path:   "model.safetensors",
		Size:   1024 * 1024 * 1024,
		SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	// Check SHA256 format (should be 64 hex characters)
	assert.Equal(t, 64, len(file.SHA256))
	
	// Check size is positive
	assert.Greater(t, file.Size, int64(0))
	
	// Check path is not empty
	assert.NotEmpty(t, file.Path)
}