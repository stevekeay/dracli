package redfish

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const maxPages = 1_000

type Client struct {
	baseURL  *url.URL
	username string
	password string
	http     *http.Client
}

type HTTPError struct {
	URL        string
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("request %s: %s", e.URL, e.Status)
	}
	return fmt.Sprintf("request %s: %s: %s", e.URL, e.Status, e.Body)
}

type LogEntry struct {
	Created string `json:"Created"`
	Message string `json:"Message"`
}

type LogPage struct {
	Number  int
	Entries []json.RawMessage
	More    bool
}

type collection struct {
	Members    []json.RawMessage `json:"Members"`
	NextLink   string            `json:"Members@odata.nextLink"`
	LegacyNext string            `json:"@odata.nextLink"`
}

func NewClient(baseURL, username, password string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redfish base URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, errors.New("Redfish base URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("Redfish base URL has no host")
	}
	if parsed.User != nil {
		return nil, errors.New("Redfish base URL must not contain credentials")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: parsed, username: username, password: password, http: httpClient}, nil
}

func (c *Client) LifecycleLogs(ctx context.Context, managerID string) ([]json.RawMessage, error) {
	var entries []json.RawMessage
	err := c.LifecycleLogPages(ctx, managerID, func(page LogPage) (bool, error) {
		entries = append(entries, page.Entries...)
		return true, nil
	})
	return entries, err
}

// LifecycleLogPages visits each Lifecycle Controller log page until there are
// no more pages or visit returns false. Pagination URLs remain constrained to
// the iDRAC origin.
func (c *Client) LifecycleLogPages(ctx context.Context, managerID string, visit func(LogPage) (bool, error)) error {
	path := "/redfish/v1/Managers/" + url.PathEscape(managerID) + "/LogServices/Lclog/Entries"
	pageURL := c.baseURL.ResolveReference(&url.URL{Path: path})
	seen := make(map[string]struct{})

	for page := 0; page < maxPages; page++ {
		if _, exists := seen[pageURL.String()]; exists {
			return fmt.Errorf("Redfish pagination loop at %s", pageURL.Redacted())
		}
		seen[pageURL.String()] = struct{}{}

		collection, err := c.getCollection(ctx, pageURL)
		if err != nil {
			return err
		}

		next := collection.NextLink
		if next == "" {
			next = collection.LegacyNext
		}
		var nextURL *url.URL
		if next != "" {
			nextURL, err = url.Parse(next)
			if err != nil {
				return fmt.Errorf("parse Redfish next link: %w", err)
			}
			nextURL = pageURL.ResolveReference(nextURL)
			if !sameOrigin(c.baseURL, nextURL) {
				return fmt.Errorf("refusing cross-origin Redfish next link to %s", nextURL.Redacted())
			}
		}

		proceed, err := visit(LogPage{Number: page + 1, Entries: collection.Members, More: nextURL != nil})
		if err != nil {
			return err
		}
		if nextURL == nil || !proceed {
			return nil
		}
		pageURL = nextURL
	}

	return fmt.Errorf("Redfish response exceeded %d pages", maxPages)
}

func (c *Client) getCollection(ctx context.Context, endpoint *url.URL) (collection, error) {
	var result collection
	if err := c.get(ctx, endpoint, &result); err != nil {
		return collection{}, err
	}
	return result, nil
}

func (c *Client) getPath(ctx context.Context, path string, target any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse Redfish path: %w", err)
	}
	endpoint := c.baseURL.ResolveReference(reference)
	if !sameOrigin(c.baseURL, endpoint) {
		return fmt.Errorf("refusing cross-origin Redfish path to %s", endpoint.Redacted())
	}
	return c.get(ctx, endpoint, target)
}

func (c *Client) get(ctx context.Context, endpoint *url.URL, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create Redfish request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.username, c.password)

	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return &HTTPError{
			URL:        endpoint.Redacted(),
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       string(body),
		}
	}

	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response from %s: %w", endpoint.Redacted(), err)
	}
	return nil
}

func (c *Client) patchPath(ctx context.Context, path string, value any) error {
	return c.sendPath(ctx, http.MethodPatch, path, value)
}

func (c *Client) postPath(ctx context.Context, path string, value any) error {
	return c.sendPath(ctx, http.MethodPost, path, value)
}

func (c *Client) sendPath(ctx context.Context, method, path string, value any) error {
	reference, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse Redfish path: %w", err)
	}
	endpoint := c.baseURL.ResolveReference(reference)
	if !sameOrigin(c.baseURL, endpoint) {
		return fmt.Errorf("refusing cross-origin Redfish path to %s", endpoint.Redacted())
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Redfish request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Redfish request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint.Redacted(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return &HTTPError{URL: endpoint.Redacted(), StatusCode: response.StatusCode, Status: response.Status, Body: string(responseBody)}
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host
}
