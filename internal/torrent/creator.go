package torrent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// CreateTorrentFromDirectory creates a .torrent file from a directory
func CreateTorrentFromDirectory(sourceDir string, outputPath string, pieceLength int64) (string, error) {
	fmt.Printf("[TorrentCreator] Creating torrent from directory: %s\n", sourceDir)
	fmt.Printf("[TorrentCreator] Output path: %s\n", outputPath)
	
	// Set default piece length if not specified
	if pieceLength <= 0 {
		pieceLength = 4 * 1024 * 1024 // 4MB default
	}
	fmt.Printf("[TorrentCreator] Using piece length: %d bytes\n", pieceLength)

	// Create metainfo builder
	info := metainfo.Info{
		PieceLength: pieceLength,
	}

	// Build file list
	err := filepath.Walk(sourceDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		
		// Skip hidden files and special files
		if filepath.Base(path)[0] == '.' {
			return nil
		}
		
		// Skip the silmaril manifest file itself
		if filepath.Base(path) == ".silmaril.json" {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   []string{filepath.ToSlash(relPath)},
			Length: fi.Size(),
		})
		
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}
	fmt.Printf("[TorrentCreator] Found %d files to include\n", len(info.Files))

	// Calculate pieces
	fmt.Printf("[TorrentCreator] Generating pieces...\n")
	err = info.GeneratePieces(func(fi metainfo.FileInfo) (io.ReadCloser, error) {
		path := filepath.Join(sourceDir, filepath.FromSlash(fi.Path[0]))
		return os.Open(path)
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate pieces: %w", err)
	}

	// Create the metainfo
	mi := metainfo.MetaInfo{
		InfoBytes: nil,
	}
	
	// Set the info
	mi.InfoBytes, err = bencode.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("failed to marshal info: %w", err)
	}

	// Set creation date and metadata
	mi.CreationDate = time.Now().Unix()
	mi.CreatedBy = "Silmaril P2P"
	mi.Comment = "Distributed via Silmaril P2P network"
	
	// No need to add DHT nodes here - the daemon handles all DHT operations

	// Write to file
	file, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create torrent file: %w", err)
	}
	defer file.Close()

	err = mi.Write(file)
	if err != nil {
		return "", fmt.Errorf("failed to write torrent file: %w", err)
	}

	// Calculate info hash
	h := sha256.New()
	h.Write(mi.InfoBytes)
	infoHash := hex.EncodeToString(h.Sum(nil))
	
	fmt.Printf("[TorrentCreator] Torrent created successfully\n")
	fmt.Printf("[TorrentCreator] InfoHash: %s\n", infoHash)
	fmt.Printf("[TorrentCreator] Torrent file: %s\n", outputPath)

	return infoHash, nil
}