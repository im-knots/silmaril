package discovery

import (
	"strings"
)

const (
	// Well-known seed for the Silmaril discovery catalog
	WellKnownSeed = "silmaril-discovery-v1"
	
	// Maximum size for BEP 44 value (1000 bytes)
	MaxValueSize = 1000
)

// ModelCatalog is the catalog of all discoverable models
type ModelCatalog struct {
	Version  int                    `json:"v"`
	Sequence int64                  `json:"seq"`
	Updated  int64                  `json:"t"`
	Models   map[string]ModelEntry  `json:"m"`
}

// ModelEntry is a single model in the catalog
type ModelEntry struct {
	InfoHash string   `json:"h"`
	Size     int64    `json:"s,omitempty"`
	Tags     []string `json:"t,omitempty"`
	Added    int64    `json:"a"`
}

// extractTags extracts searchable tags from a model name
func extractTags(name string) []string {
	var tags []string
	tagSet := make(map[string]bool) // Use set to avoid duplicates
	lower := strings.ToLower(name)
	
	// Extract org/model parts
	if parts := strings.Split(lower, "/"); len(parts) > 0 {
		// Add organization part, splitting on hyphens
		for _, part := range strings.Split(parts[0], "-") {
			if part != "" {
				tagSet[part] = true
			}
		}
		
		// Add model name parts, splitting on hyphens
		if len(parts) > 1 {
			for _, part := range strings.Split(parts[1], "-") {
				if part != "" {
					tagSet[part] = true
				}
			}
		}
	} else {
		// No slash, just split on hyphens
		for _, part := range strings.Split(lower, "-") {
			if part != "" {
				tagSet[part] = true
			}
		}
	}
	
	// Common sizes - add these as additional tags
	for _, size := range []string{"3b", "7b", "8b", "8x7b", "13b", "70b"} {
		if strings.Contains(lower, size) {
			tagSet[size] = true
		}
	}
	
	// Model families - add if contained in name
	for _, family := range []string{"llama", "mistral", "gpt", "gemma", "phi"} {
		if strings.Contains(lower, family) {
			tagSet[family] = true
		}
	}
	
	// Convert set to slice
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	
	return tags
}

// matchesPattern checks if a name matches a search pattern
func matchesPattern(name, pattern string) bool {
	// Handle wildcard pattern
	if pattern == "*" || pattern == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}