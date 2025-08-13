package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/silmaril/silmaril/internal/daemon"
)

type Handlers struct {
	daemon *daemon.Daemon
}

func NewHandlers(d *daemon.Daemon) *Handlers {
	return &Handlers{
		daemon: d,
	}
}

// Health endpoint for health checks
func (h *Handlers) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().Unix(),
	})
}

// Status returns daemon status information
func (h *Handlers) Status(c *gin.Context) {
	status := h.daemon.GetStatus()
	c.JSON(http.StatusOK, status)
}

// Shutdown gracefully shuts down the daemon
func (h *Handlers) Shutdown(c *gin.Context) {
	// Schedule shutdown after response
	go func() {
		time.Sleep(1 * time.Second)
		h.daemon.Shutdown()
	}()
	
	c.JSON(http.StatusOK, gin.H{
		"message": "daemon shutting down",
	})
}