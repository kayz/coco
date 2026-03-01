package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kayz/coco/internal/config"
	cronpkg "github.com/kayz/coco/internal/cron"
	"github.com/kayz/coco/internal/router"
)

type remoteCronClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type remoteCronCreateRequest struct {
	Name      string         `json:"name"`
	Tag       string         `json:"tag,omitempty"`
	Type      string         `json:"type,omitempty"`
	Schedule  string         `json:"schedule"`
	Message   string         `json:"message,omitempty"`
	Prompt    string         `json:"prompt,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Endpoint  string         `json:"endpoint,omitempty"`
	Auth      string         `json:"auth,omitempty"`
	RelayMode bool           `json:"relay_mode,omitempty"`
	Platform  string         `json:"platform"`
	ChannelID string         `json:"channel_id"`
	UserID    string         `json:"user_id"`
}

func newRemoteCronClient(cfg *config.Config) *remoteCronClient {
	if cfg == nil || !cfg.Relay.CronOnKeeper {
		return nil
	}
	baseURL := inferKeeperBaseURLForCron(cfg.Relay.WebhookURL, cfg.Relay.ServerURL)
	if baseURL == "" {
		return nil
	}
	return &remoteCronClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(cfg.Relay.Token),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func inferKeeperBaseURLForCron(webhookURL, serverURL string) string {
	if strings.TrimSpace(webhookURL) != "" {
		if u, err := url.Parse(strings.TrimSpace(webhookURL)); err == nil && strings.TrimSpace(u.Host) != "" {
			u.Path = ""
			u.RawQuery = ""
			u.Fragment = ""
			return u.String()
		}
	}
	if strings.TrimSpace(serverURL) != "" {
		if u, err := url.Parse(strings.TrimSpace(serverURL)); err == nil && strings.TrimSpace(u.Host) != "" {
			switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
			case "wss":
				u.Scheme = "https"
			case "ws":
				u.Scheme = "http"
			default:
				return ""
			}
			u.Path = ""
			u.RawQuery = ""
			u.Fragment = ""
			return u.String()
		}
	}
	return ""
}

func (c *remoteCronClient) buildRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-Keeper-Token", c.token)
	}
	return req, nil
}

func (c *remoteCronClient) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	req, err := c.buildRequest(ctx, method, path, payload)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("keeper cron api %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func (c *remoteCronClient) Create(ctx context.Context, req remoteCronCreateRequest) (*cronpkg.Job, error) {
	var resp struct {
		OK  bool         `json:"ok"`
		Job *cronpkg.Job `json:"job"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/cron/create", req, &resp); err != nil {
		return nil, err
	}
	if !resp.OK || resp.Job == nil {
		return nil, fmt.Errorf("keeper cron create returned empty job")
	}
	return resp.Job, nil
}

func (c *remoteCronClient) List(ctx context.Context, msg router.Message, tag string) ([]*cronpkg.Job, error) {
	v := url.Values{}
	v.Set("platform", msg.Platform)
	v.Set("channel_id", msg.ChannelID)
	v.Set("user_id", msg.UserID)
	if strings.TrimSpace(tag) != "" {
		v.Set("tag", strings.TrimSpace(tag))
	}
	var resp struct {
		OK   bool           `json:"ok"`
		Jobs []*cronpkg.Job `json:"jobs"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/cron/list?"+v.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("keeper cron list returned ok=false")
	}
	return resp.Jobs, nil
}

func (c *remoteCronClient) Delete(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/cron/delete", map[string]string{"id": id}, &map[string]any{})
}

func (c *remoteCronClient) Pause(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/cron/pause", map[string]string{"id": id}, &map[string]any{})
}

func (c *remoteCronClient) Resume(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/cron/resume", map[string]string{"id": id}, &map[string]any{})
}
