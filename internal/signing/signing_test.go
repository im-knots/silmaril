package signing

import (
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"

	"github.com/silmaril/silmaril/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeyPair(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	assert.NotNil(t, keyPair)
	assert.NotNil(t, keyPair.PrivateKey)
	assert.NotNil(t, keyPair.PublicKey)
	
	// Verify key size
	assert.Equal(t, 2048, keyPair.PrivateKey.Size()*8)
	
	// Verify public key matches private key
	assert.Equal(t, &keyPair.PrivateKey.PublicKey, keyPair.PublicKey)
}

func TestSaveAndLoadKeyPair(t *testing.T) {
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "private.pem")
	publicKeyPath := filepath.Join(tempDir, "public.pem")
	
	// Generate key pair
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	// Save key pair
	err = keyPair.SaveKeyPair(privateKeyPath, publicKeyPath)
	require.NoError(t, err)
	
	// Check files exist
	assert.FileExists(t, privateKeyPath)
	assert.FileExists(t, publicKeyPath)
	
	// Load private key
	loadedPrivateKey, err := LoadPrivateKey(privateKeyPath)
	require.NoError(t, err)
	assert.Equal(t, keyPair.PrivateKey.D, loadedPrivateKey.D)
	assert.Equal(t, keyPair.PrivateKey.N, loadedPrivateKey.N)
	
	// Load public key
	loadedPublicKey, err := LoadPublicKey(publicKeyPath)
	require.NoError(t, err)
	assert.Equal(t, keyPair.PublicKey.N, loadedPublicKey.N)
	assert.Equal(t, keyPair.PublicKey.E, loadedPublicKey.E)
}

func TestLoadPrivateKeyErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() string
		wantErr string
	}{
		{
			name: "file not found",
			setup: func() string {
				return "/nonexistent/private.pem"
			},
			wantErr: "failed to read private key",
		},
		{
			name: "invalid PEM format",
			setup: func() string {
				tempFile := filepath.Join(t.TempDir(), "invalid.pem")
				os.WriteFile(tempFile, []byte("not a pem file"), 0644)
				return tempFile
			},
			wantErr: "failed to parse PEM block",
		},
		{
			name: "invalid key data",
			setup: func() string {
				tempFile := filepath.Join(t.TempDir(), "invalid-key.pem")
				content := "-----BEGIN RSA PRIVATE KEY-----\ninvalid key data\n-----END RSA PRIVATE KEY-----\n"
				os.WriteFile(tempFile, []byte(content), 0644)
				return tempFile
			},
			wantErr: "failed to parse PEM block",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup()
			_, err := LoadPrivateKey(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadPublicKeyErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() string
		wantErr string
	}{
		{
			name: "file not found",
			setup: func() string {
				return "/nonexistent/public.pem"
			},
			wantErr: "failed to read public key",
		},
		{
			name: "invalid PEM format",
			setup: func() string {
				tempFile := filepath.Join(t.TempDir(), "invalid.pem")
				os.WriteFile(tempFile, []byte("not a pem file"), 0644)
				return tempFile
			},
			wantErr: "failed to parse PEM block",
		},
		{
			name: "not an RSA key",
			setup: func() string {
				tempFile := filepath.Join(t.TempDir(), "not-rsa.pem")
				// This is a valid PEM but not an RSA key
				content := "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE\n-----END PUBLIC KEY-----\n"
				os.WriteFile(tempFile, []byte(content), 0644)
				return tempFile
			},
			wantErr: "failed to parse public key",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup()
			_, err := LoadPublicKey(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSignAndVerifyManifest(t *testing.T) {
	// Generate key pair
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	// Create test manifest
	manifest := &models.ModelManifest{
		Name:        "test/model",
		Version:     "1.0",
		Description: "Test model",
		License:     "MIT",
		TotalSize:   1024,
		Files: []models.ModelFile{
			{
				Path:   "model.bin",
				Size:   1024,
				SHA256: "abc123",
			},
		},
	}
	
	// Sign manifest
	err = SignManifest(manifest, keyPair.PrivateKey)
	require.NoError(t, err)
	assert.NotEmpty(t, manifest.Signature)
	
	// Verify signature
	err = VerifyManifest(manifest, keyPair.PublicKey)
	assert.NoError(t, err)
}

func TestVerifyManifestErrors(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	// Different key pair for wrong key test
	wrongKeyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	manifest := &models.ModelManifest{
		Name:    "test/model",
		Version: "1.0",
	}
	
	tests := []struct {
		name    string
		setup   func()
		key     *rsa.PublicKey
		wantErr string
	}{
		{
			name: "unsigned manifest",
			setup: func() {
				manifest.Signature = ""
			},
			key:     keyPair.PublicKey,
			wantErr: "manifest is not signed",
		},
		{
			name: "invalid base64 signature",
			setup: func() {
				manifest.Signature = "not-valid-base64!"
			},
			key:     keyPair.PublicKey,
			wantErr: "failed to decode signature",
		},
		{
			name: "wrong public key",
			setup: func() {
				SignManifest(manifest, keyPair.PrivateKey)
			},
			key:     wrongKeyPair.PublicKey,
			wantErr: "signature verification failed",
		},
		{
			name: "tampered manifest",
			setup: func() {
				SignManifest(manifest, keyPair.PrivateKey)
				manifest.Version = "2.0" // Change after signing
			},
			key:     keyPair.PublicKey,
			wantErr: "signature verification failed",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset manifest
			manifest = &models.ModelManifest{
				Name:    "test/model",
				Version: "1.0",
			}
			
			tt.setup()
			err := VerifyManifest(manifest, tt.key)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGetOrCreateKeys(t *testing.T) {
	// Create temp home directory
	tempHome := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", originalHome)
	
	// First call should create keys
	keyPair1, err := GetOrCreateKeys()
	require.NoError(t, err)
	assert.NotNil(t, keyPair1)
	
	// Check keys were saved
	keysDir := filepath.Join(tempHome, ".silmaril", "keys")
	assert.DirExists(t, keysDir)
	assert.FileExists(t, filepath.Join(keysDir, "private.pem"))
	assert.FileExists(t, filepath.Join(keysDir, "public.pem"))
	
	// Second call should load existing keys
	keyPair2, err := GetOrCreateKeys()
	require.NoError(t, err)
	assert.NotNil(t, keyPair2)
	
	// Verify same keys were loaded
	assert.Equal(t, keyPair1.PrivateKey.N, keyPair2.PrivateKey.N)
	assert.Equal(t, keyPair1.PrivateKey.D, keyPair2.PrivateKey.D)
}

func TestSignatureConsistency(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	
	manifest := &models.ModelManifest{
		Name:        "test/model",
		Version:     "1.0",
		Description: "Test model",
	}
	
	// Sign the same manifest multiple times
	err = SignManifest(manifest, keyPair.PrivateKey)
	require.NoError(t, err)
	sig1 := manifest.Signature
	
	manifest.Signature = ""
	err = SignManifest(manifest, keyPair.PrivateKey)
	require.NoError(t, err)
	sig2 := manifest.Signature
	
	// Signatures might be different (due to randomization in RSA) but could be the same
	// The important thing is both verify correctly
	// assert.NotEqual(t, sig1, sig2) // This assertion is not guaranteed
	
	// But both should verify correctly
	manifest.Signature = sig1
	assert.NoError(t, VerifyManifest(manifest, keyPair.PublicKey))
	
	manifest.Signature = sig2
	assert.NoError(t, VerifyManifest(manifest, keyPair.PublicKey))
}

func BenchmarkGenerateKeyPair(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateKeyPair()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignManifest(b *testing.B) {
	keyPair, _ := GenerateKeyPair()
	manifest := &models.ModelManifest{
		Name:        "test/model",
		Version:     "1.0",
		Description: "Test model with lots of data",
		Files: []models.ModelFile{
			{Path: "file1.bin", Size: 1024, SHA256: "hash1"},
			{Path: "file2.bin", Size: 2048, SHA256: "hash2"},
			{Path: "file3.bin", Size: 4096, SHA256: "hash3"},
		},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manifest.Signature = ""
		err := SignManifest(manifest, keyPair.PrivateKey)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyManifest(b *testing.B) {
	keyPair, _ := GenerateKeyPair()
	manifest := &models.ModelManifest{
		Name:        "test/model",
		Version:     "1.0",
		Description: "Test model",
	}
	SignManifest(manifest, keyPair.PrivateKey)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := VerifyManifest(manifest, keyPair.PublicKey)
		if err != nil {
			b.Fatal(err)
		}
	}
}