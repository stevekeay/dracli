package redfish

import (
	"context"
	"fmt"
	"net/url"
)

var dracSettingNames = []string{
	"SNMP.1.AgentEnable",
	"SNMP.1.SNMPProtocol",
	"SNMP.1.AgentCommunity",
	"SNMP.1.AlertPort",
	"SwitchConnectionView.1.Enable",
	"NTPConfigGroup.1.NTPEnable",
	"Time.1.Timezone",
	"IPv4.1.DNS1",
	"IPv4.1.DNS2",
	"NTPConfigGroup.1.NTP1",
	"NTPConfigGroup.1.NTP2",
}

var biosSettingNames = []string{
	"HttpDev1TlsMode",
	"TimeZone",
	"OS-BMC.1.AdminState",
	"IPMILan.1.Enable",
	"SecureBoot",
	"PxeDev1EnDis",
	"HttpDev1EnDis",
	"HttpDev2EnDis",
	"HttpDev3EnDis",
	"HttpDev4EnDis",
}

type SettingsSelection struct {
	Hostname        string         `json:"hostname,omitempty"`
	Attributes      map[string]any `json:"attributes"`
	Order           []string       `json:"-"`
	IncludeHostname bool           `json:"-"`
}

type attributesResource struct {
	Attributes map[string]any `json:"Attributes"`
}

func (c *Client) DRACSettings(ctx context.Context, managerID string) (SettingsSelection, error) {
	escapedManager := url.PathEscape(managerID)
	var resource attributesResource
	path := "/redfish/v1/Managers/" + escapedManager + "/Attributes"
	if err := c.getPath(ctx, path, &resource); err != nil {
		if !isUnsupported(err) {
			return SettingsSelection{}, err
		}
		path = "/redfish/v1/Managers/" + escapedManager + "/Oem/Dell/DellAttributes/" + escapedManager
		if err := c.getPath(ctx, path, &resource); err != nil {
			return SettingsSelection{}, err
		}
	}

	hostname, err := c.managerHostname(ctx, managerID)
	if err != nil && !isUnsupported(err) {
		return SettingsSelection{}, err
	}
	return SettingsSelection{
		Hostname:        hostname,
		Attributes:      selectAttributes(resource.Attributes, dracSettingNames),
		Order:           dracSettingNames,
		IncludeHostname: true,
	}, nil
}

func (c *Client) BIOSSettings(ctx context.Context, systemID string) (SettingsSelection, error) {
	var resource attributesResource
	path := "/redfish/v1/Systems/" + url.PathEscape(systemID) + "/Bios"
	if err := c.getPath(ctx, path, &resource); err != nil {
		return SettingsSelection{}, err
	}
	return SettingsSelection{Attributes: selectAttributes(resource.Attributes, biosSettingNames), Order: biosSettingNames}, nil
}

func (c *Client) managerHostname(ctx context.Context, managerID string) (string, error) {
	path := "/redfish/v1/Managers/" + url.PathEscape(managerID) + "/EthernetInterfaces"
	members, err := c.collectionMembers(ctx, path)
	if err != nil {
		return "", err
	}
	if len(members) == 0 {
		return "", nil
	}
	var ethernet struct {
		HostName string `json:"HostName"`
	}
	if err := c.memberResource(ctx, members[0], &ethernet); err != nil {
		return "", fmt.Errorf("query iDRAC hostname: %w", err)
	}
	return ethernet.HostName, nil
}

func selectAttributes(attributes map[string]any, names []string) map[string]any {
	selected := make(map[string]any, len(names))
	for _, name := range names {
		selected[name] = attributes[name]
	}
	return selected
}
