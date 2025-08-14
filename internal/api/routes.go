package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/silmaril/silmaril/internal/api/handlers"
	"github.com/silmaril/silmaril/internal/daemon"
)

func SetupRoutes(d *daemon.Daemon) *gin.Engine {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	router := gin.New()
	
	// Add middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())
	
	// Create handlers
	h := handlers.NewHandlers(d)
	
	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Health and status endpoints
		v1.GET("/health", h.Health)
		v1.GET("/status", h.Status)
		
		// Debug test
		v1.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "v1 POST test works"})
		})
		
		// Model endpoints
		models := v1.Group("/models")
		{
			models.GET("", h.ListModels)
			models.GET("/:name", h.GetModel)
			models.POST("/download", h.DownloadModel)
			models.POST("/share", h.ShareModel)
			models.POST("/mirror", h.MirrorModel)
			models.DELETE("/:name", h.RemoveModel)
			
			// Debug endpoint
			models.POST("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "POST test works"})
			})
		}
		
		// Discovery endpoints
		v1.GET("/discover", h.DiscoverModels)
		
		// Transfer endpoints
		transfers := v1.Group("/transfers")
		{
			transfers.GET("", h.ListTransfers)
			transfers.GET("/:id", h.GetTransfer)
			transfers.PUT("/:id/pause", h.PauseTransfer)
			transfers.PUT("/:id/resume", h.ResumeTransfer)
			transfers.DELETE("/:id", h.CancelTransfer)
		}
		
		// Admin endpoints
		admin := v1.Group("/admin")
		{
			admin.POST("/shutdown", h.Shutdown)
		}
	}
	
	// Catch-all for undefined routes
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "endpoint not found",
			"path":  c.Request.URL.Path,
		})
	})
	
	return router
}

// corsMiddleware adds CORS headers for local development
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		
		c.Next()
	}
}