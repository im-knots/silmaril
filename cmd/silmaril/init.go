package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/config"
	"github.com/silmaril/silmaril/internal/storage"
)

var (
	initForce   bool
	initPath    string
	initCleanup bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize or clean up Silmaril directories and configuration",
	Long: `Initialize the Silmaril environment by creating necessary directories
and a default configuration file if they don't already exist.

This command will create:
  - Base directory (~/.silmaril by default)
  - Models directory for storing downloaded models
  - Torrents directory for torrent files
  - Registry directory for model manifests
  - Database directory for metadata
  - Configuration file (~/.config/silmaril/config.yaml)

Use --path to initialize in a custom location instead of the default.
Use --cleanup to remove all Silmaril directories and configuration.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing configuration")
	initCmd.Flags().StringVar(&initPath, "path", "", "initialize in a custom path instead of default")
	initCmd.Flags().BoolVar(&initCleanup, "cleanup", false, "remove all Silmaril directories and configuration")
}

func runInit(cmd *cobra.Command, args []string) error {
	// Determine base directory
	var baseDir string
	if initPath != "" {
		// Use custom path
		baseDir = initPath
	} else {
		// Use default path
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".silmaril")
	}

	// Handle cleanup if requested
	if initCleanup {
		return cleanupSilmaril(baseDir)
	}

	fmt.Printf("Initializing Silmaril in: %s\n\n", baseDir)

	// Create directory structure
	dirs := []struct {
		path string
		desc string
	}{
		{baseDir, "Base directory"},
		{filepath.Join(baseDir, "models"), "Models directory"},
		{filepath.Join(baseDir, "torrents"), "Torrents directory"},
		{filepath.Join(baseDir, "registry"), "Registry directory"},
		{filepath.Join(baseDir, "db"), "Database directory"},
		{filepath.Join(baseDir, "keys"), "Keys directory"},
	}

	for _, dir := range dirs {
		if err := createDirectory(dir.path, dir.desc); err != nil {
			return err
		}
	}

	// Create configuration file
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "silmaril")
	if initPath != "" {
		// If custom path, put config in the same directory
		configDir = filepath.Join(baseDir, "config")
	}
	
	if err := createDirectory(configDir, "Configuration directory"); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if err := createConfigFile(configPath, baseDir); err != nil {
		return err
	}

	// Create a README in the models directory
	readmePath := filepath.Join(baseDir, "models", "README.txt")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		readmeContent := `This directory contains AI models managed by Silmaril.

Models are organized in the format:
  organization/model-name/

Each model directory contains:
  - Model files (*.safetensors, *.bin, etc.)
  - Configuration files (config.json, tokenizer.json)
  - Silmaril manifest (.silmaril.json)

Do not manually modify .silmaril.json files as they are managed by Silmaril.
`
		if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
			fmt.Printf("  ⚠️  Failed to create README: %v\n", err)
		} else {
			fmt.Printf("  ✅ Created models README\n")
		}
	}

	fmt.Println("\n✅ Silmaril initialization complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Run 'silmaril discover' to search for available models")
	fmt.Println("  2. Run 'silmaril get <model-name>' to download a model")
	fmt.Println("  3. Run 'silmaril mirror <huggingface-url>' to mirror from HuggingFace")
	
	if initPath != "" {
		fmt.Printf("\nNote: You initialized in a custom location: %s\n", baseDir)
		fmt.Printf("Set SILMARIL_HOME=%s to use this location by default\n", baseDir)
	}

	return nil
}

func createDirectory(path, description string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			fmt.Printf("  ✓ %s already exists: %s\n", description, path)
			return nil
		}
		return fmt.Errorf("%s exists but is not a directory: %s", description, path)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", description, err)
	}
	
	fmt.Printf("  ✅ Created %s: %s\n", description, path)
	return nil
}

func createConfigFile(configPath, baseDir string) error {
	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		if !initForce {
			fmt.Printf("  ✓ Configuration already exists: %s\n", configPath)
			fmt.Println("    (use --force to overwrite)")
			return nil
		}
		fmt.Printf("  ⚠️  Overwriting existing configuration\n")
	}

	// Create default configuration
	configContent := fmt.Sprintf(`# Silmaril Configuration
# Generated by 'silmaril init'

# Storage configuration
storage:
  base_dir: %s
  models_dir: %s
  torrents_dir: %s
  registry_dir: %s
  db_dir: %s

# Network configuration
network:
  dht_enabled: true
  dht_bootstrap_nodes:
    - "router.bittorrent.com:6881"
    - "dht.transmissionbt.com:6881"
    - "router.utorrent.com:6881"
  dht_port: 0  # 0 = random port
  listen_port: 0  # 0 = random port
  max_connections: 50
  upload_rate_limit: 0    # bytes/sec, 0 = unlimited
  download_rate_limit: 0  # bytes/sec, 0 = unlimited
  disable_trackers: true

# Torrent configuration
torrent:
  piece_length: 4194304   # 4MB pieces
  seed_ratio: 0           # 0 = unlimited
  seed_time: 0            # seconds, 0 = unlimited
  download_timeout: 1800  # 30 minutes

# UI configuration
ui:
  progress_bar: true
  color: true
  verbose: false

# Security configuration
security:
  sign_manifests: true
  verify_manifests: true
  verify_checksums: true
  keys_dir: %s
`,
		baseDir,
		filepath.Join(baseDir, "models"),
		filepath.Join(baseDir, "torrents"),
		filepath.Join(baseDir, "registry"),
		filepath.Join(baseDir, "db"),
		filepath.Join(baseDir, "keys"),
	)

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to create configuration file: %w", err)
	}

	fmt.Printf("  ✅ Created configuration: %s\n", configPath)
	return nil
}

// InitializeEnvironment is a helper function that can be called by other commands
// to ensure the environment is set up
func InitializeEnvironment() error {
	paths, err := storage.NewPaths()
	if err != nil {
		return err
	}

	// Check if base directory exists
	if _, err := os.Stat(paths.BaseDir()); os.IsNotExist(err) {
		fmt.Println("Silmaril is not initialized. Running initialization...")
		fmt.Println()
		
		// Run init with default settings
		if err := runInit(nil, nil); err != nil {
			return fmt.Errorf("failed to initialize: %w", err)
		}
		fmt.Println()
	}

	// Ensure config is loaded
	if err := config.Initialize(); err != nil {
		// Config might not exist, try to create it
		homeDir, _ := os.UserHomeDir()
		configPath := filepath.Join(homeDir, ".config", "silmaril", "config.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// Create minimal config
			configDir := filepath.Dir(configPath)
			os.MkdirAll(configDir, 0755)
			if err := createConfigFile(configPath, paths.BaseDir()); err != nil {
				return err
			}
			// Try loading again
			return config.Initialize()
		}
		return err
	}

	return nil
}

// cleanupSilmaril removes all Silmaril directories and configuration
func cleanupSilmaril(baseDir string) error {
	fmt.Printf("Cleaning up Silmaril installation...\n\n")

	// Confirm with user
	fmt.Printf("⚠️  WARNING: This will remove:\n")
	fmt.Printf("  - Base directory: %s\n", baseDir)
	
	// Determine config directory
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "silmaril")
	if initPath != "" {
		configDir = filepath.Join(baseDir, "config")
	}
	fmt.Printf("  - Configuration: %s\n", configDir)
	
	fmt.Printf("\nThis action cannot be undone. All downloaded models will be deleted.\n")
	fmt.Printf("Are you sure? Type 'yes' to continue: ")
	
	var response string
	fmt.Scanln(&response)
	
	if response != "yes" {
		fmt.Println("Cleanup cancelled.")
		return nil
	}

	fmt.Println()

	// Remove base directory
	if err := removeDirectory(baseDir, "Base directory"); err != nil {
		// Don't fail if directory doesn't exist
		if !os.IsNotExist(err) {
			fmt.Printf("  ⚠️  Failed to remove base directory: %v\n", err)
		}
	}

	// Remove config directory
	if err := removeDirectory(configDir, "Configuration directory"); err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("  ⚠️  Failed to remove configuration: %v\n", err)
		}
	}

	fmt.Println("\n✅ Silmaril cleanup complete!")
	fmt.Println("\nTo reinstall, run: silmaril init")
	
	return nil
}

// removeDirectory removes a directory and all its contents
func removeDirectory(path, description string) error {
	// Check if directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("  ✓ %s does not exist: %s\n", description, path)
		return nil
	}

	// Remove the directory
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	
	fmt.Printf("  ✅ Removed %s: %s\n", description, path)
	return nil
}