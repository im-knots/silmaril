package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths manages all storage locations for Silmaril
type Paths struct {
	baseDir     string
	modelsDir   string
	torrentsDir string
	registryDir string
	configDir   string
	dbDir       string
}

// NewPaths creates a new Paths instance
func NewPaths() (*Paths, error) {
	baseDir, err := getBaseDir()
	if err != nil {
		return nil, err
	}
	
	p := &Paths{
		baseDir:     baseDir,
		modelsDir:   filepath.Join(baseDir, "models"),
		torrentsDir: filepath.Join(baseDir, "torrents"),
		registryDir: filepath.Join(baseDir, "registry"),
		dbDir:       filepath.Join(baseDir, "db"),
	}
	
	// Config dir is separate
	configDir, err := getConfigDir()
	if err != nil {
		return nil, err
	}
	p.configDir = configDir
	
	return p, nil
}

// getBaseDir returns the base directory for Silmaril data
func getBaseDir() (string, error) {
	// Check environment variable first
	if dir := os.Getenv("SILMARIL_HOME"); dir != "" {
		return dir, nil
	}
	
	// Default to ~/.silmaril
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	
	return filepath.Join(home, ".silmaril"), nil
}

// getConfigDir returns the configuration directory
func getConfigDir() (string, error) {
	// Check environment variable first
	if dir := os.Getenv("SILMARIL_CONFIG"); dir != "" {
		return dir, nil
	}
	
	// Use XDG_CONFIG_HOME if set
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "silmaril"), nil
	}
	
	// Platform-specific defaults
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "silmaril"), nil
		
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "silmaril"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Roaming", "silmaril"), nil
		
	default: // Linux and others
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "silmaril"), nil
	}
}

// Initialize creates all necessary directories
func (p *Paths) Initialize() error {
	dirs := []string{
		p.modelsDir,
		p.torrentsDir,
		p.registryDir,
		p.configDir,
		p.dbDir,
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	
	return nil
}

// BaseDir returns the base directory
func (p *Paths) BaseDir() string {
	return p.baseDir
}

// ModelsDir returns the models directory
func (p *Paths) ModelsDir() string {
	return p.modelsDir
}

// ModelPath returns the path for a specific model
func (p *Paths) ModelPath(modelName string) string {
	return filepath.Join(p.modelsDir, modelName)
}

// TorrentsDir returns the torrents directory
func (p *Paths) TorrentsDir() string {
	return p.torrentsDir
}

// TorrentPath returns the path for a specific torrent file
func (p *Paths) TorrentPath(modelName string) string {
	return filepath.Join(p.torrentsDir, modelName+".torrent")
}

// RegistryDir returns the registry directory
func (p *Paths) RegistryDir() string {
	return p.registryDir
}

// ConfigDir returns the config directory
func (p *Paths) ConfigDir() string {
	return p.configDir
}

// ConfigPath returns the main config file path
func (p *Paths) ConfigPath() string {
	return filepath.Join(p.configDir, "config.yaml")
}

// DBDir returns the database directory
func (p *Paths) DBDir() string {
	return p.dbDir
}

// GetDiskUsage returns disk usage statistics for Silmaril
func (p *Paths) GetDiskUsage() (DiskUsage, error) {
	usage := DiskUsage{
		Models:    getDirSize(p.modelsDir),
		Torrents:  getDirSize(p.torrentsDir),
		Registry:  getDirSize(p.registryDir),
		Database:  getDirSize(p.dbDir),
	}
	usage.Total = usage.Models + usage.Torrents + usage.Registry + usage.Database
	
	return usage, nil
}

// DiskUsage represents disk space usage
type DiskUsage struct {
	Total     int64
	Models    int64
	Torrents  int64
	Registry  int64
	Database  int64
}

// getDirSize calculates the total size of a directory
func getDirSize(path string) int64 {
	var size int64
	
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	
	return size
}

// CleanupOldFiles removes old temporary files
func (p *Paths) CleanupOldFiles(daysOld int) error {
	// Clean up old torrent files that aren't being used
	// Clean up incomplete downloads
	// etc.
	return nil
}