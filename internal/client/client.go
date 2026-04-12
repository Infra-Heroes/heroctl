// Package client provides a typed HTTP client for the hero-api.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
)

// Client is a typed HTTP client for the hero-api.
type Client struct {
	baseURL    string
	authDomain string
	clientID   string
	httpClient *http.Client
	token      *auth.Token
}

// New creates a Client with the given server URL, auth domain, and token.
func New(serverURL, authDomain, clientID string, tok *auth.Token) *Client {
	return &Client{
		baseURL:    serverURL,
		authDomain: authDomain,
		clientID:   clientID,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		token:      tok,
	}
}

// Org represents a hero-api organisation.
// Field names match the PascalCase JSON produced by sqlc's Org struct (no json tags).
type Org struct {
	ID        int64  `json:"ID"`
	ZitadelID string `json:"ZitadelID"`
	Name      string `json:"Name"`
	VmCap     int32  `json:"VmCap"`
	CreatedAt string `json:"CreatedAt"`
}

// Credits is the response from GET /api/v1/orgs/{id}/credits.
type Credits struct {
	Credits      float64 `json:"credits"`
	MilliCredits int64   `json:"milli_credits"`
}

// Project represents a hero-api project.
// Field names match the lowercase JSON produced by the custom projectResp struct.
type Project struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	Name      string `json:"name"`
	VNI       int    `json:"vni"`
	CreatedAt string `json:"created_at"`
}

// Deployment represents a deployment returned from list/get endpoints.
// Field names match the PascalCase JSON from dbsqlc.Deployment (no json tags).
type Deployment struct {
	ID        int64  `json:"ID"`
	ProjectID int64  `json:"ProjectID"`
	Image     string `json:"Image"`
	Status    string `json:"Status"`
	CPU       int32  `json:"Cpu"`
	MemoryMB  int32  `json:"MemoryMb"`
	Port      int32  `json:"Port"`
	CreatedAt string `json:"CreatedAt"`
}

// DeploymentCreated is the response from POST /api/v1/projects/{id}/deployments.
// Uses lowercase snake_case keys (returned as a map[string]any from the handler).
type DeploymentCreated struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"project_id"`
	Image      string `json:"image"`
	Status     string `json:"status"`
	NomadJobID string `json:"nomad_job_id"`
	CPU        int64  `json:"cpu"`
	MemoryMB   int64  `json:"memory_mb"`
	Port       int64  `json:"port"`
	AppName    string `json:"app_name"`
	Hostname   string `json:"hostname"`
}

// CreateDeploymentRequest is the body for POST /api/v1/projects/{id}/deployments.
type CreateDeploymentRequest struct {
	AppName    string            `json:"app_name"`
	Image      string            `json:"image"`
	CPU        int               `json:"cpu"`
	MemoryMB   int               `json:"memory_mb"`
	Port       int               `json:"port"`
	Env        map[string]string `json:"env"`
	HealthPath string            `json:"health_path"`
}

// DeploymentStatus is the response from GET /api/v1/projects/{id}/deployments/{id}/status.
type DeploymentStatus struct {
	NomadStatus string `json:"nomad_status"`
}

// Secret represents a secret key (value is never returned by the API).
type Secret struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	CreatedAt string `json:"created_at"`
}

// RegistryCreds holds the short-lived credentials for docker login.
type RegistryCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SignupRequest is the body for POST /api/v1/signup.
type SignupRequest struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	OrgName     string `json:"org_name"`
	ProjectName string `json:"project_name,omitempty"`
}

// SignupResponse is the response from POST /api/v1/signup.
type SignupResponse struct {
	Org     Org     `json:"org"`
	Project Project `json:"project"`
}

// Signup creates a new account without requiring authentication.
func (c *Client) Signup(ctx context.Context, req SignupRequest) (*SignupResponse, error) {
	var out SignupResponse
	return &out, c.doOpen(ctx, http.MethodPost, "/api/v1/signup", req, &out)
}

// GetOrg fetches the authenticated user's org.
// The URL path parameter is ignored by the server (it uses JWT claims), so we
// send "me" as a conventional placeholder.
func (c *Client) GetOrg(ctx context.Context) (*Org, error) {
	var out Org
	return &out, c.do(ctx, http.MethodGet, "/api/v1/orgs/me", nil, &out)
}

// CreateOrg creates (or returns the existing) org for the authenticated user.
func (c *Client) CreateOrg(ctx context.Context, name string) (*Org, error) {
	var out Org
	return &out, c.do(ctx, http.MethodPost, "/api/v1/orgs", map[string]string{"name": name}, &out)
}

// GetCredits returns the current credit balance for the given internal org ID.
func (c *Client) GetCredits(ctx context.Context, orgID int64) (*Credits, error) {
	var out Credits
	return &out, c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/orgs/%d/credits", orgID), nil, &out)
}

// ListProjects returns all projects for the authenticated org.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	if err := c.do(ctx, http.MethodGet, "/api/v1/projects", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Project{}
	}
	return out, nil
}

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, name string) (*Project, error) {
	var out Project
	return &out, c.do(ctx, http.MethodPost, "/api/v1/projects", map[string]string{"name": name}, &out)
}

// DeleteProject deletes a project by its internal ID.
func (c *Client) DeleteProject(ctx context.Context, projectID int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/projects/%d", projectID), nil, nil)
}

// ListDeployments returns all deployments for a project.
func (c *Client) ListDeployments(ctx context.Context, projectID int64) ([]Deployment, error) {
	var out []Deployment
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d/deployments", projectID), nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Deployment{}
	}
	return out, nil
}

// GetDeployment fetches a single deployment.
func (c *Client) GetDeployment(ctx context.Context, projectID, deploymentID int64) (*Deployment, error) {
	var out Deployment
	return &out, c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%d/deployments/%d", projectID, deploymentID),
		nil, &out)
}

// GetDeploymentStatus returns the live Nomad job status for a deployment.
// Possible values: "pending", "running", "dead".
func (c *Client) GetDeploymentStatus(ctx context.Context, projectID, deploymentID int64) (string, error) {
	var out DeploymentStatus
	if err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%d/deployments/%d/status", projectID, deploymentID),
		nil, &out); err != nil {
		return "", err
	}
	return out.NomadStatus, nil
}

// CreateDeployment submits a new deployment.
func (c *Client) CreateDeployment(ctx context.Context, projectID int64, req CreateDeploymentRequest) (*DeploymentCreated, error) {
	var out DeploymentCreated
	return &out, c.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%d/deployments", projectID),
		req, &out)
}

// StopDeployment stops a running deployment.
func (c *Client) StopDeployment(ctx context.Context, projectID, deploymentID int64) error {
	return c.do(ctx, http.MethodDelete,
		fmt.Sprintf("/api/v1/projects/%d/deployments/%d", projectID, deploymentID),
		nil, nil)
}

// SetSecret creates or updates a secret.
func (c *Client) SetSecret(ctx context.Context, key, value string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/secrets",
		map[string]string{"key": key, "value": value}, nil)
}

// ListSecrets returns all secret keys for the authenticated org.
func (c *Client) ListSecrets(ctx context.Context) ([]Secret, error) {
	var out []Secret
	if err := c.do(ctx, http.MethodGet, "/api/v1/secrets", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Secret{}
	}
	return out, nil
}

// DeleteSecret removes a secret by key.
func (c *Client) DeleteSecret(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/secrets/"+key, nil, nil)
}

// RegistryCredentials obtains short-lived docker registry credentials.
func (c *Client) RegistryCredentials(ctx context.Context) (*RegistryCreds, error) {
	var out RegistryCreds
	return &out, c.do(ctx, http.MethodPost, "/api/v1/registry/credentials", nil, &out)
}

// do performs an authenticated HTTP request, refreshing the token if needed.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doOpen performs an unauthenticated HTTP request (no token required).
func (c *Client) doOpen(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ensureToken refreshes the access token if it has expired.
func (c *Client) ensureToken(ctx context.Context) error {
	if !c.token.IsExpired() {
		return nil
	}
	newTok, err := auth.Refresh(ctx, c.authDomain, c.clientID, c.token.RefreshToken)
	if err != nil {
		return fmt.Errorf("token refresh failed (%w) — run 'heroctl login' again", err)
	}
	if err := auth.Save(newTok); err != nil {
		return fmt.Errorf("save refreshed token: %w", err)
	}
	c.token = newTok
	return nil
}
