package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/silmaril/silmaril/internal/torrent"
)

var shareCmd = &cobra.Command{
	Use:   "share [path]",
	Short: "Seed models to the P2P network",
	Long: `Seeds all downloaded models or a specific model/torrent file to help
distribute them across the network.`,
	RunE: runShare,
}

var (
	shareAll bool
)

func init() {
	rootCmd.AddCommand(shareCmd)
	
	shareCmd.Flags().BoolVar(&shareAll, "all", false, "seed all downloaded models")
}

func runShare(cmd *cobra.Command, args []string) error {
	// Create torrent client
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	
	cfg := torrent.Config{
		DataDir:        filepath.Join(home, ".silmaril", "models"),
		MaxConnections: 100,
		SeedRatio:      0, // Seed indefinitely
	}
	
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create torrent client: %w", err)
	}
	defer client.Close()
	
	if shareAll || len(args) == 0 {
		// Seed all models in the models directory
		fmt.Println("Seeding all downloaded models...")
		
		modelsDir := filepath.Join(home, ".silmaril", "models")
		err = filepath.Walk(modelsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			
			if filepath.Ext(path) == ".torrent" {
				fmt.Printf("Loading torrent: %s\n", filepath.Base(path))
				_, err := client.AddTorrentFile(path)
				if err != nil {
					fmt.Printf("Warning: Failed to add %s: %v\n", path, err)
				}
			}
			
			return nil
		})
		
		if err != nil {
			return fmt.Errorf("failed to scan models directory: %w", err)
		}
	} else {
		// Seed specific file or directory
		path := args[0]
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot access path: %w", err)
		}
		
		if info.IsDir() {
			// Look for torrent file in directory
			torrentPath := filepath.Join(path, "model.torrent")
			if _, err := os.Stat(torrentPath); err == nil {
				fmt.Printf("Loading torrent from directory: %s\n", torrentPath)
				_, err = client.AddTorrentFile(torrentPath)
				if err != nil {
					return fmt.Errorf("failed to add torrent: %w", err)
				}
			} else {
				return fmt.Errorf("no model.torrent found in directory")
			}
		} else if filepath.Ext(path) == ".torrent" {
			// Direct torrent file
			fmt.Printf("Loading torrent: %s\n", path)
			_, err = client.AddTorrentFile(path)
			if err != nil {
				return fmt.Errorf("failed to add torrent: %w", err)
			}
		} else {
			return fmt.Errorf("path must be a directory with model.torrent or a .torrent file")
		}
	}
	
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	
	// Monitor seeding
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	fmt.Println("\nSeeding models... Press Ctrl+C to stop")
	fmt.Println()
	
	for {
		select {
		case <-ticker.C:
			// Print seeding statistics
			var totalUploaded int64
			var activeTorrents int
			
			// TODO: Get stats from client
			// This would require adding a method to list all torrents
			
			fmt.Printf("\rActive torrents: %d | Total uploaded: %.2f GB",
				activeTorrents, float64(totalUploaded)/(1024*1024*1024))
			
		case <-sigChan:
			fmt.Println("\n\nShutting down...")
			return nil
		}
	}
}