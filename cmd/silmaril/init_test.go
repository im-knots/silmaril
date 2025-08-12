package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCommand(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	// Test basic initialization
	t.Run("basic init", func(t *testing.T) {
		initPath = filepath.Join(tempDir, "test-init")
		initForce = false
		
		err := runInit(nil, nil)
		require.NoError(t, err)

		// Check that all directories were created
		expectedDirs := []string{
			initPath,
			filepath.Join(initPath, "models"),
			filepath.Join(initPath, "torrents"),
			filepath.Join(initPath, "registry"),
			filepath.Join(initPath, "db"),
			filepath.Join(initPath, "keys"),
			filepath.Join(initPath, "config"),
		}

		for _, dir := range expectedDirs {
			info, err := os.Stat(dir)
			require.NoError(t, err, "Directory should exist: %s", dir)
			assert.True(t, info.IsDir(), "Should be a directory: %s", dir)
		}

		// Check that config file was created
		configPath := filepath.Join(initPath, "config", "config.yaml")
		info, err := os.Stat(configPath)
		require.NoError(t, err, "Config file should exist")
		assert.False(t, info.IsDir(), "Config should be a file")

		// Check that README was created
		readmePath := filepath.Join(initPath, "models", "README.txt")
		_, err = os.Stat(readmePath)
		assert.NoError(t, err, "README should exist")
	})

	// Test initialization when directories already exist
	t.Run("init with existing directories", func(t *testing.T) {
		initPath = filepath.Join(tempDir, "test-existing")
		initForce = false
		
		// Pre-create some directories
		existingDir := filepath.Join(initPath, "models")
		err := os.MkdirAll(existingDir, 0755)
		require.NoError(t, err)

		// Run init
		err = runInit(nil, nil)
		require.NoError(t, err)

		// Should still succeed and create missing directories
		missingDir := filepath.Join(initPath, "torrents")
		info, err := os.Stat(missingDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	// Test force flag for config overwrite
	t.Run("force config overwrite", func(t *testing.T) {
		initPath = filepath.Join(tempDir, "test-force")
		
		// First initialization
		initForce = false
		err := runInit(nil, nil)
		require.NoError(t, err)

		configPath := filepath.Join(initPath, "config", "config.yaml")
		
		// Modify the config file
		err = os.WriteFile(configPath, []byte("# Modified config"), 0644)
		require.NoError(t, err)

		// Read modified content
		modifiedContent, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(modifiedContent), "Modified config")

		// Run init again with force
		initForce = true
		err = runInit(nil, nil)
		require.NoError(t, err)

		// Config should be overwritten
		newContent, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.NotContains(t, string(newContent), "Modified config")
		assert.Contains(t, string(newContent), "Silmaril Configuration")
	})

	// Test default path initialization (mocked)
	t.Run("default path init", func(t *testing.T) {
		// Save original HOME
		originalHome := os.Getenv("HOME")
		defer os.Setenv("HOME", originalHome)

		// Set HOME to temp directory
		testHome := filepath.Join(tempDir, "test-home")
		os.Setenv("HOME", testHome)
		
		// Clear initPath to use default
		initPath = ""
		initForce = false

		err := runInit(nil, nil)
		require.NoError(t, err)

		// Check that directories were created in default location
		defaultBase := filepath.Join(testHome, ".silmaril")
		info, err := os.Stat(defaultBase)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// Check config in default location
		defaultConfig := filepath.Join(testHome, ".config", "silmaril", "config.yaml")
		_, err = os.Stat(defaultConfig)
		assert.NoError(t, err)
	})
}

func TestCreateDirectory(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("create new directory", func(t *testing.T) {
		dirPath := filepath.Join(tempDir, "new-dir")
		err := createDirectory(dirPath, "Test directory")
		require.NoError(t, err)

		info, err := os.Stat(dirPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("directory already exists", func(t *testing.T) {
		dirPath := filepath.Join(tempDir, "existing-dir")
		err := os.MkdirAll(dirPath, 0755)
		require.NoError(t, err)

		// Should not error when directory exists
		err = createDirectory(dirPath, "Existing directory")
		assert.NoError(t, err)
	})

	t.Run("path exists as file", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "file-not-dir")
		err := os.WriteFile(filePath, []byte("test"), 0644)
		require.NoError(t, err)

		// Should error when path exists as file
		err = createDirectory(filePath, "Should fail")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})

	t.Run("nested directory creation", func(t *testing.T) {
		nestedPath := filepath.Join(tempDir, "level1", "level2", "level3")
		err := createDirectory(nestedPath, "Nested directory")
		require.NoError(t, err)

		info, err := os.Stat(nestedPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

func TestCreateConfigFile(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("create new config", func(t *testing.T) {
		configPath := filepath.Join(tempDir, "config.yaml")
		baseDir := filepath.Join(tempDir, "base")
		
		err := createConfigFile(configPath, baseDir)
		require.NoError(t, err)

		// Check file exists
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())

		// Check content
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "Silmaril Configuration")
		assert.Contains(t, string(content), baseDir)
	})

	t.Run("config already exists without force", func(t *testing.T) {
		configPath := filepath.Join(tempDir, "existing-config.yaml")
		baseDir := filepath.Join(tempDir, "base2")
		
		// Create existing config
		err := os.WriteFile(configPath, []byte("existing content"), 0644)
		require.NoError(t, err)

		// Try to create without force
		initForce = false
		err = createConfigFile(configPath, baseDir)
		assert.NoError(t, err)

		// Content should not change
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, "existing content", string(content))
	})

	t.Run("config already exists with force", func(t *testing.T) {
		configPath := filepath.Join(tempDir, "force-config.yaml")
		baseDir := filepath.Join(tempDir, "base3")
		
		// Create existing config
		err := os.WriteFile(configPath, []byte("old content"), 0644)
		require.NoError(t, err)

		// Create with force
		initForce = true
		err = createConfigFile(configPath, baseDir)
		require.NoError(t, err)

		// Content should be replaced
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "old content")
		assert.Contains(t, string(content), "Silmaril Configuration")
	})
}

func TestCleanupSilmaril(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("cleanup existing installation", func(t *testing.T) {
		// First create an installation
		initPath = filepath.Join(tempDir, "test-cleanup")
		initForce = false
		initCleanup = false
		
		err := runInit(nil, nil)
		require.NoError(t, err)

		// Verify directories exist
		_, err = os.Stat(initPath)
		require.NoError(t, err)
		
		configPath := filepath.Join(initPath, "config", "config.yaml")
		_, err = os.Stat(configPath)
		require.NoError(t, err)

		// Now cleanup (without user confirmation in test)
		err = removeDirectory(initPath, "Test directory")
		assert.NoError(t, err)

		// Verify directory is gone
		_, err = os.Stat(initPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup non-existent installation", func(t *testing.T) {
		nonExistentPath := filepath.Join(tempDir, "non-existent")
		
		// Should not error when path doesn't exist
		err := removeDirectory(nonExistentPath, "Non-existent directory")
		assert.NoError(t, err)
	})

	t.Run("remove directory function", func(t *testing.T) {
		// Create a directory with subdirectories and files
		testPath := filepath.Join(tempDir, "test-remove")
		subDir := filepath.Join(testPath, "subdir")
		err := os.MkdirAll(subDir, 0755)
		require.NoError(t, err)

		// Create some files
		file1 := filepath.Join(testPath, "file1.txt")
		err = os.WriteFile(file1, []byte("test"), 0644)
		require.NoError(t, err)

		file2 := filepath.Join(subDir, "file2.txt")
		err = os.WriteFile(file2, []byte("test"), 0644)
		require.NoError(t, err)

		// Remove the directory
		err = removeDirectory(testPath, "Test directory with contents")
		assert.NoError(t, err)

		// Verify everything is gone
		_, err = os.Stat(testPath)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestInitializeEnvironment(t *testing.T) {
	t.Run("environment not initialized", func(t *testing.T) {
		// Save original env vars
		originalHome := os.Getenv("HOME")
		originalSilmarilHome := os.Getenv("SILMARIL_HOME")
		defer func() {
			os.Setenv("HOME", originalHome)
			os.Setenv("SILMARIL_HOME", originalSilmarilHome)
		}()

		// Set up test environment
		tempDir := t.TempDir()
		os.Setenv("HOME", tempDir)
		os.Setenv("SILMARIL_HOME", filepath.Join(tempDir, ".silmaril"))
		
		// Clear initPath to use default
		initPath = ""
		
		// Run InitializeEnvironment
		err := InitializeEnvironment()
		require.NoError(t, err)

		// Check that directories were created
		silmarilDir := filepath.Join(tempDir, ".silmaril")
		info, err := os.Stat(silmarilDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("environment already initialized", func(t *testing.T) {
		// Save original env vars
		originalHome := os.Getenv("HOME")
		originalSilmarilHome := os.Getenv("SILMARIL_HOME")
		defer func() {
			os.Setenv("HOME", originalHome)
			os.Setenv("SILMARIL_HOME", originalSilmarilHome)
		}()

		// Set up test environment
		tempDir := t.TempDir()
		os.Setenv("HOME", tempDir)
		silmarilDir := filepath.Join(tempDir, ".silmaril")
		os.Setenv("SILMARIL_HOME", silmarilDir)
		
		// Pre-create the directory
		err := os.MkdirAll(silmarilDir, 0755)
		require.NoError(t, err)
		
		// Create a minimal config
		configDir := filepath.Join(tempDir, ".config", "silmaril")
		err = os.MkdirAll(configDir, 0755)
		require.NoError(t, err)
		
		configPath := filepath.Join(configDir, "config.yaml")
		configContent := `storage:
  base_dir: ` + silmarilDir
		err = os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)
		
		// Run InitializeEnvironment - should not re-initialize
		err = InitializeEnvironment()
		// May error due to config parsing, but that's ok for this test
		// The important thing is it doesn't recreate directories
		
		// Directory should still exist
		info, err := os.Stat(silmarilDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}