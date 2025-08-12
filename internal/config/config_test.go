package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDefaultBaseDir(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		expected string
	}{
		{
			name:     "with environment variable",
			envVar:   "/custom/path",
			expected: "/custom/path",
		},
		{
			name:     "without environment variable",
			envVar:   "",
			expected: filepath.Join(os.Getenv("HOME"), ".silmaril"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			originalEnv := os.Getenv("SILMARIL_HOME")
			defer os.Setenv("SILMARIL_HOME", originalEnv)

			os.Setenv("SILMARIL_HOME", tt.envVar)
			result := getDefaultBaseDir()
			
			if tt.envVar != "" {
				assert.Equal(t, tt.expected, result)
			} else {
				assert.Contains(t, result, ".silmaril")
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE") // Windows
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "expand tilde",
			input:    "~/test",
			expected: filepath.Join(home, "test"),
		},
		{
			name:     "expand environment variable",
			input:    "$HOME/test",
			expected: filepath.Join(home, "test"),
		},
		{
			name:     "no expansion needed",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetDefaults(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	// Test storage defaults
	assert.NotEmpty(t, v.Get("storage.base_dir"))
	assert.Empty(t, v.Get("storage.models_dir"))
	assert.Empty(t, v.Get("storage.torrents_dir"))

	// Test network defaults
	assert.True(t, v.GetBool("network.dht_enabled"))
	assert.Equal(t, 100, v.GetInt("network.max_connections"))
	assert.Equal(t, int64(0), v.GetInt64("network.upload_rate_limit"))
	assert.True(t, v.GetBool("network.disable_trackers"))

	// Test torrent defaults
	assert.Equal(t, int64(4*1024*1024), v.GetInt64("torrent.piece_length"))
	assert.Equal(t, 0.0, v.GetFloat64("torrent.seed_ratio"))
	assert.Equal(t, 1800, v.GetInt("torrent.download_timeout"))

	// Test UI defaults
	assert.True(t, v.GetBool("ui.progress_bar"))
	assert.True(t, v.GetBool("ui.color"))
	assert.False(t, v.GetBool("ui.verbose"))
	assert.Equal(t, "text", v.GetString("ui.output_format"))

	// Test security defaults
	assert.True(t, v.GetBool("security.sign_manifests"))
	assert.True(t, v.GetBool("security.verify_manifests"))
}

func TestExpandPaths(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{
			BaseDir: "~/silmaril",
		},
	}

	expandPaths(cfg)

	// Check that base dir was expanded
	assert.NotContains(t, cfg.Storage.BaseDir, "~")
	assert.Contains(t, cfg.Storage.BaseDir, "silmaril")

	// Check that subdirectories were set
	assert.Equal(t, filepath.Join(cfg.Storage.BaseDir, "models"), cfg.Storage.ModelsDir)
	assert.Equal(t, filepath.Join(cfg.Storage.BaseDir, "torrents"), cfg.Storage.TorrentsDir)
	assert.Equal(t, filepath.Join(cfg.Storage.BaseDir, "registry"), cfg.Storage.RegistryDir)
	assert.Equal(t, filepath.Join(cfg.Storage.BaseDir, "db"), cfg.Storage.DBDir)
	assert.Equal(t, filepath.Join(cfg.Storage.BaseDir, "keys"), cfg.Security.KeysDir)
}

func TestCreateAllDirs(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	cfg = &Config{
		Storage: StorageConfig{
			BaseDir:     filepath.Join(tempDir, "base"),
			ModelsDir:   filepath.Join(tempDir, "models"),
			TorrentsDir: filepath.Join(tempDir, "torrents"),
			RegistryDir: filepath.Join(tempDir, "registry"),
			DBDir:       filepath.Join(tempDir, "db"),
		},
		Security: SecurityConfig{
			KeysDir: filepath.Join(tempDir, "keys"),
		},
	}

	err := CreateAllDirs()
	require.NoError(t, err)

	// Check all directories exist
	assert.DirExists(t, cfg.Storage.BaseDir)
	assert.DirExists(t, cfg.Storage.ModelsDir)
	assert.DirExists(t, cfg.Storage.TorrentsDir)
	assert.DirExists(t, cfg.Storage.RegistryDir)
	assert.DirExists(t, cfg.Storage.DBDir)
	assert.DirExists(t, cfg.Security.KeysDir)

	// Check keys directory permissions
	info, err := os.Stat(cfg.Security.KeysDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())
}

func TestInitialize(t *testing.T) {
	// Save original config
	originalCfg := cfg
	originalV := v
	defer func() {
		cfg = originalCfg
		v = originalV
	}()

	// Reset global variables
	cfg = nil
	v = nil

	err := Initialize()
	require.NoError(t, err)

	// Check that config was initialized
	assert.NotNil(t, cfg)
	assert.NotNil(t, v)

	// Check that paths were expanded
	assert.NotEmpty(t, cfg.Storage.BaseDir)
	assert.NotEmpty(t, cfg.Storage.ModelsDir)
}

func TestGet(t *testing.T) {
	// Save original config
	originalCfg := cfg
	defer func() {
		cfg = originalCfg
	}()

	// Test panic when not initialized
	cfg = nil
	assert.Panics(t, func() {
		Get()
	})

	// Test normal operation
	cfg = &Config{}
	result := Get()
	assert.Equal(t, cfg, result)
}

func TestGetViper(t *testing.T) {
	// Save original viper
	originalV := v
	defer func() {
		v = originalV
	}()

	// Test panic when not initialized
	v = nil
	assert.Panics(t, func() {
		GetViper()
	})

	// Test normal operation
	v = viper.New()
	result := GetViper()
	assert.Equal(t, v, result)
}

func TestConfigWithFile(t *testing.T) {
	// Create temp config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")
	
	configContent := `
storage:
  base_dir: /custom/base
network:
  max_connections: 200
  dht_enabled: false
ui:
  color: false
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Save original config
	originalCfg := cfg
	originalV := v
	defer func() {
		cfg = originalCfg
		v = originalV
	}()

	// Initialize with config file
	v = viper.New()
	v.SetConfigFile(configFile)
	
	// Set defaults first
	setDefaults(v)
	
	// Read config
	err = v.ReadInConfig()
	require.NoError(t, err)

	// Check that values were overridden
	assert.Equal(t, "/custom/base", v.GetString("storage.base_dir"))
	assert.Equal(t, 200, v.GetInt("network.max_connections"))
	assert.False(t, v.GetBool("network.dht_enabled"))
	assert.False(t, v.GetBool("ui.color"))

	// Check that defaults are still set for non-overridden values
	assert.True(t, v.GetBool("ui.progress_bar"))
	assert.True(t, v.GetBool("security.sign_manifests"))
}