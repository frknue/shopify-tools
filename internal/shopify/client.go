// Package shopify is a thin, dependency-free client for the Shopify Admin API.
//
// It deliberately exposes only transport concerns (auth, retries, rate limits,
// error mapping). Domain specific queries live in the tool packages that use
// them, so adding a tool never means growing this package.
package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/frknue/shopify-tools/internal/buildinfo"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	maxResponseBytes  = 32 << 20 // 32 MiB guard against unbounded reads
)

// Client talks to one Shopify store.
type Client struct {
	shop        string
	accessToken string
	apiVersion  string

	httpClient *http.Client
	logger     *slog.Logger
	maxRetries int
	baseURL    *url.URL
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client (useful in tests).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithTimeout bounds a single request, including retries of that request.
func WithTimeout(d time.Duration) Option {
	return func(cl *Client) { cl.httpClient.Timeout = d }
}

// WithLogger attaches a structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(cl *Client) { cl.logger = l }
}

// WithMaxRetries sets how often throttled or failed requests are retried.
func WithMaxRetries(n int) Option {
	return func(cl *Client) { cl.maxRetries = n }
}

// WithBaseURL overrides the API host. Intended for tests against httptest.
func WithBaseURL(raw string) Option {
	return func(cl *Client) {
		if u, err := url.Parse(raw); err == nil {
			cl.baseURL = u
		}
	}
}

// New builds a client for the given store.
func New(shop, accessToken, apiVersion string, opts ...Option) (*Client, error) {
	if shop == "" {
		return nil, fmt.Errorf("shopify: shop domain is required")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("shopify: access token is required")
	}
	if apiVersion == "" {
		return nil, fmt.Errorf("shopify: api version is required")
	}

	c := &Client{
		shop:        shop,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: defaultTimeout},
		logger:      slog.New(slog.DiscardHandler),
		maxRetries:  defaultMaxRetries,
	}
	base, err := url.Parse("https://" + shop)
	if err != nil {
		return nil, fmt.Errorf("shopify: invalid shop domain %q: %w", shop, err)
	}
	c.baseURL = base

	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Shop returns the store domain this client is bound to.
func (c *Client) Shop() string { return c.shop }

// APIVersion returns the pinned Admin API version.
func (c *Client) APIVersion() string { return c.apiVersion }

// Do performs a REST Admin API call. path is relative to the versioned admin
// root, e.g. "shop.json". out may be nil to discard the body.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	u := c.baseURL.JoinPath("admin", "api", c.apiVersion, path)

	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("shopify: encode request: %w", err)
		}
	}

	resp, err := c.doWithRetry(ctx, method, u.String(), payload)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("shopify: read response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return newAPIError(resp, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("shopify: decode response: %w", err)
	}
	return nil
}

// doWithRetry sends the request, retrying on throttling and transient errors
// with exponential backoff that honours Retry-After.
func (c *Client) doWithRetry(ctx context.Context, method, rawURL string, payload []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt)
			c.logger.Debug("retrying request", "attempt", attempt, "delay", delay, "url", rawURL)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
		if err != nil {
			return nil, fmt.Errorf("shopify: build request: %w", err)
		}
		req.Header.Set("X-Shopify-Access-Token", c.accessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", buildinfo.UserAgent())
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("shopify: %s %s: %w", method, rawURL, err)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		c.logger.Debug("request complete",
			"method", method, "url", rawURL, "status", resp.StatusCode, "duration", time.Since(start))

		if !shouldRetry(resp.StatusCode) || attempt == c.maxRetries {
			return resp, nil
		}

		wait := retryAfter(resp)
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("shopify: %s %s: retryable status %d", method, rawURL, resp.StatusCode)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return nil, lastErr
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

func backoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt-1))) * 500 * time.Millisecond
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}
