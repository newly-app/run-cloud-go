// Package runcloud is the public Go client for run.cloud.
package runcloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type ClientOptions struct {
	APIKey     string
	APIURL     string
	HTTPClient *http.Client
}

type Client struct {
	apiKey     string
	apiURL     string
	httpClient *http.Client
}

type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("run.cloud API %d: %s", e.Status, e.Detail)
}

type CreateSandboxOptions struct {
	Image            string  `json:"image,omitempty"`
	CPU              float64 `json:"cpu,omitempty"`
	Memory           int     `json:"memory,omitempty"`
	Disk             int     `json:"disk,omitempty"`
	IdlePauseSeconds *int    `json:"idlePauseSeconds,omitempty"`
	TimeoutSeconds   *int    `json:"timeoutSeconds,omitempty"`
	Region           string  `json:"region,omitempty"`
	Name             string  `json:"name,omitempty"`
	OrgID            string  `json:"orgId,omitempty"`
	IdempotencyKey   string  `json:"-"`
}

type SandboxRecord struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	Name             string `json:"name"`
	Image            string `json:"image"`
	Region           string `json:"region"`
	MilliCPU         int    `json:"milli_cpu"`
	MemoryMB         int    `json:"mem_mb"`
	IdlePauseSeconds *int   `json:"idle_pause_seconds"`
	TimeoutSeconds   *int   `json:"timeout_seconds"`
	CreatedAt        string `json:"created_at"`
}

type ExecOptions struct {
	Cwd            string
	Env            map[string]string
	TimeoutSeconds int
}

type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type Sandbox struct {
	client *Client
	SandboxRecord
}

func NewClient(options ClientOptions) (*Client, error) {
	apiKey := strings.TrimSpace(options.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("RUN_CLOUD_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("RUN_CLOUD_API_TOKEN"))
	}
	if apiKey == "" {
		return nil, errors.New("Run Cloud API key required: set RUN_CLOUD_API_KEY or pass ClientOptions.APIKey")
	}
	apiURL := strings.TrimRight(strings.TrimSpace(options.APIURL), "/")
	if apiURL == "" {
		apiURL = strings.TrimRight(strings.TrimSpace(os.Getenv("RUN_CLOUD_API_URL")), "/")
	}
	if apiURL == "" {
		apiURL = "https://api.run.cloud"
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{apiKey: apiKey, apiURL: apiURL, httpClient: httpClient}, nil
}

func (c *Client) CreateSandbox(ctx context.Context, options CreateSandboxOptions) (*Sandbox, error) {
	if options.Image == "" {
		options.Image = "runcloud/agent-base"
	}
	var record SandboxRecord
	headers := map[string]string{}
	if options.IdempotencyKey != "" {
		headers["Idempotency-Key"] = options.IdempotencyKey
	}
	if err := c.request(ctx, http.MethodPost, "/run-cloud/sandboxes", options, headers, &record); err != nil {
		return nil, err
	}
	return &Sandbox{client: c, SandboxRecord: record}, nil
}

func (c *Client) GetSandbox(ctx context.Context, id string) (*Sandbox, error) {
	var record SandboxRecord
	if err := c.request(
		ctx,
		http.MethodGet,
		"/run-cloud/sandboxes/"+url.PathEscape(id),
		nil,
		nil,
		&record,
	); err != nil {
		return nil, err
	}
	return &Sandbox{client: c, SandboxRecord: record}, nil
}

func (c *Client) ListSandboxes(ctx context.Context, state string) ([]*Sandbox, error) {
	path := "/run-cloud/sandboxes"
	if state != "" {
		path += "?state=" + url.QueryEscape(state)
	}
	var payload struct {
		Items []SandboxRecord `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, path, nil, nil, &payload); err != nil {
		return nil, err
	}
	items := make([]*Sandbox, 0, len(payload.Items))
	for _, record := range payload.Items {
		items = append(items, &Sandbox{client: c, SandboxRecord: record})
	}
	return items, nil
}

func (s *Sandbox) Refresh(ctx context.Context) error {
	current, err := s.client.GetSandbox(ctx, s.ID)
	if err != nil {
		return err
	}
	s.SandboxRecord = current.SandboxRecord
	return nil
}

func (s *Sandbox) Exec(ctx context.Context, command any, options ExecOptions) (ExecResult, error) {
	var argv []string
	switch value := command.(type) {
	case string:
		argv = []string{"/bin/sh", "-c", value}
	case []string:
		argv = value
	default:
		return ExecResult{}, errors.New("command must be a string or []string")
	}
	if len(argv) == 0 {
		return ExecResult{}, errors.New("command must not be empty")
	}
	body := map[string]any{"cmd": argv}
	if options.Cwd != "" {
		body["cwd"] = options.Cwd
	}
	if len(options.Env) > 0 {
		body["env"] = options.Env
	}
	if options.TimeoutSeconds > 0 {
		body["timeout_seconds"] = options.TimeoutSeconds
	}
	var result ExecResult
	err := s.client.request(
		ctx,
		http.MethodPost,
		"/run-cloud/sandboxes/"+url.PathEscape(s.ID)+"/exec",
		body,
		nil,
		&result,
	)
	return result, err
}

func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	var payload struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if err := s.client.request(
		ctx,
		http.MethodPost,
		"/run-cloud/sandboxes/"+url.PathEscape(s.ID)+"/fs",
		map[string]string{"op": "read", "path": path},
		nil,
		&payload,
	); err != nil {
		return nil, err
	}
	if payload.Error != "" {
		return nil, errors.New(payload.Error)
	}
	return base64.StdEncoding.DecodeString(payload.Content)
}

func (s *Sandbox) Pause(ctx context.Context) error {
	return s.lifecycle(ctx, "pause")
}

func (s *Sandbox) Resume(ctx context.Context) error {
	return s.lifecycle(ctx, "resume")
}

func (s *Sandbox) SetTimeout(ctx context.Context, seconds int) error {
	var record SandboxRecord
	if err := s.client.request(
		ctx,
		http.MethodPost,
		"/run-cloud/sandboxes/"+url.PathEscape(s.ID)+"/timeout",
		map[string]int{"timeoutSeconds": seconds},
		nil,
		&record,
	); err != nil {
		return err
	}
	s.SandboxRecord = record
	return nil
}

func (s *Sandbox) Destroy(ctx context.Context) error {
	return s.client.request(
		ctx,
		http.MethodDelete,
		"/run-cloud/sandboxes/"+url.PathEscape(s.ID),
		nil,
		nil,
		nil,
	)
}

func (s *Sandbox) lifecycle(ctx context.Context, action string) error {
	var record SandboxRecord
	if err := s.client.request(
		ctx,
		http.MethodPost,
		"/run-cloud/sandboxes/"+url.PathEscape(s.ID)+"/"+action,
		map[string]any{},
		nil,
		&record,
	); err != nil {
		return err
	}
	s.SandboxRecord = record
	return nil
}

func (c *Client) request(
	ctx context.Context,
	method string,
	path string,
	body any,
	headers map[string]string,
	output any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		detail := strings.TrimSpace(string(raw))
		var payload map[string]any
		if json.Unmarshal(raw, &payload) == nil {
			for _, key := range []string{"detail", "error", "message"} {
				if value, ok := payload[key].(string); ok && value != "" {
					detail = value
					break
				}
			}
		}
		return &Error{Status: response.StatusCode, Detail: detail}
	}
	if output == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, output)
}
