package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLifecycleLogsFollowsPagination(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "root" || password != "secret" {
			t.Errorf("BasicAuth() = %q, %q, %v", username, password, ok)
		}
		switch request.URL.Query().Get("page") {
		case "":
			return jsonResponse(http.StatusOK, `{"Members":[{"Created":"first","Message":"one"}],"Members@odata.nextLink":"?page=2"}`), nil
		case "2":
			return jsonResponse(http.StatusOK, `{"Members":[{"Created":"second","Message":"two"}]}`), nil
		default:
			return jsonResponse(http.StatusBadRequest, `{"error":"unexpected page"}`), nil
		}
	})}

	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := client.LifecycleLogs(context.Background(), "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	var second LogEntry
	if err := json.Unmarshal(entries[1], &second); err != nil {
		t.Fatal(err)
	}
	if second.Message != "two" {
		t.Fatalf("second message = %q, want two", second.Message)
	}
}

func TestSystemEventLogsUseSELService(t *testing.T) {
	t.Parallel()

	var requestedPath string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedPath = request.URL.Path
		return jsonResponse(http.StatusOK, `{"Members":[{"Created":"now","Message":"Fan failure"}]}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := client.SystemEventLogs(context.Background(), "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if requestedPath != "/redfish/v1/Managers/iDRAC.Embedded.1/LogServices/Sel/Entries" {
		t.Fatalf("request path = %q", requestedPath)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestLifecycleLogsRejectsCrossOriginPagination(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"Members":[],"Members@odata.nextLink":"https://example.invalid/steal"}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.LifecycleLogs(context.Background(), "iDRAC.Embedded.1"); err == nil {
		t.Fatal("LifecycleLogs() accepted a cross-origin next link")
	}
}

func TestLifecycleLogPagesCanStopAfterFirstPage(t *testing.T) {
	t.Parallel()

	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusOK, `{"Members":[{"Message":"one"}],"Members@odata.nextLink":"?page=2"}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	var got LogPage
	err = client.LifecycleLogPages(context.Background(), "iDRAC.Embedded.1", func(page LogPage) (bool, error) {
		got = page
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || got.Number != 1 || !got.More || len(got.Entries) != 1 {
		t.Fatalf("requests = %d, page = %#v", requests, got)
	}
}

func TestGetRetriesServerErrorAfterThirtySeconds(t *testing.T) {
	t.Parallel()

	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return jsonResponse(http.StatusServiceUnavailable, `{"error":"temporarily unavailable"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"PowerState":"On"}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	var waited time.Duration
	var notified time.Duration
	client.SetRetryNotifier(func(delay time.Duration) {
		notified = delay
	})
	client.wait = func(_ context.Context, delay time.Duration) error {
		waited = delay
		return nil
	}

	status, err := client.SystemStatus(context.Background(), "System.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if waited != 30*time.Second {
		t.Fatalf("retry delay = %s, want 30s", waited)
	}
	if notified != 30*time.Second {
		t.Fatalf("notified delay = %s, want 30s", notified)
	}
	if status.PowerState != "On" {
		t.Fatalf("power state = %q, want On", status.PowerState)
	}
}

func TestGetDoesNotRetryClientError(t *testing.T) {
	t.Parallel()

	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error {
		t.Fatal("wait called for a client error")
		return nil
	}

	_, err = client.SystemStatus(context.Background(), "System.Embedded.1")
	if err == nil {
		t.Fatal("SystemStatus() succeeded, want an error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("error = %v, want HTTP 404 error", err)
	}
}

func TestGetStopsAfterOneServerErrorRetry(t *testing.T) {
	t.Parallel()

	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(http.StatusServiceUnavailable, `{"attempt":`+strconv.Itoa(requests)+`}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }

	_, err = client.SystemStatus(context.Background(), "System.Embedded.1")
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %v, want HTTP 503 error", err)
	}
	if httpErr.Body != `{"attempt":2}` {
		t.Fatalf("error body = %q, want final response body", httpErr.Body)
	}
}

func TestGetCancelsServerErrorBackoff(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, `{}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.SystemStatus(ctx, "System.Embedded.1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
