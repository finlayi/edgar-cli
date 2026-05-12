package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	requestInterval = 125 * time.Millisecond
	maxAttempts     = 4
)

type SecClient struct {
	userAgent string
	verbose   bool
	http      *http.Client
	logger    func(string)
	hosts     SECHosts
	mu        sync.Mutex
	nextAt    time.Time
}

type SecClientOptions struct {
	UserAgent  string
	Verbose    bool
	HTTPClient *http.Client
	Logger     func(string)
	Hosts      SECHosts
}

func NewSecClient(options SecClientOptions) *SecClient {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	logger := options.Logger
	if logger == nil {
		logger = func(string) {}
	}
	return &SecClient{
		userAgent: options.UserAgent,
		verbose:   options.Verbose,
		http:      httpClient,
		logger:    logger,
		hosts:     options.Hosts.withDefaults(),
	}
}

func (c *SecClient) Hosts() SECHosts {
	return c.hosts.withDefaults()
}

func (c *SecClient) FetchSECJSON(ctx context.Context, url string, out any) error {
	body, err := c.request(ctx, url, "json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return NewCLIError(ErrorParse, fmt.Sprintf("Unable to parse SEC JSON response from %s: %s", url, err.Error()))
	}
	return nil
}

func (c *SecClient) FetchSECText(ctx context.Context, url string) (string, error) {
	return c.request(ctx, url, "text")
}

func (c *SecClient) logf(format string, args ...any) {
	if c.verbose {
		c.logger(fmt.Sprintf(format, args...))
	}
}

func (c *SecClient) pace(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	delay := c.nextAt.Sub(now)
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	base := time.Now()
	if c.nextAt.After(base) {
		base = c.nextAt
	}
	c.nextAt = base.Add(requestInterval)
	return nil
}

func (c *SecClient) request(ctx context.Context, url string, kind string) (string, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.pace(ctx); err != nil {
			return "", toNetworkError(url, err.Error(), true)
		}

		c.logf("GET %s (attempt %d/%d)", url, attempt, maxAttempts)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", toNetworkError(url, err.Error(), false)
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept-Encoding", "identity")

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < maxAttempts {
				delay := retryDelay(attempt, 0)
				c.logf("Transient network error, waiting %s before retry", delay)
				if sleepErr := sleepContext(ctx, delay); sleepErr != nil {
					return "", toNetworkError(url, sleepErr.Error(), true)
				}
				continue
			}
			return "", toNetworkError(url, err.Error(), true)
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return "", toNetworkError(url, readErr.Error(), true)
		}
		if closeErr != nil {
			return "", toNetworkError(url, closeErr.Error(), true)
		}
		body := string(bodyBytes)

		switch resp.StatusCode {
		case http.StatusForbidden:
			if isUndeclaredAutomationBody(body) {
				return "", NewCLIError(ErrorIdentityRequired, "SEC rejected request as undeclared automation. Use a valid --user-agent or EDGAR_USER_AGENT.")
			}
			return "", toNetworkError(url, "403 Forbidden", false)
		case http.StatusNotFound:
			return "", NewCLIError(ErrorNotFound, "SEC resource not found at "+url)
		case http.StatusTooManyRequests:
			if attempt < maxAttempts {
				delay := retryDelay(attempt, retryAfter(resp.Header.Get("retry-after")))
				c.logf("429 received, waiting %s before retry", delay)
				if err := sleepContext(ctx, delay); err != nil {
					return "", toNetworkError(url, err.Error(), true)
				}
				continue
			}
			return "", NewRetriableCLIError(ErrorRateLimited, "SEC rate limit reached for "+url)
		case http.StatusServiceUnavailable:
			if attempt < maxAttempts {
				delay := retryDelay(attempt, retryAfter(resp.Header.Get("retry-after")))
				c.logf("503 received, waiting %s before retry", delay)
				if err := sleepContext(ctx, delay); err != nil {
					return "", toNetworkError(url, err.Error(), true)
				}
				continue
			}
			return "", toNetworkError(url, "503 Service Unavailable", true)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode >= 500 && attempt < maxAttempts {
				delay := retryDelay(attempt, 0)
				c.logf("HTTP %d, waiting %s before retry", resp.StatusCode, delay)
				if err := sleepContext(ctx, delay); err != nil {
					return "", toNetworkError(url, err.Error(), true)
				}
				continue
			}
			return "", toNetworkError(url, fmt.Sprintf("HTTP %d", resp.StatusCode), false)
		}

		if kind == "json" && strings.TrimSpace(body) == "" {
			return "", NewCLIError(ErrorParse, "SEC returned empty JSON response from "+url)
		}
		return body, nil
	}

	return "", toNetworkError(url, "Request failed after retries", false)
}

func toNetworkError(url string, message string, retriable bool) *CLIError {
	return &CLIError{
		Code:      ErrorNetwork,
		Message:   fmt.Sprintf("SEC request failed for %s: %s", url, message),
		Retriable: retriable,
	}
}

func retryAfter(value string) time.Duration {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		delay := time.Until(retryAt)
		if delay < 0 {
			return 0
		}
		return delay
	}
	return 0
}

func retryDelay(attempt int, headerDelay time.Duration) time.Duration {
	if headerDelay > 0 {
		return headerDelay
	}
	backoff := 250 * time.Millisecond * time.Duration(1<<(attempt-1))
	jitter := time.Duration(rand.Intn(120)) * time.Millisecond
	return backoff + jitter
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isUndeclaredAutomationBody(value string) bool {
	lowered := strings.ToLower(value)
	return strings.Contains(lowered, "undeclared automated tool") ||
		strings.Contains(lowered, "please declare your traffic") ||
		strings.Contains(lowered, "acceptable policy")
}
