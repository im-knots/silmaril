package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProgressBar wraps the progressbar library with our styling
type ProgressBar struct {
	bar        *progressbar.ProgressBar
	startTime  time.Time
	totalBytes int64
}

// NewProgressBar creates a new progress bar for downloads
func NewProgressBar(totalBytes int64, description string) *ProgressBar {
	bar := progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(nil), // Use default stdout
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]█[reset]",
			SaucerHead:    "[green]█[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	
	return &ProgressBar{
		bar:        bar,
		startTime:  time.Now(),
		totalBytes: totalBytes,
	}
}

// Update updates the progress bar with current bytes
func (p *ProgressBar) Update(currentBytes int64) {
	p.bar.Set64(currentBytes)
}

// UpdateWithStats updates with additional statistics
func (p *ProgressBar) UpdateWithStats(currentBytes int64, downloadRate float64, peers int, seeders int) {
	// Build description with stats
	desc := fmt.Sprintf("Downloading [%.1f MB/s | Peers: %d | Seeders: %d]",
		downloadRate/(1024*1024), peers, seeders)
	
	p.bar.Describe(desc)
	p.bar.Set64(currentBytes)
}

// Finish completes the progress bar
func (p *ProgressBar) Finish() {
	p.bar.Finish()
}

// MultiFileProgress handles progress for multiple files
type MultiFileProgress struct {
	files      map[string]*ProgressBar
	totalBar   *ProgressBar
	totalBytes int64
}

// NewMultiFileProgress creates a progress tracker for multiple files
func NewMultiFileProgress(totalBytes int64) *MultiFileProgress {
	return &MultiFileProgress{
		files:      make(map[string]*ProgressBar),
		totalBar:   NewProgressBar(totalBytes, "Total Progress"),
		totalBytes: totalBytes,
	}
}

// AddFile adds a file to track
func (m *MultiFileProgress) AddFile(filename string, size int64) {
	shortName := filename
	if len(filename) > 40 {
		shortName = "..." + filename[len(filename)-37:]
	}
	m.files[filename] = NewProgressBar(size, shortName)
}

// UpdateFile updates progress for a specific file
func (m *MultiFileProgress) UpdateFile(filename string, currentBytes int64) {
	if bar, ok := m.files[filename]; ok {
		bar.Update(currentBytes)
	}
}

// UpdateTotal updates the total progress
func (m *MultiFileProgress) UpdateTotal(currentBytes int64, downloadRate float64, peers int, seeders int) {
	m.totalBar.UpdateWithStats(currentBytes, downloadRate, peers, seeders)
}

// Finish completes all progress bars
func (m *MultiFileProgress) Finish() {
	for _, bar := range m.files {
		bar.Finish()
	}
	m.totalBar.Finish()
}

// FormatBytes formats bytes into human readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats duration into human readable format
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// FormatETA calculates and formats estimated time of arrival
func FormatETA(bytesRemaining int64, bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "calculating..."
	}
	
	seconds := float64(bytesRemaining) / bytesPerSecond
	return FormatDuration(time.Duration(seconds) * time.Second)
}

// TruncateString truncates a string with ellipsis
func TruncateString(str string, maxLen int) string {
	if len(str) <= maxLen {
		return str
	}
	if maxLen <= 3 {
		return str[:maxLen]
	}
	return str[:maxLen-3] + "..."
}

// PadRight pads a string to the right
func PadRight(str string, length int) string {
	if len(str) >= length {
		return str
	}
	return str + strings.Repeat(" ", length-len(str))
}