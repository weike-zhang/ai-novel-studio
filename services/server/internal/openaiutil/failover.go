// Package openaiutil provides OpenAI-compatible client helpers with
// primary/fallback failover. When the primary endpoint fails (transport
// error, timeout, or an HTTP 5xx), the same request is transparently retried
// against the fallback endpoint with its own API key and model name.
package openaiutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Endpoint describes one OpenAI-compatible upstream.
type Endpoint struct {
	BaseURL string
	APIKey  string
	Model   string
}

// failoverTransport retries a failed request against the fallback endpoint.
type failoverTransport struct {
	primary   Endpoint
	fallback  *Endpoint
	transport http.RoundTripper
}

func (t *failoverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var snapshot []byte
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		snapshot = raw
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	}

	resp, err := t.transport.RoundTrip(req)
	if err == nil && resp.StatusCode < 500 {
		return resp, nil
	}
	if t.fallback == nil {
		return resp, err
	}
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return t.doFallback(req, snapshot)
}

func (t *failoverTransport) doFallback(req *http.Request, snapshot []byte) (*http.Response, error) {
	fallbackURL, err := url.Parse(t.fallback.BaseURL)
	if err != nil {
		return nil, err
	}
	primaryURL, err := url.Parse(t.primary.BaseURL)
	if err != nil {
		return nil, err
	}
	retry, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), nil)
	if err != nil {
		return nil, err
	}
	retry.URL.Scheme = fallbackURL.Scheme
	retry.URL.Host = fallbackURL.Host
	retry.URL.Path = swapPath(primaryURL.Path, fallbackURL.Path, req.URL.Path)
	retry.Header = req.Header.Clone()
	retry.Header.Set("Authorization", "Bearer "+t.fallback.APIKey)

	body := snapshot
	if body == nil && req.Body != nil {
		raw, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		body = raw
	}
	if len(body) > 0 {
		if t.fallback.Model != "" && t.fallback.Model != t.primary.Model {
			if rewritten, rewriteErr := rewriteModel(body, t.fallback.Model); rewriteErr == nil {
				body = rewritten
			}
		}
		retry.ContentLength = int64(len(body))
		retry.Body = io.NopCloser(bytes.NewReader(body))
		retry.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return t.transport.RoundTrip(retry)
}

func rewriteModel(body []byte, model string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = model
	return json.Marshal(payload)
}

// swapPath replaces the primary base path inside the request path with the
// fallback base path, preserving everything that follows it. When the primary
// has no base path, the fallback base path is simply prepended.
func swapPath(primaryBase, fallbackBase, requestPath string) string {
	requestPath = strings.TrimPrefix(requestPath, "/")
	if primaryBase != "" && primaryBase != "/" {
		prefix := strings.TrimPrefix(primaryBase, "/")
		if requestPath == prefix {
			requestPath = ""
		} else if strings.HasPrefix(requestPath, prefix+"/") {
			requestPath = strings.TrimPrefix(requestPath, prefix)
		}
	}
	if fallbackBase != "" && fallbackBase != "/" {
		return "/" + strings.TrimPrefix(fallbackBase, "/") + "/" + strings.TrimPrefix(requestPath, "/")
	}
	return "/" + strings.TrimPrefix(requestPath, "/")
}

// NewFailoverClient builds an OpenAI client whose primary endpoint fails over
// to the given fallback. When fallback is nil only the primary is used.
func NewFailoverClient(primary, fallback *Endpoint, timeout time.Duration) openai.Client {
	base := &http.Client{Timeout: timeout}
	if fallback == nil {
		return openai.NewClient(option.WithAPIKey(primary.APIKey), option.WithBaseURL(primary.BaseURL), option.WithHTTPClient(base))
	}
	base.Transport = &failoverTransport{
		primary:  *primary,
		fallback: fallback,
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return openai.NewClient(option.WithAPIKey(primary.APIKey), option.WithBaseURL(primary.BaseURL), option.WithHTTPClient(base))
}
