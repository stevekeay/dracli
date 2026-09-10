package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type Job struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	State           string `json:"state,omitempty"`
	Type            string `json:"type,omitempty"`
	Message         string `json:"message,omitempty"`
	PercentComplete int    `json:"percent_complete"`
	StartTime       string `json:"start_time,omitempty"`
	EndTime         string `json:"end_time,omitempty"`
	CompletionTime  string `json:"completion_time,omitempty"`
}

type jobResource struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	JobState        string `json:"JobState"`
	JobType         string `json:"JobType"`
	Message         string `json:"Message"`
	PercentComplete int    `json:"PercentComplete"`
	StartTime       string `json:"StartTime"`
	EndTime         string `json:"EndTime"`
	CompletionTime  string `json:"CompletionTime"`
}

func (c *Client) Jobs(ctx context.Context, managerID string) ([]Job, error) {
	path := "/redfish/v1/Managers/" + url.PathEscape(managerID) + "/Jobs?$expand=*($levels=1)"
	members, err := c.collectionMembers(ctx, path)
	if err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(members))
	for _, member := range members {
		var resource jobResource
		if err := json.Unmarshal(member, &resource); err != nil {
			return nil, fmt.Errorf("decode job queue member: %w", err)
		}
		if resource.ID == "" {
			if err := c.memberResource(ctx, member, &resource); err != nil {
				return nil, fmt.Errorf("query job queue member: %w", err)
			}
		}
		jobs = append(jobs, Job{
			ID: resource.ID, Name: resource.Name, State: resource.JobState,
			Type: resource.JobType, Message: resource.Message,
			PercentComplete: resource.PercentComplete, StartTime: resource.StartTime,
			EndTime: resource.EndTime, CompletionTime: resource.CompletionTime,
		})
	}
	return jobs, nil
}

func (c *Client) ClearJobs(ctx context.Context, managerID string) error {
	path := "/redfish/v1/Dell/Managers/" + url.PathEscape(managerID) + "/DellJobService/Actions/DellJobService.DeleteJobQueue"
	return c.postPath(ctx, path, map[string]string{"JobID": "JID_CLEARALL"})
}
