package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPaths(t *testing.T) {
	// Save original env
	originalHome := os.Getenv("SILMARIL_HOME")
	defer os.Setenv("SILMARIL_HOME", originalHome)
	
	tests := []struct {
		name        string
		envVar      string
		expectError bool
	}{
		{
			name:        "with SILMARIL_HOME",
			envVar:      "/custom/silmaril",
			expectError: false,
		},
		{
			name:        "without SILMARIL_HOME",
			envVar:      "",
			expectError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("SILMARIL_HOME", tt.envVar)
			
			paths, err := NewPaths()
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.NotNil(t, paths)
			
			if tt.envVar != "" {
				assert.Equal(t, tt.envVar, paths.baseDir)
			} else {
				assert.Contains(t, paths.baseDir, ".silmaril")
			}
			
			// Check all paths are set
			assert.Equal(t, filepath.Join(paths.baseDir, "models"), paths.modelsDir)
			assert.Equal(t, filepath.Join(paths.baseDir, "torrents"), paths.torrentsDir)
			assert.Equal(t, filepath.Join(paths.baseDir, "registry"), paths.registryDir)
			assert.Equal(t, filepath.Join(paths.baseDir, "db"), paths.dbDir)
			assert.NotEmpty(t, paths.configDir)
		})
	}
}

func TestGetBaseDir(t *testing.T) {
	// Save original env
	originalHome := os.Getenv("SILMARIL_HOME")
	defer os.Setenv("SILMARIL_HOME", originalHome)
	
	tests := []struct {
		name     string
		envVar   string
		expected string
	}{
		{
			name:     "with SILMARIL_HOME",
			envVar:   "/opt/silmaril",
			expected: "/opt/silmaril",
		},
		{
			name:   "without SILMARIL_HOME",
			envVar: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("SILMARIL_HOME", tt.envVar)
			
			result, err := getBaseDir()
			require.NoError(t, err)
			
			if tt.envVar != "" {
				assert.Equal(t, tt.expected, result)
			} else {
				assert.Contains(t, result, ".silmaril")
			}
		})
	}
}

func TestGetConfigDir(t *testing.T) {
	// Save original envs
	originalConfig := os.Getenv("SILMARIL_CONFIG")
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	defer func() {
		os.Setenv("SILMARIL_CONFIG", originalConfig)
		os.Setenv("XDG_CONFIG_HOME", originalXDG)
	}()
	
	tests := []struct {
		name           string
		silmarilConfig string
		xdgConfig      string
		goos           string
		expected       func() string
	}{
		{
			name:           "with SILMARIL_CONFIG",
			silmarilConfig: "/custom/config",
			expected:       func() string { return "/custom/config" },
		},
		{
			name:      "with XDG_CONFIG_HOME",
			xdgConfig: "/home/user/.config",
			expected:  func() string { return "/home/user/.config/silmaril" },
		},
		{
			name: "darwin default",
			goos: "darwin",
			expected: func() string {
				home, _ := os.UserHomeDir()
				return filepath.Join(home, "Library", "Application Support", "silmaril")
			},
		},
		{
			name: "windows default",
			goos: "windows",
			expected: func() string {
				if appData := os.Getenv("APPDATA"); appData != "" {
					return filepath.Join(appData, "silmaril")
				}
				home, _ := os.UserHomeDir()
				return filepath.Join(home, "AppData", "Roaming", "silmaril")
			},
		},
		{
			name: "linux default",
			goos: "linux",
			expected: func() string {
				home, _ := os.UserHomeDir()
				return filepath.Join(home, ".config", "silmaril")
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("SILMARIL_CONFIG", tt.silmarilConfig)
			os.Setenv("XDG_CONFIG_HOME", tt.xdgConfig)
			
			// Skip OS-specific tests if not on that OS
			if tt.goos != "" && tt.goos != runtime.GOOS {
				t.Skip("Skipping OS-specific test")
			}
			
			result, err := getConfigDir()
			require.NoError(t, err)
			
			if tt.expected != nil {
				assert.Equal(t, tt.expected(), result)
			}
		})
	}
}

func TestPathsInitialize(t *testing.T) {
	tempDir := t.TempDir()
	
	paths := &Paths{
		baseDir:     filepath.Join(tempDir, "base"),
		modelsDir:   filepath.Join(tempDir, "models"),
		torrentsDir: filepath.Join(tempDir, "torrents"),
		registryDir: filepath.Join(tempDir, "registry"),
		configDir:   filepath.Join(tempDir, "config"),
		dbDir:       filepath.Join(tempDir, "db"),
	}
	
	err := paths.Initialize()
	require.NoError(t, err)
	
	// Check all directories exist
	assert.DirExists(t, paths.modelsDir)
	assert.DirExists(t, paths.torrentsDir)
	assert.DirExists(t, paths.registryDir)
	assert.DirExists(t, paths.configDir)
	assert.DirExists(t, paths.dbDir)
}

func TestPathGetters(t *testing.T) {
	paths := &Paths{
		baseDir:     "/base",
		modelsDir:   "/base/models",
		torrentsDir: "/base/torrents",
		registryDir: "/base/registry",
		configDir:   "/base/config",
		dbDir:       "/base/db",
	}
	
	assert.Equal(t, "/base", paths.BaseDir())
	assert.Equal(t, "/base/models", paths.ModelsDir())
	assert.Equal(t, "/base/torrents", paths.TorrentsDir())
	assert.Equal(t, "/base/registry", paths.RegistryDir())
	assert.Equal(t, "/base/config", paths.ConfigDir())
	assert.Equal(t, "/base/db", paths.DBDir())
}

func TestModelPath(t *testing.T) {
	paths := &Paths{
		modelsDir: "/base/models",
	}
	
	tests := []struct {
		modelName string
		expected  string
	}{
		{
			modelName: "meta-llama/Llama-3.1-8B",
			expected:  "/base/models/meta-llama/Llama-3.1-8B",
		},
		{
			modelName: "simple-model",
			expected:  "/base/models/simple-model",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			result := paths.ModelPath(tt.modelName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTorrentPath(t *testing.T) {
	paths := &Paths{
		torrentsDir: "/base/torrents",
	}
	
	tests := []struct {
		modelName string
		expected  string
	}{
		{
			modelName: "meta-llama/Llama-3.1-8B",
			expected:  "/base/torrents/meta-llama/Llama-3.1-8B.torrent",
		},
		{
			modelName: "simple-model",
			expected:  "/base/torrents/simple-model.torrent",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			result := paths.TorrentPath(tt.modelName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigPath(t *testing.T) {
	paths := &Paths{
		configDir: "/base/config",
	}
	
	result := paths.ConfigPath()
	assert.Equal(t, "/base/config/config.yaml", result)
}

func TestGetDirSize(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create some test files
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")
	subDir := filepath.Join(tempDir, "subdir")
	file3 := filepath.Join(subDir, "file3.txt")
	
	os.Mkdir(subDir, 0755)
	os.WriteFile(file1, []byte("hello"), 0644)     // 5 bytes
	os.WriteFile(file2, []byte("world!"), 0644)    // 6 bytes
	os.WriteFile(file3, []byte("testing"), 0644)   // 7 bytes
	
	size := getDirSize(tempDir)
	assert.Equal(t, int64(18), size) // 5 + 6 + 7
}

func TestGetDiskUsage(t *testing.T) {
	tempDir := t.TempDir()
	
	paths := &Paths{
		modelsDir:   filepath.Join(tempDir, "models"),
		torrentsDir: filepath.Join(tempDir, "torrents"),
		registryDir: filepath.Join(tempDir, "registry"),
		dbDir:       filepath.Join(tempDir, "db"),
	}
	
	// Create directories
	os.MkdirAll(paths.modelsDir, 0755)
	os.MkdirAll(paths.torrentsDir, 0755)
	os.MkdirAll(paths.registryDir, 0755)
	os.MkdirAll(paths.dbDir, 0755)
	
	// Create some test files
	os.WriteFile(filepath.Join(paths.modelsDir, "model.bin"), make([]byte, 1000), 0644)
	os.WriteFile(filepath.Join(paths.torrentsDir, "model.torrent"), make([]byte, 100), 0644)
	os.WriteFile(filepath.Join(paths.registryDir, "manifest.json"), make([]byte, 200), 0644)
	os.WriteFile(filepath.Join(paths.dbDir, "data.db"), make([]byte, 300), 0644)
	
	usage, err := paths.GetDiskUsage()
	require.NoError(t, err)
	
	assert.Equal(t, int64(1000), usage.Models)
	assert.Equal(t, int64(100), usage.Torrents)
	assert.Equal(t, int64(200), usage.Registry)
	assert.Equal(t, int64(300), usage.Database)
	assert.Equal(t, int64(1600), usage.Total)
}

func TestCleanupOldFiles(t *testing.T) {
	paths := &Paths{}
	
	// For now this is a no-op, but test it doesn't error
	err := paths.CleanupOldFiles(7)
	assert.NoError(t, err)
}

func BenchmarkGetDirSize(b *testing.B) {
	tempDir := b.TempDir()
	
	// Create many files
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(filename, make([]byte, 1024), 0644)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getDirSize(tempDir)
	}
}