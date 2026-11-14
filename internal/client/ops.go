package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// The calls below are what the API has long offered for observing and tuning a
// running deployment; the CLI had no way to make them.

// DeploymentEvent is one entry of a deployment's operational history: a start,
// a stop, a restart, a resize, with its outcome.
type DeploymentEvent struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail"`
	VCPU      *int   `json:"vcpu"`
	MemoryMB  *int   `json:"memory_mb"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
}

// DeploymentEvents is one page of events, newest first.
type DeploymentEvents struct {
	Events []DeploymentEvent `json:"events"`
	Total  int64             `json:"total"`
}

// ListDeploymentEvents reads up to limit events of a deployment.
func (c *Client) ListDeploymentEvents(ctx context.Context, projectID, app string, limit int) (*DeploymentEvents, error) {
	var out DeploymentEvents
	path := "/api/v1/projects/" + projectID + "/deployments/" + app + "/events"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	return &out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// MetricPoint is one sample: a Unix time and a value.
type MetricPoint struct {
	T int64   `json:"T"`
	V float64 `json:"V"`
}

// MetricSeries is one measured quantity over the asked range.
type MetricSeries struct {
	Name   string        `json:"name"`
	Unit   string        `json:"unit"`
	Points []MetricPoint `json:"points"`
}

// DeploymentMetrics is a deployment's usage over a range.
type DeploymentMetrics struct {
	Allocations int            `json:"allocations"`
	Range       string         `json:"range"`
	Series      []MetricSeries `json:"series"`
	StepSeconds int            `json:"step_seconds"`
}

// GetDeploymentMetrics reads CPU, memory and network usage over rangeKey
// (for example 1h); empty lets the server choose.
func (c *Client) GetDeploymentMetrics(ctx context.Context, projectID, app, rangeKey string) (*DeploymentMetrics, error) {
	var out DeploymentMetrics
	path := "/api/v1/projects/" + projectID + "/deployments/" + app + "/metrics"
	if rangeKey != "" {
		path += "?range=" + url.QueryEscape(rangeKey)
	}
	return &out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// DeploymentAlert is a rule that notifies when a deployment's usage stays past a
// threshold, or when it has no healthy instance.
type DeploymentAlert struct {
	ID            string  `json:"id"`
	Metric        string  `json:"metric"`
	Unit          string  `json:"unit"`
	Threshold     float64 `json:"threshold"`
	ForSeconds    int32   `json:"for_seconds"`
	Enabled       bool    `json:"enabled"`
	State         string  `json:"state"`
	BreachedSince string  `json:"breached_since,omitempty"`
}

// DeploymentAlertRequest is the body for PUT .../alerts.
type DeploymentAlertRequest struct {
	Metric     string  `json:"metric"`
	Threshold  float64 `json:"threshold"`
	ForSeconds *int32  `json:"for_seconds,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

func alertsPath(projectID, app string) string {
	return "/api/v1/projects/" + projectID + "/deployments/" + app + "/alerts"
}

// ListDeploymentAlerts reads a deployment's alert rules.
func (c *Client) ListDeploymentAlerts(ctx context.Context, projectID, app string) ([]DeploymentAlert, error) {
	var out struct {
		Alerts []DeploymentAlert `json:"alerts"`
	}
	if err := c.do(ctx, http.MethodGet, alertsPath(projectID, app), nil, &out); err != nil {
		return nil, err
	}
	return out.Alerts, nil
}

// PutDeploymentAlert creates or updates the rule for one metric.
func (c *Client) PutDeploymentAlert(ctx context.Context, projectID, app string, req DeploymentAlertRequest) (*DeploymentAlert, error) {
	var out DeploymentAlert
	return &out, c.do(ctx, http.MethodPut, alertsPath(projectID, app), req, &out)
}

// DeleteDeploymentAlert removes a rule.
func (c *Client) DeleteDeploymentAlert(ctx context.Context, projectID, app, alertID string) error {
	return c.do(ctx, http.MethodDelete, alertsPath(projectID, app)+"/"+alertID, nil, nil)
}

// SetDeploymentEnv replaces a deployment's whole environment and redeploys it.
// Values may be "secret:KEY" references. The server never returns secret values.
func (c *Client) SetDeploymentEnv(ctx context.Context, projectID, app string, env map[string]string) error {
	body := map[string]map[string]string{"env": env}
	return c.do(ctx, http.MethodPost, "/api/v1/projects/"+projectID+"/deployments/"+app+"/env", body, nil)
}

// ResizeVolume grows a volume to sizeGB; a volume cannot be made smaller.
func (c *Client) ResizeVolume(ctx context.Context, projectID, volumeID string, sizeGB int) error {
	body := map[string]int{"size_gb": sizeGB}
	return c.do(ctx, http.MethodPost, "/api/v1/projects/"+projectID+"/volumes/"+volumeID+"/resize", body, nil)
}

// AuditEntry is one recorded action in the organisation.
type AuditEntry struct {
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// AuditLog is one page of the organisation's audit log, newest first.
type AuditLog struct {
	Entries []AuditEntry `json:"entries"`
	Total   int64        `json:"total"`
}

// ListAudit reads up to limit entries of the organisation's audit log, starting
// offset entries from the newest.
func (c *Client) ListAudit(ctx context.Context, limit, offset int) (*AuditLog, error) {
	var out AuditLog
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	path := "/api/v1/audit"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return &out, c.do(ctx, http.MethodGet, path, nil, &out)
}
