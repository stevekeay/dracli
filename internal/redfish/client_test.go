package redfish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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
