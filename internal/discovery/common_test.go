package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTagsFromModelNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:  "llama model with size",
			input: "meta-llama/Llama-2-7b-hf",
			expected: []string{"meta", "llama", "7b"},
		},
		{
			name:  "mistral model",
			input: "mistralai/Mistral-7B-v0.1",
			expected: []string{"mistralai", "mistral", "7b"},
		},
		{
			name:  "gpt model",
			input: "openai/gpt-3b-instruct",
			expected: []string{"openai", "gpt", "3b", "instruct"},
		},
		{
			name:  "phi model",
			input: "microsoft/phi-2",
			expected: []string{"microsoft", "phi"},
		},
		{
			name:  "gemma model",
			input: "google/gemma-7b",
			expected: []string{"google", "gemma", "7b"},
		},
		{
			name:  "model with 13b size",
			input: "org/model-13b-chat",
			expected: []string{"org", "model", "13b", "chat"},
		},
		{
			name:  "model with 70b size",
			input: "meta-llama/Llama-2-70b",
			expected: []string{"meta", "llama", "70b"},
		},
		{
			name:  "model with 8b size",
			input: "meta-llama/Llama-3.1-8B",
			expected: []string{"meta", "llama", "8b"},
		},
		{
			name:  "simple name",
			input: "mymodel",
			expected: []string{"mymodel"},
		},
		{
			name:  "name with many hyphens",
			input: "org/my-cool-model-v2",
			expected: []string{"org", "my", "cool", "model", "v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := extractTags(tt.input)
			
			// Check that all expected tags are present
			for _, expectedTag := range tt.expected {
				assert.Contains(t, tags, expectedTag, 
					"Expected tag '%s' not found in tags for '%s'", expectedTag, tt.input)
			}
		})
	}
}

func TestMatchesPatternCases(t *testing.T) {
	tests := []struct {
		name     string
		modelName string
		pattern  string
		expected bool
	}{
		// Wildcard patterns
		{"wildcard matches all", "any-model-name", "*", true},
		{"empty pattern matches all", "any-model-name", "", true},
		
		// Exact matches
		{"exact match", "llama", "llama", true},
		{"exact no match", "llama", "mistral", false},
		
		// Case insensitive
		{"case insensitive match", "LLaMA-7B", "llama", true},
		{"case insensitive pattern", "llama-7b", "LLAMA", true},
		{"mixed case", "Meta-Llama/Llama-7B", "meta", true},
		
		// Partial matches
		{"partial match beginning", "meta-llama/model", "meta", true},
		{"partial match middle", "meta-llama/model", "llama", true},
		{"partial match end", "meta-llama/model", "model", true},
		{"partial match with slash", "meta-llama/model", "llama/mod", true},
		
		// Size patterns
		{"size match 7b", "meta-llama/llama-7b", "7b", true},
		{"size match 13b", "some-org/model-13b", "13b", true},
		{"size no match", "meta-llama/llama-7b", "13b", false},
		
		// Organization patterns
		{"org match", "mistralai/mistral-7b", "mistralai", true},
		{"org partial match", "mistralai/mistral-7b", "mistral", true},
		{"org no match", "mistralai/mistral-7b", "openai", false},
		
		// Complex patterns
		{"complex pattern", "meta-llama/CodeLlama-7b-hf", "code", true},
		{"version pattern", "model-v0.1", "v0.1", true},
		{"hyphenated pattern", "my-cool-model", "cool-model", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.modelName, tt.pattern)
			assert.Equal(t, tt.expected, result,
				"matchesPattern('%s', '%s') returned %v, expected %v",
				tt.modelName, tt.pattern, result, tt.expected)
		})
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{-1, 0, -1},
		{0, -1, -1},
		{100, 200, 100},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := min(tt.a, tt.b)
			assert.Equal(t, tt.expected, result,
				"min(%d, %d) = %d, expected %d",
				tt.a, tt.b, result, tt.expected)
		})
	}
}

func TestTagExtractionEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		shouldHaveTag string
		shouldNotHaveTag string
	}{
		{
			name:          "all parts included",
			input:         "a/b-c-model",
			shouldHaveTag: "model",
			// All parts are now included, even short ones
		},
		{
			name:          "numbers in name",
			input:         "model123/test456",
			shouldHaveTag: "model123",
		},
		{
			name:          "underscore in name",
			input:         "my_model/test_file",
			shouldHaveTag: "test_file",
		},
		{
			name:          "dots in name",
			input:         "model.v2/test.model",
			shouldHaveTag: "test.model",
		},
		{
			name:          "special characters",
			input:         "model@org/test#1",
			shouldHaveTag: "test#1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := extractTags(tt.input)
			
			if tt.shouldHaveTag != "" {
				// Check for tags that should be present
				found := false
				for _, tag := range tags {
					if tag == tt.shouldHaveTag {
						found = true
						break
					}
				}
				assert.True(t, found, 
					"Expected to find tag '%s' in tags for '%s', got %v",
					tt.shouldHaveTag, tt.input, tags)
			}
			
			if tt.shouldNotHaveTag != "" {
				// Check for tags that should NOT be present
				for _, tag := range tags {
					assert.NotEqual(t, tt.shouldNotHaveTag, tag,
						"Did not expect tag '%s' in tags for '%s'",
						tt.shouldNotHaveTag, tt.input)
				}
			}
		})
	}
}

func TestWellKnownSeed(t *testing.T) {
	// Ensure the well-known seed is consistent
	assert.Equal(t, "silmaril-discovery-v1", WellKnownSeed)
	
	// Ensure max value size is correct for BEP44
	assert.Equal(t, 1000, MaxValueSize)
}

func TestModelCatalogStructure(t *testing.T) {
	catalog := &ModelCatalog{
		Version:  1,
		Sequence: 5,
		Updated:  1234567890,
		Models:   make(map[string]ModelEntry),
	}

	// Add a model entry
	catalog.Models["test/model"] = ModelEntry{
		InfoHash: "abc123",
		Size:     1000000,
		Tags:     []string{"test", "model"},
		Added:    1234567890,
	}

	// Verify structure
	assert.Equal(t, 1, catalog.Version)
	assert.Equal(t, int64(5), catalog.Sequence)
	assert.Equal(t, 1, len(catalog.Models))
	
	entry := catalog.Models["test/model"]
	assert.Equal(t, "abc123", entry.InfoHash)
	assert.Equal(t, int64(1000000), entry.Size)
	assert.Contains(t, entry.Tags, "test")
	assert.Contains(t, entry.Tags, "model")
}