package redfish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestJobsReadsExpandedQueue(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("$expand") != "*($levels=1)" {
			t.Fatalf("query = %q", request.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `{"Members":[{"@odata.id":"/jobs/JID_1","Id":"JID_1","Name":"Configure","JobState":"Scheduled","JobType":"Configuration","Message":"Task successfully scheduled.","PercentComplete":20,"StartTime":"TIME_NOW"}]}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := client.Jobs(context.Background(), "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
	job := jobs[0]
	if job.ID != "JID_1" || job.State != "Scheduled" || job.PercentComplete != 20 || job.Type != "Configuration" {
		t.Fatalf("job = %#v", job)
	}
}

func TestClearJobsUsesDellDeleteJobQueueAction(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		wantPath := "/redfish/v1/Dell/Managers/iDRAC.Embedded.1/DellJobService/Actions/DellJobService.DeleteJobQueue"
		if request.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", request.URL.Path, wantPath)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["JobID"] != "JID_CLEARALL" {
			t.Fatalf("payload = %s", body)
		}
		return jsonResponse(http.StatusOK, `{}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ClearJobs(context.Background(), "iDRAC.Embedded.1"); err != nil {
		t.Fatal(err)
	}
}
