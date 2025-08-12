package torrent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	config := Config{
		DataDir:         t.TempDir(),
		SeedRatio:       2.0,
		DownloadTimeout: 30 * time.Second,
		MaxConnections:  50,
	}

	client, err := NewClient(config)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, client.client)
	assert.NotNil(t, client.downloads)

	// Close should not error
	err = client.Close()
	assert.NoError(t, err)
}

func TestClientAddMagnet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping torrent test in short mode")
	}

	config := Config{
		DataDir:         t.TempDir(),
		DownloadTimeout: 5 * time.Second,
	}

	client, err := NewClient(config)
	require.NoError(t, err)
	defer client.Close()

	// Use a well-formed but non-existent magnet link
	magnetURI := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test"

	download, err := client.AddMagnet(magnetURI)
	require.NoError(t, err)
	assert.NotNil(t, download)
	assert.NotNil(t, download.Torrent)
	assert.NotNil(t, download.Progress)
	assert.NotNil(t, download.Done)

	// Cancel the download
	download.Cancel()
}

func TestClientConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		check  func(t *testing.T, c *Client)
	}{
		{
			name: "with rate limits",
			config: Config{
				DataDir:           t.TempDir(),
				UploadRateLimit:   1024 * 1024,   // 1MB/s
				DownloadRateLimit: 2 * 1024 * 1024, // 2MB/s
			},
			check: func(t *testing.T, c *Client) {
				assert.Equal(t, int64(1024*1024), c.config.UploadRateLimit)
				assert.Equal(t, int64(2*1024*1024), c.config.DownloadRateLimit)
			},
		},
		{
			name: "with connection limits",
			config: Config{
				DataDir:        t.TempDir(),
				MaxConnections: 100,
			},
			check: func(t *testing.T, c *Client) {
				assert.Equal(t, 100, c.config.MaxConnections)
			},
		},
		{
			name: "with seed ratio",
			config: Config{
				DataDir:   t.TempDir(),
				SeedRatio: 3.0,
			},
			check: func(t *testing.T, c *Client) {
				assert.Equal(t, 3.0, c.config.SeedRatio)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			require.NoError(t, err)
			defer client.Close()

			tt.check(t, client)
		})
	}
}

func TestProgressUpdate(t *testing.T) {
	update := ProgressUpdate{
		BytesCompleted: 1024 * 1024,     // 1MB
		BytesTotal:     10 * 1024 * 1024, // 10MB
		DownloadRate:   512 * 1024,       // 512KB/s
		UploadRate:     256 * 1024,       // 256KB/s
		NumPeers:       10,
		NumSeeders:     5,
	}

	assert.Equal(t, int64(1024*1024), update.BytesCompleted)
	assert.Equal(t, int64(10*1024*1024), update.BytesTotal)
	assert.Equal(t, float64(512*1024), update.DownloadRate)
	assert.Equal(t, float64(256*1024), update.UploadRate)
	assert.Equal(t, 10, update.NumPeers)
	assert.Equal(t, 5, update.NumSeeders)

	// Calculate percentage
	percentage := float64(update.BytesCompleted) / float64(update.BytesTotal) * 100
	assert.Equal(t, float64(10), percentage)
}

func TestDownloadCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	download := &Download{
		Progress: make(chan ProgressUpdate, 1),
		Done:     make(chan error, 1),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Cancel should work
	download.Cancel()

	// Context should be cancelled
	select {
	case <-download.ctx.Done():
		// Expected
	default:
		t.Fatal("Context should be cancelled")
	}
}

// MockClient for testing components that depend on torrent client
type MockClient struct {
	downloads     map[string]*Download
	shouldFail    bool
	addMagnetFunc func(magnetURI string) (*Download, error)
}

func NewMockClient() *MockClient {
	return &MockClient{
		downloads: make(map[string]*Download),
	}
}

func (m *MockClient) AddMagnet(magnetURI string) (*Download, error) {
	if m.addMagnetFunc != nil {
		return m.addMagnetFunc(magnetURI)
	}

	if m.shouldFail {
		return nil, assert.AnError
	}

	ctx, cancel := context.WithCancel(context.Background())
	dl := &Download{
		Progress: make(chan ProgressUpdate, 1),
		Done:     make(chan error, 1),
		ctx:      ctx,
		cancel:   cancel,
	}

	m.downloads[magnetURI] = dl

	// Simulate download progress
	go func() {
		for i := 0; i <= 100; i += 10 {
			select {
			case <-dl.ctx.Done():
				dl.Done <- dl.ctx.Err()
				return
			case dl.Progress <- ProgressUpdate{
				BytesCompleted: int64(i * 10240),
				BytesTotal:     1024 * 1024,
				DownloadRate:   1024 * 100,
				NumPeers:       5,
			}:
			}
			time.Sleep(10 * time.Millisecond)
		}
		dl.Done <- nil
	}()

	return dl, nil
}

func (m *MockClient) Close() error {
	for _, dl := range m.downloads {
		dl.Cancel()
	}
	return nil
}

func TestMockClient(t *testing.T) {
	mock := NewMockClient()

	// Test successful operation
	magnetURI := "magnet:?xt=urn:btih:test"
	dl, err := mock.AddMagnet(magnetURI)
	assert.NoError(t, err)
	assert.NotNil(t, dl)

	// Receive some progress updates
	select {
	case update := <-dl.Progress:
		assert.GreaterOrEqual(t, update.BytesCompleted, int64(0))
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for progress")
	}

	// Cancel download
	dl.Cancel()

	// Should receive cancellation
	select {
	case err := <-dl.Done:
		assert.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for done")
	}

	// Test failure mode
	mock.shouldFail = true
	_, err = mock.AddMagnet(magnetURI)
	assert.Error(t, err)
}