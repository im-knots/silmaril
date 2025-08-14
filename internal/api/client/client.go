package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Health checks if the daemon is healthy
func (c *Client) Health() error {
	resp, err := c.get("/api/v1/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon unhealthy: status %d", resp.StatusCode)
	}
	
	return nil
}

// GetStatus returns the daemon status
func (c *Client) GetStatus() (map[string]interface{}, error) {
	resp, err := c.get("/api/v1/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	
	return status, nil
}

// Shutdown requests daemon shutdown
func (c *Client) Shutdown() error {
	resp, err := c.post("/api/v1/admin/shutdown", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown failed: status %d", resp.StatusCode)
	}
	
	return nil
}

// ListModels returns all local models
func (c *Client) ListModels() ([]map[string]interface{}, error) {
	resp, err := c.get("/api/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		Models []map[string]interface{} `json:"models"`
		Count  int                      `json:"count"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result.Models, nil
}

// GetModel returns details about a specific model
func (c *Client) GetModel(name string) (map[string]interface{}, error) {
	resp, err := c.get(fmt.Sprintf("/api/v1/models/%s", name))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("model not found: %s", name)
	}
	
	var model map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, err
	}
	
	return model, nil
}

// DownloadModel starts downloading a model
func (c *Client) DownloadModel(modelName, infoHash string, seed bool) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"model_name": modelName,
		"info_hash":  infoHash,
		"seed":       seed,
	}
	
	resp, err := c.post("/api/v1/models/download", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result, nil
}

// ShareModelOptions contains options for sharing a model
type ShareModelOptions struct {
	ModelName    string
	Path         string
	All          bool
	Name         string // For publishing new models
	License      string
	Version      string
	PieceLength  int64
	SkipDHT      bool
	SignManifest bool
	// Repository cloning options
	RepoURL      string
	Branch       string
	Depth        int
	SkipLFS      bool
}

// ShareModel starts sharing a model
func (c *Client) ShareModel(opts ShareModelOptions) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"model_name":    opts.ModelName,
		"path":          opts.Path,
		"all":           opts.All,
		"name":          opts.Name,
		"license":       opts.License,
		"version":       opts.Version,
		"piece_length":  opts.PieceLength,
		"skip_dht":      opts.SkipDHT,
		"sign_manifest": opts.SignManifest,
		// Repository cloning fields
		"repo_url":      opts.RepoURL,
		"branch":        opts.Branch,
		"depth":         opts.Depth,
		"skip_lfs":      opts.SkipLFS,
	}
	
	resp, err := c.post("/api/v1/models/share", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	// Check if response contains an error
	// Accept both 200 (OK) and 202 (Accepted) as success
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		if _, ok := result["error"].(string); ok {
			return result, nil // Return the result so caller can check the error
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	
	return result, nil
}

// RemoveModel removes a model
func (c *Client) RemoveModel(name string) error {
	resp, err := c.delete(fmt.Sprintf("/api/v1/models/%s", name))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to remove model: status %d", resp.StatusCode)
	}
	
	return nil
}

// DiscoverModels searches for models on the P2P network
func (c *Client) DiscoverModels(pattern string) ([]map[string]interface{}, error) {
	url := "/api/v1/discover"
	if pattern != "" {
		url = fmt.Sprintf("%s?pattern=%s", url, pattern)
	}
	
	resp, err := c.get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		Models []map[string]interface{} `json:"models"`
		Count  int                      `json:"count"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result.Models, nil
}

// GetTransfer returns details about a specific transfer
func (c *Client) GetTransfer(id string) (map[string]interface{}, error) {
	resp, err := c.get(fmt.Sprintf("/api/v1/transfers/%s", id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("transfer not found: %s", id)
	}
	
	var transfer map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&transfer); err != nil {
		return nil, err
	}
	
	return transfer, nil
}

// ListTransfers returns all transfers
func (c *Client) ListTransfers(status string) ([]map[string]interface{}, error) {
	url := "/api/v1/transfers"
	if status != "" {
		url = fmt.Sprintf("%s?status=%s", url, status)
	}
	
	resp, err := c.get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		Transfers []map[string]interface{} `json:"transfers"`
		Count     int                      `json:"count"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result.Transfers, nil
}

// PauseTransfer pauses a transfer
func (c *Client) PauseTransfer(id string) error {
	resp, err := c.put(fmt.Sprintf("/api/v1/transfers/%s/pause", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to pause transfer: status %d", resp.StatusCode)
	}
	
	return nil
}

// ResumeTransfer resumes a transfer
func (c *Client) ResumeTransfer(id string) error {
	resp, err := c.put(fmt.Sprintf("/api/v1/transfers/%s/resume", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to resume transfer: status %d", resp.StatusCode)
	}
	
	return nil
}

// CancelTransfer cancels a transfer
func (c *Client) CancelTransfer(id string) error {
	resp, err := c.delete(fmt.Sprintf("/api/v1/transfers/%s", id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to cancel transfer: status %d", resp.StatusCode)
	}
	
	return nil
}

// AutoStartDaemon attempts to start the daemon if it's not running
func (c *Client) AutoStartDaemon() error {
	// Check if daemon is running
	if err := c.Health(); err == nil {
		return nil // Already running
	}
	
	// TODO: Implement daemon auto-start
	// This would require executing the daemon start command
	
	return fmt.Errorf("daemon is not running, please start it with: silmaril daemon start")
}

// HTTP helper methods

func (c *Client) get(path string) (*http.Response, error) {
	return c.httpClient.Get(c.baseURL + path)
}

func (c *Client) post(path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(data)
	}
	
	req, err := http.NewRequest("POST", c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *Client) put(path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(data)
	}
	
	req, err := http.NewRequest("PUT", c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *Client) delete(path string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	
	return c.httpClient.Do(req)
}