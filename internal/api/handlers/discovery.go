package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// DiscoverModels searches for models on the P2P network
func (h *Handlers) DiscoverModels(c *gin.Context) {
	pattern := c.Query("pattern")
	if pattern == "" {
		pattern = "*" // Search for all models
	}
	
	// Search via DHT
	results, err := h.daemon.GetDHTManager().DiscoverModels(pattern)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to discover models: %v", err),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"models":  results,
		"count":   len(results),
		"pattern": pattern,
	})
}