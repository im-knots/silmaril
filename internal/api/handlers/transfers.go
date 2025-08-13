package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/silmaril/silmaril/internal/daemon"
)

// ListTransfers returns all transfers
func (h *Handlers) ListTransfers(c *gin.Context) {
	tm := h.daemon.GetTransferManager()
	
	// Filter by status if provided
	status := c.Query("status")
	var transfers []*daemon.Transfer
	
	if status == "active" {
		transfers = tm.GetActiveTransfers()
	} else {
		transfers = tm.GetAllTransfers()
	}
	
	c.JSON(http.StatusOK, gin.H{
		"transfers": transfers,
		"count":     len(transfers),
	})
}

// GetTransfer returns details about a specific transfer
func (h *Handlers) GetTransfer(c *gin.Context) {
	transferID := c.Param("id")
	
	tm := h.daemon.GetTransferManager()
	transfer, exists := tm.GetTransfer(transferID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("transfer %s not found", transferID),
		})
		return
	}
	
	// Update stats before returning
	tm.UpdateStats()
	
	c.JSON(http.StatusOK, transfer)
}

// PauseTransfer pauses an active transfer
func (h *Handlers) PauseTransfer(c *gin.Context) {
	transferID := c.Param("id")
	
	tm := h.daemon.GetTransferManager()
	if err := tm.PauseTransfer(transferID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to pause transfer: %v", err),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":     "transfer paused",
		"transfer_id": transferID,
	})
}

// ResumeTransfer resumes a paused transfer
func (h *Handlers) ResumeTransfer(c *gin.Context) {
	transferID := c.Param("id")
	
	tm := h.daemon.GetTransferManager()
	if err := tm.ResumeTransfer(transferID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to resume transfer: %v", err),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":     "transfer resumed",
		"transfer_id": transferID,
	})
}

// CancelTransfer cancels and removes a transfer
func (h *Handlers) CancelTransfer(c *gin.Context) {
	transferID := c.Param("id")
	
	tm := h.daemon.GetTransferManager()
	if err := tm.CancelTransfer(transferID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to cancel transfer: %v", err),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":     "transfer cancelled",
		"transfer_id": transferID,
	})
}