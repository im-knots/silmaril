package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

// Config represents the Silmaril configuration
type Config struct {
	// Storage paths
	Storage StorageConfig `mapstructure:"storage"`
	
	// Network settings
	Network NetworkConfig `mapstructure:"network"`
	
	// Torrent settings
	Torrent TorrentConfig `mapstructure:"torrent"`
	
	// UI settings
	UI UIConfig `mapstructure:"ui"`
	
	// Security settings
	Security SecurityConfig `mapstructure:"security"`
}

type StorageConfig struct {
	BaseDir     string `mapstructure:"base_dir"`
	ModelsDir   string `mapstructure:"models_dir"`
	TorrentsDir string `mapstructure:"torrents_dir"`
	RegistryDir string `mapstructure:"registry_dir"`
	DBDir       string `mapstructure:"db_dir"`
}

type NetworkConfig struct {
	// DHT settings
	DHTEnabled        bool     `mapstructure:"dht_enabled"`
	DHTBootstrapNodes []string `mapstructure:"dht_bootstrap_nodes"`
	DHTPort           int      `mapstructure:"dht_port"`
	
	// Torrent network settings
	ListenPort        int   `mapstructure:"listen_port"`
	MaxConnections    int   `mapstructure:"max_connections"`
	UploadRateLimit   int64 `mapstructure:"upload_rate_limit"`
	DownloadRateLimit int64 `mapstructure:"download_rate_limit"`
	
	// Disable trackers (we want DHT only)
	DisableTrackers bool `mapstructure:"disable_trackers"`
}

type TorrentConfig struct {
	PieceLength     int64   `mapstructure:"piece_length"`
	SeedRatio       float64 `mapstructure:"seed_ratio"`
	SeedTime        int     `mapstructure:"seed_time"`
	DownloadTimeout int     `mapstructure:"download_timeout"`
}

type UIConfig struct {
	ProgressBar  bool   `mapstructure:"progress_bar"`
	Color        bool   `mapstructure:"color"`
	Verbose      bool   `mapstructure:"verbose"`
	OutputFormat string `mapstructure:"output_format"`
}

type SecurityConfig struct {
	SignManifests   bool   `mapstructure:"sign_manifests"`
	VerifyManifests bool   `mapstructure:"verify_manifests"`
	KeysDir         string `mapstructure:"keys_dir"`
}

var (
	cfg  *Config
	v    *viper.Viper
)

// Helper methods for accessing config values

// GetInt returns an integer value from the config
func (c *Config) GetInt(key string) int {
	if v != nil {
		return v.GetInt(key)
	}
	return 0
}

// GetBool returns a boolean value from the config
func (c *Config) GetBool(key string) bool {
	if v != nil {
		return v.GetBool(key)
	}
	return false
}

// GetString returns a string value from the config
func (c *Config) GetString(key string) string {
	if v != nil {
		return v.GetString(key)
	}
	return ""
}

// GetStringSlice returns a string slice from the config
func (c *Config) GetStringSlice(key string) []string {
	if v != nil {
		return v.GetStringSlice(key)
	}
	return nil
}

// Initialize sets up the configuration
func Initialize() error {
	v = viper.New()
	
	// Set config name and type
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	
	// Add config paths
	// 1. Same directory as executable
	if exe, err := os.Executable(); err == nil {
		v.AddConfigPath(filepath.Dir(exe))
	}
	
	// 2. Current working directory
	v.AddConfigPath(".")
	
	// 3. User config directory
	if configDir := getUserConfigDir(); configDir != "" {
		v.AddConfigPath(configDir)
	}
	
	// Set defaults
	setDefaults(v)
	
	// Bind environment variables
	v.SetEnvPrefix("SILMARIL")
	v.AutomaticEnv()
	
	// Read config file if exists
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is ok, we'll use defaults
	}
	
	// Unmarshal into struct
	cfg = &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}
	
	// Expand paths
	expandPaths(cfg)
	
	return nil
}

// setDefaults sets all default values
func setDefaults(v *viper.Viper) {
	// Storage defaults
	v.SetDefault("storage.base_dir", getDefaultBaseDir())
	v.SetDefault("storage.models_dir", "")      // Will be set to base_dir/models
	v.SetDefault("storage.torrents_dir", "")    // Will be set to base_dir/torrents
	v.SetDefault("storage.registry_dir", "")    // Will be set to base_dir/registry
	v.SetDefault("storage.db_dir", "")          // Will be set to base_dir/db
	
	// Network defaults
	v.SetDefault("network.dht_enabled", true)
	v.SetDefault("network.dht_bootstrap_nodes", []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
	})
	v.SetDefault("network.dht_port", 0) // Random port
	v.SetDefault("network.listen_port", 0) // Random port
	v.SetDefault("network.max_connections", 100)
	v.SetDefault("network.upload_rate_limit", 0) // Unlimited
	v.SetDefault("network.download_rate_limit", 0) // Unlimited
	v.SetDefault("network.disable_trackers", true)
	
	// Torrent defaults
	v.SetDefault("torrent.piece_length", 4*1024*1024) // 4MB
	v.SetDefault("torrent.seed_ratio", 0) // Unlimited
	v.SetDefault("torrent.seed_time", 0) // Unlimited
	v.SetDefault("torrent.download_timeout", 1800) // 30 minutes
	
	// UI defaults
	v.SetDefault("ui.progress_bar", true)
	v.SetDefault("ui.color", true)
	v.SetDefault("ui.verbose", false)
	v.SetDefault("ui.output_format", "text") // text or json
	
	// Security defaults
	v.SetDefault("security.sign_manifests", true)
	v.SetDefault("security.verify_manifests", true)
	v.SetDefault("security.keys_dir", "") // Will be set to base_dir/keys
}

// getDefaultBaseDir returns the default base directory
func getDefaultBaseDir() string {
	if dir := os.Getenv("SILMARIL_HOME"); dir != "" {
		return dir
	}
	
	home, err := os.UserHomeDir()
	if err != nil {
		return ".silmaril"
	}
	
	return filepath.Join(home, ".silmaril")
}

// getUserConfigDir returns the user's config directory
func getUserConfigDir() string {
	// Use XDG_CONFIG_HOME if set
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "silmaril")
	}
	
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "silmaril")
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "silmaril")
		}
		return filepath.Join(home, "AppData", "Roaming", "silmaril")
	default:
		return filepath.Join(home, ".config", "silmaril")
	}
}

// expandPaths expands relative paths and sets defaults
func expandPaths(cfg *Config) {
	// Expand base dir
	if cfg.Storage.BaseDir != "" {
		cfg.Storage.BaseDir = expandPath(cfg.Storage.BaseDir)
	}
	
	// Set subdirectories if not specified
	if cfg.Storage.ModelsDir == "" {
		cfg.Storage.ModelsDir = filepath.Join(cfg.Storage.BaseDir, "models")
	} else {
		cfg.Storage.ModelsDir = expandPath(cfg.Storage.ModelsDir)
	}
	
	if cfg.Storage.TorrentsDir == "" {
		cfg.Storage.TorrentsDir = filepath.Join(cfg.Storage.BaseDir, "torrents")
	} else {
		cfg.Storage.TorrentsDir = expandPath(cfg.Storage.TorrentsDir)
	}
	
	if cfg.Storage.RegistryDir == "" {
		cfg.Storage.RegistryDir = filepath.Join(cfg.Storage.BaseDir, "registry")
	} else {
		cfg.Storage.RegistryDir = expandPath(cfg.Storage.RegistryDir)
	}
	
	if cfg.Storage.DBDir == "" {
		cfg.Storage.DBDir = filepath.Join(cfg.Storage.BaseDir, "db")
	} else {
		cfg.Storage.DBDir = expandPath(cfg.Storage.DBDir)
	}
	
	if cfg.Security.KeysDir == "" {
		cfg.Security.KeysDir = filepath.Join(cfg.Storage.BaseDir, "keys")
	} else {
		cfg.Security.KeysDir = expandPath(cfg.Security.KeysDir)
	}
}

// expandPath expands ~ and environment variables
func expandPath(path string) string {
	if path == "" {
		return path
	}
	
	// Expand ~
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	
	// Expand environment variables
	return os.ExpandEnv(path)
}

// Get returns the current configuration
func Get() *Config {
	if cfg == nil {
		panic("config not initialized")
	}
	return cfg
}

// GetViper returns the viper instance
func GetViper() *viper.Viper {
	if v == nil {
		panic("config not initialized")
	}
	return v
}

// SaveConfig saves the current configuration to file
func SaveConfig(path string) error {
	return v.WriteConfigAs(path)
}

// CreateAllDirs creates all configured directories
func CreateAllDirs() error {
	dirs := []string{
		cfg.Storage.BaseDir,
		cfg.Storage.ModelsDir,
		cfg.Storage.TorrentsDir,
		cfg.Storage.RegistryDir,
		cfg.Storage.DBDir,
		cfg.Security.KeysDir,
	}
	
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	
	// Keys dir should be more secure
	if err := os.Chmod(cfg.Security.KeysDir, 0700); err != nil {
		return fmt.Errorf("failed to secure keys directory: %w", err)
	}
	
	return nil
}