package torrent

import (
	"context"
	"fmt"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

type Client struct {
	client    *torrent.Client
	downloads map[string]*Download
	config    Config
}

type Config struct {
	DataDir          string
	SeedRatio        float64
	DownloadTimeout  time.Duration
	MaxConnections   int
	UploadRateLimit  int64
	DownloadRateLimit int64
}

type Download struct {
	Torrent  *torrent.Torrent
	Progress chan ProgressUpdate
	Done     chan error
	ctx      context.Context
	cancel   context.CancelFunc
}

type ProgressUpdate struct {
	BytesCompleted int64
	BytesTotal     int64
	DownloadRate   float64
	UploadRate     float64
	NumPeers       int
	NumSeeders     int
}

func NewClient(cfg Config) (*Client, error) {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = cfg.DataDir
	clientConfig.Seed = true
	
	// Enable DHT - this is crucial for decentralized operation
	clientConfig.NoDHT = false
	
	// Disable trackers to rely purely on DHT
	clientConfig.DisableTrackers = true
	clientConfig.DisableWebtorrent = true
	
	// Enable peer exchange for better peer discovery
	clientConfig.DisablePEX = false
	
	// Set reasonable timeouts
	clientConfig.HandshakesTimeout = 20 * time.Second
	
	if cfg.MaxConnections > 0 {
		clientConfig.EstablishedConnsPerTorrent = cfg.MaxConnections / 2
		clientConfig.HalfOpenConnsPerTorrent = cfg.MaxConnections / 2
	}
	
	if cfg.UploadRateLimit > 0 {
		clientConfig.UploadRateLimiter = rate.NewLimiter(rate.Limit(cfg.UploadRateLimit), int(cfg.UploadRateLimit))
	}
	
	if cfg.DownloadRateLimit > 0 {
		clientConfig.DownloadRateLimiter = rate.NewLimiter(rate.Limit(cfg.DownloadRateLimit), int(cfg.DownloadRateLimit))
	}
	
	// Use file storage
	clientConfig.DefaultStorage = storage.NewFileByInfoHash(cfg.DataDir)
	
	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}
	
	return &Client{
		client:    client,
		downloads: make(map[string]*Download),
		config:    cfg,
	}, nil
}

func (c *Client) Close() error {
	c.client.Close()
	return nil
}

func (c *Client) AddMagnet(magnetURI string) (*Download, error) {
	t, err := c.client.AddMagnet(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	dl := &Download{
		Torrent:  t,
		Progress: make(chan ProgressUpdate, 1),
		Done:     make(chan error, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
	
	c.downloads[t.InfoHash().String()] = dl
	
	// Start monitoring download
	go c.monitorDownload(dl)
	
	return dl, nil
}

func (c *Client) AddTorrentFile(path string) (*Download, error) {
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load torrent file: %w", err)
	}
	
	t, err := c.client.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("failed to add torrent: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	dl := &Download{
		Torrent:  t,
		Progress: make(chan ProgressUpdate, 1),
		Done:     make(chan error, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
	
	c.downloads[t.InfoHash().String()] = dl
	
	// Start monitoring download
	go c.monitorDownload(dl)
	
	return dl, nil
}

func (c *Client) monitorDownload(dl *Download) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	// Wait for info
	select {
	case <-dl.Torrent.GotInfo():
	case <-dl.ctx.Done():
		dl.Done <- dl.ctx.Err()
		return
	case <-time.After(c.config.DownloadTimeout):
		dl.Done <- fmt.Errorf("timeout waiting for torrent info")
		return
	}
	
	// Start download
	dl.Torrent.DownloadAll()
	
	var lastBytes int64
	lastTime := time.Now()
	
	for {
		select {
		case <-ticker.C:
			stats := dl.Torrent.Stats()
			now := time.Now()
			duration := now.Sub(lastTime).Seconds()
			
			downloadRate := float64(stats.BytesReadData.Int64()-lastBytes) / duration
			
			update := ProgressUpdate{
				BytesCompleted: dl.Torrent.BytesCompleted(),
				BytesTotal:     dl.Torrent.Length(),
				DownloadRate:   downloadRate,
				UploadRate:     float64(stats.BytesWrittenData.Int64()) / duration,
				NumPeers:       len(dl.Torrent.PeerConns()),
				NumSeeders:     dl.Torrent.Stats().ConnectedSeeders,
			}
			
			select {
			case dl.Progress <- update:
			default:
			}
			
			lastBytes = stats.BytesReadData.Int64()
			lastTime = now
			
			// Check if complete
			if dl.Torrent.BytesCompleted() == dl.Torrent.Length() {
				dl.Done <- nil
				return
			}
			
		case <-dl.ctx.Done():
			dl.Done <- dl.ctx.Err()
			return
		}
	}
}

func (dl *Download) Cancel() {
	dl.cancel()
}