package clash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Version struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

type RuntimeConfig struct {
	Mode      string `json:"mode"`
	MixedPort int    `json:"mixed-port"`
}

type DelayHistory struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

type Proxy struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Alive   bool           `json:"alive"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	History []DelayHistory `json:"history"`
}

type ProxiesResponse struct {
	Proxies map[string]Proxy `json:"proxies"`
}

type Provider struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	VehicleType string    `json:"vehicleType"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Proxies     []Proxy   `json:"proxies"`
}

type ProvidersResponse struct {
	Providers map[string]Provider `json:"providers"`
}

type API interface {
	Version(context.Context) (Version, error)
	Proxies(context.Context) (map[string]Proxy, error)
	Delay(context.Context, string, string, time.Duration) (int, error)
	Select(context.Context, string, string) error
	Providers(context.Context) (map[string]Provider, error)
	UpdateProvider(context.Context, string) error
}

type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Clash API 返回 HTTP %d: %s", e.Code, e.Body)
}

func IsUnauthorized(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && (status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden)
}

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func NewClient(controller, unixSocket, secret string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	baseURL := strings.TrimRight(controller, "/")
	if unixSocket != "" {
		baseURL = "http://localhost"
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", unixSocket)
		}
	}
	if baseURL == "" {
		return nil, errors.New("缺少 Clash controller 地址")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("Clash controller 地址无效: %q", baseURL)
	}
	return &Client{
		baseURL: baseURL,
		secret:  secret,
		http: &http.Client{
			Transport: transport,
			Timeout:   timeout + 3*time.Second,
		},
	}, nil
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var result Version
	err := c.do(ctx, http.MethodGet, "/version", nil, &result)
	return result, err
}

func (c *Client) RuntimeConfig(ctx context.Context) (RuntimeConfig, error) {
	var result RuntimeConfig
	err := c.do(ctx, http.MethodGet, "/configs", nil, &result)
	return result, err
}

func (c *Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var result ProxiesResponse
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, &result); err != nil {
		return nil, err
	}
	return result.Proxies, nil
}

func (c *Client) Delay(ctx context.Context, proxyName, checkURL string, timeout time.Duration) (int, error) {
	endpoint := "/proxies/" + url.PathEscape(proxyName) + "/delay"
	query := url.Values{}
	query.Set("url", checkURL)
	query.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	var result struct {
		Delay int `json:"delay"`
	}
	if err := c.do(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil, &result); err != nil {
		return 0, err
	}
	if result.Delay <= 0 {
		return 0, errors.New("节点检测没有返回有效延迟")
	}
	return result.Delay, nil
}

func (c *Client) Select(ctx context.Context, group, proxyName string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": proxyName}, nil)
}

func (c *Client) Providers(ctx context.Context) (map[string]Provider, error) {
	var result ProvidersResponse
	if err := c.do(ctx, http.MethodGet, "/providers/proxies", nil, &result); err != nil {
		return nil, err
	}
	return result.Providers, nil
}

func (c *Client) UpdateProvider(ctx context.Context, providerName string) error {
	return c.do(ctx, http.MethodPut, "/providers/proxies/"+url.PathEscape(providerName), nil, nil)
}

func (c *Client) do(ctx context.Context, method, endpoint string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("解析 Clash API 响应: %w", err)
	}
	return nil
}
