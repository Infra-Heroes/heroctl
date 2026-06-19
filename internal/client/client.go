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
	baseURL      string
	authDomain   string
	clientID     string
	httpClient   *http.Client
	streamClient *http.Client
	token        *auth.Token
}

// New creates a Client with the given server URL, auth domain, and token.
func New(serverURL, authDomain, clientID string, tok *auth.Token) *Client {
	return &Client{
		baseURL:      serverURL,
		authDomain:   authDomain,
		clientID:     clientID,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		streamClient: &http.Client{},
		token:        tok,
	}
}

// Org represents a hero-api organisation.
type Org struct {
	ID          string `json:"ID"`
	ZitadelID   string `json:"ZitadelID"`
	Name        string `json:"Name"`
	VmCap       int32  `json:"VmCap"`
	RunningVMs  int64  `json:"RunningVMs"`
	MaxProjects int32  `json:"MaxProjects"`
	CreatedAt   string `json:"CreatedAt"`
}

// Credits is the response from GET /api/v1/orgs/{id}/credits.
type Credits struct {
	Credits      float64 `json:"credits"`
	MilliCredits int64   `json:"milli_credits"`
}

// Project represents a hero-api project.
// ID is the UUID string — the canonical project identifier.
// VNI is the VXLAN network identifier (separate from project identity).
type Project struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Name      string `json:"name"`
	VNI       int    `json:"vni"`
	CreatedAt string `json:"created_at"`
}

// Deployment represents a deployment returned from list/get endpoints.
// ID is the UUID — the canonical deployment identifier (not shown to users).
// Users interact with deployments by AppName within a project.
type Deployment struct {
	ID           string            `json:"ID"`
	ProjectID    string            `json:"ProjectID"`
	AppName      string            `json:"AppName"`
	Image        string            `json:"Image"`
	Status       string            `json:"Status"`
	CPU          int32             `json:"Cpu"`
	MemoryMB     int32             `json:"MemoryMb"`
	Port         int32             `json:"Port"`
	Hostname     string            `json:"Hostname"`
	ServiceScope string            `json:"ServiceScope"`
	Labels       map[string]string `json:"Labels,omitempty"`
	CreatedAt    string            `json:"CreatedAt"`
	MinReplicas  int32             `json:"MinReplicas"`
	MaxReplicas  int32             `json:"MaxReplicas"`
	Replicas     int32             `json:"Replicas"`
}

// DeploymentCreated is the response from POST /api/v1/projects/{id}/deployments.
type DeploymentCreated struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	AppName     string            `json:"app_name"`
	Image       string            `json:"image"`
	Status      string            `json:"status"`
	NomadJobID  string            `json:"nomad_job_id"`
	CPU         int64             `json:"cpu"`
	MemoryMB    int64             `json:"memory_mb"`
	Port        int64             `json:"port"`
	Hostname    string            `json:"hostname"`
	Labels      map[string]string `json:"labels,omitempty"`
	MinReplicas int64             `json:"min_replicas"`
	MaxReplicas int64             `json:"max_replicas"`
	Replicas    int64             `json:"replicas"`
}

// VolumeAttachment describes a volume to attach to a deployment.
type VolumeAttachment struct {
	VolumeID  string `json:"volume_id"`
	MountPath string `json:"mount_path"`
}

// CreateDeploymentRequest is the body for POST /api/v1/projects/{id}/deployments.
type CreateDeploymentRequest struct {
	AppName         string             `json:"app_name"`
	Image           string             `json:"image"`
	CPU             int                `json:"cpu"`
	MemoryMB        int                `json:"memory_mb"`
	Port            int                `json:"port"`
	Env             map[string]string  `json:"env"`
	Labels          map[string]string  `json:"labels,omitempty"`
	HealthPath      string             `json:"health_path"`
	ScaleToZero     bool               `json:"scale_to_zero"`
	ServiceScope    string             `json:"service_scope,omitempty"`
	HealthCheckType string             `json:"health_check_type,omitempty"`
	HealthCheckPort int                `json:"health_check_port,omitempty"`
	Volumes         []VolumeAttachment `json:"volumes,omitempty"`
	MinReplicas     int                `json:"min_replicas"`
	MaxReplicas     int                `json:"max_replicas"`
}

// DeploymentStatus is the response from GET /api/v1/projects/{id}/deployments/{id}/status.
type DeploymentStatus struct {
	NomadStatus string `json:"nomad_status"`
	Healthy     bool   `json:"healthy"`
}

// Secret represents a secret key (value is never returned by the API).
type Secret struct {
	ID        string `json:"id"`
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
	InviteCode  string `json:"invite_code"`
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
func (c *Client) GetCredits(ctx context.Context, orgID string) (*Credits, error) {
	var out Credits
	return &out, c.do(ctx, http.MethodGet, "/api/v1/orgs/"+orgID+"/credits", nil, &out)
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

// DeleteProject deletes a project by its UUID.
func (c *Client) DeleteProject(ctx context.Context, projectID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/projects/"+projectID, nil, nil)
}

// ListDeployments returns all deployments for a project.
func (c *Client) ListDeployments(ctx context.Context, projectID string) ([]Deployment, error) {
	var out []Deployment
	if err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/deployments", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Deployment{}
	}
	return out, nil
}

// GetDeployment fetches the most recent deployment for the named app within a project.
func (c *Client) GetDeployment(ctx context.Context, projectID, appName string) (*Deployment, error) {
	var out Deployment
	return &out, c.do(ctx, http.MethodGet,
		"/api/v1/projects/"+projectID+"/deployments/"+appName,
		nil, &out)
}

// GetDeploymentStatus returns the live status for the active deployment of an app.
func (c *Client) GetDeploymentStatus(ctx context.Context, projectID, appName string) (*DeploymentStatus, error) {
	var out DeploymentStatus
	if err := c.do(ctx, http.MethodGet,
		"/api/v1/projects/"+projectID+"/deployments/"+appName+"/status",
		nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDeployment submits a new deployment.
func (c *Client) CreateDeployment(ctx context.Context, projectID string, req CreateDeploymentRequest) (*DeploymentCreated, error) {
	var out DeploymentCreated
	return &out, c.do(ctx, http.MethodPost,
		"/api/v1/projects/"+projectID+"/deployments",
		req, &out)
}

// StopDeployment stops the active deployment for the named app within a project.
func (c *Client) StopDeployment(ctx context.Context, projectID, appName string) error {
	return c.do(ctx, http.MethodPost,
		"/api/v1/projects/"+projectID+"/deployments/"+appName+"/stop",
		nil, nil)
}

// DeleteDeployment hard-purges a deployment (stops if running, then deletes the DB row).
func (c *Client) DeleteDeployment(ctx context.Context, projectID, appName string) error {
	return c.do(ctx, http.MethodDelete,
		"/api/v1/projects/"+projectID+"/deployments/"+appName,
		nil, nil)
}

// StartDeployment brings a stopped or failed deployment back online.
func (c *Client) StartDeployment(ctx context.Context, projectID, appName string) (*DeploymentCreated, error) {
	var out DeploymentCreated
	return &out, c.do(ctx, http.MethodPost,
		"/api/v1/projects/"+projectID+"/deployments/"+appName+"/start",
		nil, &out)
}

// RestartDeployment stops a running deployment and immediately re-submits it.
func (c *Client) RestartDeployment(ctx context.Context, projectID, appName string) (*DeploymentCreated, error) {
	var out DeploymentCreated
	return &out, c.do(ctx, http.MethodPost,
		"/api/v1/projects/"+projectID+"/deployments/"+appName+"/restart",
		nil, &out)
}

// SetSecret creates or updates a secret for a project.
func (c *Client) SetSecret(ctx context.Context, projectID, key, value string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/projects/"+projectID+"/secrets",
		map[string]string{"key": key, "value": value}, nil)
}

// ListSecrets returns all secret keys for a project (values are never returned).
func (c *Client) ListSecrets(ctx context.Context, projectID string) ([]Secret, error) {
	var out []Secret
	if err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/secrets", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Secret{}
	}
	return out, nil
}

// DeleteSecret removes a secret by key from a project.
func (c *Client) DeleteSecret(ctx context.Context, projectID, key string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/projects/"+projectID+"/secrets/"+key, nil, nil)
}

// RegistryCredentials obtains short-lived docker registry credentials.
func (c *Client) RegistryCredentials(ctx context.Context) (*RegistryCreds, error) {
	var out RegistryCreds
	return &out, c.do(ctx, http.MethodPost, "/api/v1/registry/credentials", nil, &out)
}

// PAT represents a personal access token metadata.
type PAT struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// PATCreated represents a newly created PAT, containing the raw token.
type PATCreated struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

// CreatePAT creates a new Personal Access Token.
func (c *Client) CreatePAT(ctx context.Context, name, scope string) (*PATCreated, error) {
	var out PATCreated
	return &out, c.do(ctx, http.MethodPost, "/api/v1/tokens",
		map[string]string{"name": name, "scope": scope}, &out)
}

// ListPATs returns all Personal Access Tokens for the user.
func (c *Client) ListPATs(ctx context.Context) ([]PAT, error) {
	var out []PAT
	if err := c.do(ctx, http.MethodGet, "/api/v1/tokens", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []PAT{}
	}
	return out, nil
}

// DeletePAT deletes/revokes a Personal Access Token by ID.
func (c *Client) DeletePAT(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/tokens/"+id, nil, nil)
}


// ── Volumes ───────────────────────────────────────────────────────────────────

// Volume represents a Ceph RBD volume owned by a project.
type Volume struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	SizeGB               int     `json:"size_gb"`
	Status               string  `json:"status"`
	AttachedDeploymentID *string `json:"attached_deployment_id"`
	CreatedAt            string  `json:"created_at"`
}

// CreateVolumeRequest is the body for POST /api/v1/projects/{id}/volumes.
type CreateVolumeRequest struct {
	Name   string `json:"name"`
	SizeGB int    `json:"size_gb"`
}

// CreateVolume creates a new volume in the given project.
func (c *Client) CreateVolume(ctx context.Context, projectID string, req CreateVolumeRequest) (*Volume, error) {
	var out Volume
	return &out, c.do(ctx, http.MethodPost, "/api/v1/projects/"+projectID+"/volumes", req, &out)
}

// ListVolumes returns all volumes for a project.
func (c *Client) ListVolumes(ctx context.Context, projectID string) ([]Volume, error) {
	var out []Volume
	if err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/volumes", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Volume{}
	}
	return out, nil
}

// DeleteVolume permanently deletes a volume by its UUID.
func (c *Client) DeleteVolume(ctx context.Context, projectID, volumeID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/projects/"+projectID+"/volumes/"+volumeID, nil, nil)
}

// ── Members ───────────────────────────────────────────────────────────────────

// OrgMember is a member of an org as returned by the API.
type OrgMember struct {
	ID            string `json:"id"`
	PrincipalID   string `json:"principal_id"`
	PrincipalType string `json:"principal_type"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	CreatedAt     string `json:"created_at"`
}

// ProjectMember is a project-level member as returned by the API.
type ProjectMember struct {
	ID            string `json:"id"`
	PrincipalID   string `json:"principal_id"`
	PrincipalType string `json:"principal_type"`
	Role          string `json:"role"`
	CreatedAt     string `json:"created_at"`
}

// Invitation is a pending org membership invitation.
type Invitation struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	OrgRole   string `json:"org_role"`
	InvitedBy string `json:"invited_by"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// ProjectRoleEntry is one element of project_roles in an invitation request.
type ProjectRoleEntry struct {
	ProjectID string `json:"project_id"`
	Role      string `json:"role"`
}

// CreateInvitationRequest is the body for POST /api/v1/invitations.
type CreateInvitationRequest struct {
	Email        string             `json:"email"`
	OrgRole      string             `json:"org_role"`
	ProjectRoles []ProjectRoleEntry `json:"project_roles"`
}

// InvitationCreated is the response from POST /api/v1/invitations.
type InvitationCreated struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Email     string `json:"email"`
	OrgRole   string `json:"org_role"`
	ExpiresAt string `json:"expires_at"`
}

// ListOrgMembers returns all members of the authenticated org.
func (c *Client) ListOrgMembers(ctx context.Context) ([]OrgMember, error) {
	var out []OrgMember
	if err := c.do(ctx, http.MethodGet, "/api/v1/members", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []OrgMember{}
	}
	return out, nil
}

// UpdateMemberRole changes an org member's role.
func (c *Client) UpdateMemberRole(ctx context.Context, principalID, role string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/members/"+principalID+"/role",
		map[string]string{"role": role}, nil)
}

// RemoveOrgMember removes a member from the org.
func (c *Client) RemoveOrgMember(ctx context.Context, principalID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/members/"+principalID, nil, nil)
}

// ListProjectMembers returns all explicit project members.
func (c *Client) ListProjectMembers(ctx context.Context, projectID string) ([]ProjectMember, error) {
	var out []ProjectMember
	if err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/members", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ProjectMember{}
	}
	return out, nil
}

// UpsertProjectMember adds or updates a project-level member.
func (c *Client) UpsertProjectMember(ctx context.Context, projectID, principalID, role string) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+projectID+"/members/"+principalID,
		map[string]string{"role": role}, nil)
}

// RemoveProjectMember removes a project-level member.
func (c *Client) RemoveProjectMember(ctx context.Context, projectID, principalID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/projects/"+projectID+"/members/"+principalID, nil, nil)
}

// CreateInvitation creates a new org membership invitation.
func (c *Client) CreateInvitation(ctx context.Context, req CreateInvitationRequest) (*InvitationCreated, error) {
	var out InvitationCreated
	return &out, c.do(ctx, http.MethodPost, "/api/v1/invitations", req, &out)
}

// ListInvitations returns pending invitations for the org.
func (c *Client) ListInvitations(ctx context.Context) ([]Invitation, error) {
	var out []Invitation
	if err := c.do(ctx, http.MethodGet, "/api/v1/invitations", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Invitation{}
	}
	return out, nil
}

// RevokeInvitation revokes a pending invitation.
func (c *Client) RevokeInvitation(ctx context.Context, invitationID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/invitations/"+invitationID, nil, nil)
}

// AcceptInvitation accepts an invitation using a token. Requires authentication.
func (c *Client) AcceptInvitation(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/invitations/"+token+"/accept", nil, nil)
}

// StreamLogs opens a streaming connection to the logs endpoint and returns the
// response body. The caller must close it. follow=true tails live output.
func (c *Client) StreamLogs(ctx context.Context, projectID, appName string, follow bool) (io.ReadCloser, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/api/v1/projects/%s/deployments/%s/logs", c.baseURL, projectID, appName)
	if follow {
		path += "?follow=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("logs: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
