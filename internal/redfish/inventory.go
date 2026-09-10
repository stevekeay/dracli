package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type Inventory struct {
	System          SystemSummary    `json:"system"`
	IDRAC           FirmwareSummary  `json:"idrac"`
	BIOSVersion     string           `json:"bios_version,omitempty"`
	Memory          MemorySummary    `json:"memory"`
	CPU             ProcessorSummary `json:"cpu"`
	RAIDControllers []RAIDController `json:"raid_controllers"`
	NICs            []NIC            `json:"nics"`
}

type SystemSummary struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
}

type FirmwareSummary struct {
	Model   string `json:"model,omitempty"`
	Version string `json:"version,omitempty"`
}

type MemorySummary struct {
	TotalGiB float64 `json:"total_gib"`
}

type ProcessorSummary struct {
	Count   int    `json:"count"`
	Model   string `json:"model,omitempty"`
	Cores   int    `json:"cores,omitempty"`
	Threads int    `json:"threads,omitempty"`
}

type RAIDController struct {
	ID           string `json:"id,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
}

type NIC struct {
	ID           string       `json:"id"`
	Slot         string       `json:"slot,omitempty"`
	Manufacturer string       `json:"manufacturer,omitempty"`
	Model        string       `json:"model,omitempty"`
	Description  string       `json:"description,omitempty"`
	MACAddresses []string     `json:"mac_addresses"`
	Link         string       `json:"link,omitempty"`
	SpeedMbps    *int         `json:"speed_mbps,omitempty"`
	LLDP         *LLDPSummary `json:"lldp,omitempty"`
}

type LLDPSummary struct {
	SwitchID   string `json:"switch_id,omitempty"`
	SwitchPort string `json:"switch_port,omitempty"`
	Stale      string `json:"stale,omitempty"`
}

type systemResource struct {
	Manufacturer     string            `json:"Manufacturer"`
	Model            string            `json:"Model"`
	BiosVersion      string            `json:"BiosVersion"`
	MemorySummary    memoryResource    `json:"MemorySummary"`
	ProcessorSummary processorResource `json:"ProcessorSummary"`
}

type memoryResource struct {
	TotalSystemMemoryGiB float64 `json:"TotalSystemMemoryGiB"`
}

type processorResource struct {
	Count                 int    `json:"Count"`
	Model                 string `json:"Model"`
	CoreCount             int    `json:"CoreCount"`
	LogicalProcessorCount int    `json:"LogicalProcessorCount"`
}

type managerResource struct {
	Model           string `json:"Model"`
	FirmwareVersion string `json:"FirmwareVersion"`
}

type storageResource struct {
	ID                 string               `json:"Id"`
	Manufacturer       string               `json:"Manufacturer"`
	Model              string               `json:"Model"`
	FirmwareVersion    string               `json:"FirmwareVersion"`
	StorageControllers []raidControllerData `json:"StorageControllers"`
	Controllers        []raidControllerData `json:"Controllers"`
}

type raidControllerData struct {
	MemberID        string `json:"MemberId"`
	ID              string `json:"Id"`
	Manufacturer    string `json:"Manufacturer"`
	Model           string `json:"Model"`
	FirmwareVersion string `json:"FirmwareVersion"`
}

type ethernetResource struct {
	ID                  string `json:"Id"`
	Description         string `json:"Description"`
	MACAddress          string `json:"MACAddress"`
	PermanentMACAddress string `json:"PermanentMACAddress"`
	LinkStatus          string `json:"LinkStatus"`
	SpeedMbps           *int   `json:"SpeedMbps"`
}

type networkAdapterResource struct {
	ID           string `json:"Id"`
	Manufacturer string `json:"Manufacturer"`
	Model        string `json:"Model"`
	NetworkPorts link   `json:"NetworkPorts"`
	Ports        link   `json:"Ports"`
}

type networkPortResource struct {
	ID                         string   `json:"Id"`
	LinkStatus                 string   `json:"LinkStatus"`
	CurrentLinkSpeedMbps       *int     `json:"CurrentLinkSpeedMbps"`
	AssociatedNetworkAddresses []string `json:"AssociatedNetworkAddresses"`
}

type switchConnection struct {
	FQDD                   string `json:"FQDD"`
	StaleData              string `json:"StaleData"`
	SwitchConnectionID     string `json:"SwitchConnectionID"`
	SwitchPortConnectionID string `json:"SwitchPortConnectionID"`
}

type link struct {
	ODataID string `json:"@odata.id"`
}

func (c *Client) Inventory(ctx context.Context, systemID, managerID string) (Inventory, error) {
	escapedSystem := url.PathEscape(systemID)
	escapedManager := url.PathEscape(managerID)
	var system systemResource
	if err := c.getPath(ctx, "/redfish/v1/Systems/"+escapedSystem, &system); err != nil {
		return Inventory{}, err
	}
	var manager managerResource
	if err := c.getPath(ctx, "/redfish/v1/Managers/"+escapedManager, &manager); err != nil {
		return Inventory{}, err
	}

	result := Inventory{
		System:      SystemSummary{Manufacturer: system.Manufacturer, Model: system.Model},
		IDRAC:       FirmwareSummary{Model: manager.Model, Version: manager.FirmwareVersion},
		BIOSVersion: system.BiosVersion,
		Memory:      MemorySummary{TotalGiB: system.MemorySummary.TotalSystemMemoryGiB},
		CPU: ProcessorSummary{
			Count:   system.ProcessorSummary.Count,
			Model:   system.ProcessorSummary.Model,
			Cores:   system.ProcessorSummary.CoreCount,
			Threads: system.ProcessorSummary.LogicalProcessorCount,
		},
	}

	var err error
	result.RAIDControllers, err = c.raidControllers(ctx, escapedSystem)
	if err != nil {
		return Inventory{}, err
	}
	result.NICs, err = c.nics(ctx, escapedSystem)
	if err != nil {
		return Inventory{}, err
	}
	return result, nil
}

func (c *Client) raidControllers(ctx context.Context, systemID string) ([]RAIDController, error) {
	members, err := c.collectionMembers(ctx, "/redfish/v1/Systems/"+systemID+"/Storage")
	if isNotFound(err) {
		return []RAIDController{}, nil
	}
	if err != nil {
		return nil, err
	}

	controllers := make([]RAIDController, 0)
	for _, member := range members {
		var storage storageResource
		if err := c.memberResource(ctx, member, &storage); err != nil {
			return nil, err
		}
		items := append(storage.StorageControllers, storage.Controllers...)
		if len(items) == 0 && (storage.Model != "" || storage.Manufacturer != "") {
			items = []raidControllerData{{
				ID: storage.ID, Manufacturer: storage.Manufacturer,
				Model: storage.Model, FirmwareVersion: storage.FirmwareVersion,
			}}
		}
		for _, item := range items {
			id := item.MemberID
			if id == "" {
				id = item.ID
			}
			controllers = append(controllers, RAIDController{
				ID: id, Manufacturer: item.Manufacturer,
				Model: item.Model, Firmware: item.FirmwareVersion,
			})
		}
	}
	sort.Slice(controllers, func(i, j int) bool { return controllers[i].ID < controllers[j].ID })
	return controllers, nil
}

func (c *Client) nics(ctx context.Context, systemID string) ([]NIC, error) {
	adapterData, err := c.networkAdapters(ctx, systemID)
	if err != nil {
		return nil, err
	}
	lldpData, err := c.switchConnections(ctx, systemID)
	if err != nil {
		return nil, err
	}

	members, err := c.collectionMembers(ctx, "/redfish/v1/Systems/"+systemID+"/EthernetInterfaces")
	if isNotFound(err) {
		return []NIC{}, nil
	}
	if err != nil {
		return nil, err
	}

	nics := make([]NIC, 0, len(members))
	for _, member := range members {
		var ethernet ethernetResource
		if err := c.memberResource(ctx, member, &ethernet); err != nil {
			return nil, err
		}
		nic := NIC{
			ID: ethernet.ID, Slot: nicSlot(ethernet.ID), Description: ethernet.Description,
			Link: ethernet.LinkStatus, SpeedMbps: positiveSpeed(ethernet.SpeedMbps),
			MACAddresses: uniqueStrings(ethernet.MACAddress, ethernet.PermanentMACAddress),
		}
		if adapter, ok := bestAdapter(ethernet.ID, adapterData); ok {
			nic.Manufacturer = adapter.Manufacturer
			nic.Model = adapter.Model
			if port, ok := bestPort(ethernet.ID, adapter.Ports); ok {
				if nic.Link == "" {
					nic.Link = port.LinkStatus
				}
				if nic.SpeedMbps == nil {
					nic.SpeedMbps = positiveSpeed(port.CurrentLinkSpeedMbps)
				}
				nic.MACAddresses = uniqueStrings(append(nic.MACAddresses, port.AssociatedNetworkAddresses...)...)
			}
		}
		if connection, ok := bestConnection(ethernet.ID, lldpData); ok && hasLLDP(connection) {
			nic.LLDP = &LLDPSummary{
				SwitchID:   connection.SwitchConnectionID,
				SwitchPort: connection.SwitchPortConnectionID,
				Stale:      connection.StaleData,
			}
		}
		nics = append(nics, nic)
	}
	sort.Slice(nics, func(i, j int) bool { return nics[i].ID < nics[j].ID })
	return nics, nil
}

type adapterWithPorts struct {
	networkAdapterResource
	Ports []networkPortResource
}

func (c *Client) networkAdapters(ctx context.Context, systemID string) ([]adapterWithPorts, error) {
	members, err := c.collectionMembers(ctx, "/redfish/v1/Systems/"+systemID+"/NetworkAdapters")
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	result := make([]adapterWithPorts, 0, len(members))
	for _, member := range members {
		var adapter networkAdapterResource
		if err := c.memberResource(ctx, member, &adapter); err != nil {
			return nil, err
		}
		withPorts := adapterWithPorts{networkAdapterResource: adapter}
		portPath := adapter.Ports.ODataID
		if portPath == "" {
			portPath = adapter.NetworkPorts.ODataID
		}
		if portPath != "" {
			portMembers, err := c.collectionMembers(ctx, portPath)
			if err != nil && !isNotFound(err) {
				return nil, err
			}
			for _, portMember := range portMembers {
				var port networkPortResource
				if err := c.memberResource(ctx, portMember, &port); err != nil {
					return nil, err
				}
				withPorts.Ports = append(withPorts.Ports, port)
			}
		}
		result = append(result, withPorts)
	}
	return result, nil
}

func (c *Client) switchConnections(ctx context.Context, systemID string) ([]switchConnection, error) {
	path := "/redfish/v1/Systems/" + systemID + "/NetworkPorts/Oem/Dell/DellSwitchConnections"
	members, err := c.collectionMembers(ctx, path)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	connections := make([]switchConnection, 0, len(members))
	for _, member := range members {
		var connection switchConnection
		if err := c.memberResource(ctx, member, &connection); err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, nil
}

func (c *Client) collectionMembers(ctx context.Context, path string) ([]json.RawMessage, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse Redfish collection path: %w", err)
	}
	pageURL := c.baseURL.ResolveReference(reference)
	if !sameOrigin(c.baseURL, pageURL) {
		return nil, fmt.Errorf("refusing cross-origin Redfish path to %s", pageURL.Redacted())
	}
	seen := make(map[string]struct{})
	var members []json.RawMessage
	for page := 0; page < maxPages; page++ {
		if _, exists := seen[pageURL.String()]; exists {
			return nil, fmt.Errorf("Redfish pagination loop at %s", pageURL.Redacted())
		}
		seen[pageURL.String()] = struct{}{}
		var current collection
		if err := c.get(ctx, pageURL, &current); err != nil {
			return nil, err
		}
		members = append(members, current.Members...)
		next := current.NextLink
		if next == "" {
			next = current.LegacyNext
		}
		if next == "" {
			return members, nil
		}
		nextReference, err := url.Parse(next)
		if err != nil {
			return nil, fmt.Errorf("parse Redfish next link: %w", err)
		}
		pageURL = pageURL.ResolveReference(nextReference)
		if !sameOrigin(c.baseURL, pageURL) {
			return nil, fmt.Errorf("refusing cross-origin Redfish next link to %s", pageURL.Redacted())
		}
	}
	return nil, fmt.Errorf("Redfish response exceeded %d pages", maxPages)
}

func (c *Client) memberResource(ctx context.Context, raw json.RawMessage, target any) error {
	var resourceLink link
	if err := json.Unmarshal(raw, &resourceLink); err != nil {
		return fmt.Errorf("decode Redfish collection member: %w", err)
	}
	if resourceLink.ODataID != "" {
		return c.getPath(ctx, resourceLink.ODataID, target)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode expanded Redfish collection member: %w", err)
	}
	return nil
}

func isNotFound(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func bestAdapter(id string, adapters []adapterWithPorts) (adapterWithPorts, bool) {
	bestLength := -1
	var best adapterWithPorts
	for _, adapter := range adapters {
		if (id == adapter.ID || strings.HasPrefix(id, adapter.ID+"-")) && len(adapter.ID) > bestLength {
			best, bestLength = adapter, len(adapter.ID)
		}
	}
	return best, bestLength >= 0
}

func bestPort(id string, ports []networkPortResource) (networkPortResource, bool) {
	bestLength := -1
	var best networkPortResource
	for _, port := range ports {
		if (id == port.ID || strings.HasPrefix(id, port.ID+"-") || strings.HasPrefix(port.ID, id+"-")) && len(port.ID) > bestLength {
			best, bestLength = port, len(port.ID)
		}
	}
	return best, bestLength >= 0
}

func bestConnection(id string, connections []switchConnection) (switchConnection, bool) {
	for _, connection := range connections {
		if connection.FQDD == id {
			return connection, true
		}
	}
	for _, connection := range connections {
		if connection.FQDD == id+"-1" || id == connection.FQDD+"-1" {
			return connection, true
		}
	}
	return switchConnection{}, false
}

func hasLLDP(connection switchConnection) bool {
	return connection.SwitchConnectionID != "" && !strings.EqualFold(connection.SwitchConnectionID, "No Link")
}

func positiveSpeed(speed *int) *int {
	if speed == nil || *speed <= 0 {
		return nil
	}
	return speed
}

func uniqueStrings(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		key := strings.ToUpper(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func nicSlot(id string) string {
	parts := strings.Split(id, ".")
	for index, part := range parts {
		if strings.EqualFold(part, "slot") && index+1 < len(parts) {
			return strings.SplitN(parts[index+1], "-", 2)[0]
		}
	}
	return ""
}
