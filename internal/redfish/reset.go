package redfish

import (
	"context"
	"net/url"
)

// ResetToDefaults resets iDRAC configuration while preserving user accounts
// and network settings. Dell calls this ResetType "Default".
func (c *Client) ResetToDefaults(ctx context.Context, managerID string) error {
	path := "/redfish/v1/Managers/" + url.PathEscape(managerID) + "/Actions/Oem/DellManager.ResetToDefaults"
	return c.postPath(ctx, path, map[string]string{"ResetType": "Default"})
}
