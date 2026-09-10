package redfish

import (
	"context"
	"net/url"
)

type SystemStatus struct {
	PowerState   string       `json:"power_state,omitempty"`
	BootProgress BootProgress `json:"boot_progress"`
}

type BootProgress struct {
	LastState     string `json:"last_state,omitempty"`
	LastStateTime string `json:"last_state_time,omitempty"`
}

func (c *Client) SystemStatus(ctx context.Context, systemID string) (SystemStatus, error) {
	var resource struct {
		PowerState   string `json:"PowerState"`
		BootProgress struct {
			LastState     string `json:"LastState"`
			LastStateTime string `json:"LastStateTime"`
		} `json:"BootProgress"`
	}
	path := "/redfish/v1/Systems/" + url.PathEscape(systemID)
	if err := c.getPath(ctx, path, &resource); err != nil {
		return SystemStatus{}, err
	}
	return SystemStatus{
		PowerState: resource.PowerState,
		BootProgress: BootProgress{
			LastState:     resource.BootProgress.LastState,
			LastStateTime: resource.BootProgress.LastStateTime,
		},
	}, nil
}
