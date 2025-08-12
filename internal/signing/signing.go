package signing

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/silmaril/silmaril/internal/models"
)

// KeyPair manages signing keys
type KeyPair struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// GenerateKeyPair creates a new RSA key pair
func GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	
	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}

// SaveKeyPair saves the key pair to files
func (kp *KeyPair) SaveKeyPair(privateKeyPath, publicKeyPath string) error {
	// Save private key
	privateKeyFile, err := os.Create(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to create private key file: %w", err)
	}
	defer privateKeyFile.Close()
	
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(kp.PrivateKey),
	}
	
	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}
	
	// Save public key
	publicKeyFile, err := os.Create(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to create public key file: %w", err)
	}
	defer publicKeyFile.Close()
	
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(kp.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}
	
	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}
	
	if err := pem.Encode(publicKeyFile, publicKeyPEM); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}
	
	return nil
}

// LoadPrivateKey loads a private key from file
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}
	
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}
	
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	
	return key, nil
}

// LoadPublicKey loads a public key from file
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}
	
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}
	
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}
	
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	
	return rsaKey, nil
}

// SignManifest signs a model manifest with a private key
func SignManifest(manifest *models.ModelManifest, privateKey *rsa.PrivateKey) error {
	// Clear any existing signature
	manifest.Signature = ""
	
	// Serialize manifest to JSON
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	
	// Calculate hash
	hash := sha256.Sum256(data)
	
	// Sign the hash
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return fmt.Errorf("failed to sign manifest: %w", err)
	}
	
	// Encode signature as base64
	manifest.Signature = base64.StdEncoding.EncodeToString(signature)
	
	return nil
}

// VerifyManifest verifies a manifest signature with a public key
func VerifyManifest(manifest *models.ModelManifest, publicKey *rsa.PublicKey) error {
	if manifest.Signature == "" {
		return fmt.Errorf("manifest is not signed")
	}
	
	// Decode signature
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}
	
	// Clear signature for verification
	sig := manifest.Signature
	manifest.Signature = ""
	
	// Serialize manifest to JSON
	data, err := json.Marshal(manifest)
	if err != nil {
		manifest.Signature = sig
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	
	// Restore signature
	manifest.Signature = sig
	
	// Calculate hash
	hash := sha256.Sum256(data)
	
	// Verify signature
	err = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	
	return nil
}

// GetOrCreateKeys gets existing keys or creates new ones
func GetOrCreateKeys() (*KeyPair, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	
	keysDir := filepath.Join(home, ".silmaril", "keys")
	os.MkdirAll(keysDir, 0700) // Secure permissions
	
	privateKeyPath := filepath.Join(keysDir, "private.pem")
	publicKeyPath := filepath.Join(keysDir, "public.pem")
	
	// Check if keys exist
	if _, err := os.Stat(privateKeyPath); err == nil {
		privateKey, err := LoadPrivateKey(privateKeyPath)
		if err != nil {
			return nil, err
		}
		return &KeyPair{
			PrivateKey: privateKey,
			PublicKey:  &privateKey.PublicKey,
		}, nil
	}
	
	// Generate new keys
	fmt.Println("Generating new signing keys...")
	keyPair, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	
	err = keyPair.SaveKeyPair(privateKeyPath, publicKeyPath)
	if err != nil {
		return nil, err
	}
	
	fmt.Printf("Keys saved to: %s\n", keysDir)
	return keyPair, nil
}