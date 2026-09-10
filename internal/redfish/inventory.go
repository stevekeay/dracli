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
	"time"
)

const (
	UnableToParseRedfishResponse = "UNABLE TO PARSE REDFISH RESPONSE"
	SignificantClockDrift        = 60 * time.Second
)

type Inventory struct {
	System          SystemSummary     `json:"system"`
	SerialNumber    string            `json:"serial_number,omitempty"`
	IDRAC           FirmwareSummary   `json:"idrac"`
	BIOSVersion     string            `json:"bios_version,omitempty"`
	Memory          MemorySummary     `json:"memory"`
	CPU             ProcessorSummary  `json:"cpu"`
	Status          SystemStatus      `json:"status"`
	RAIDControllers []RAIDController  `json:"raid_controllers,omitempty"`
	NICs            []NIC             `json:"nics,omitempty"`
	Clock           ClockSummary      `json:"clock"`
	Errors          map[string]string `json:"errors,omitempty"`
}

type ClockSummary struct {
	DRACTime     string `json:"drac_time,omitempty"`
	LocalTime    string `json:"local_time,omitempty"`
	DriftSeconds int64  `json:"drift_seconds"`
	Significant  bool   `json:"significant"`
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
	SerialNumber     string            `json:"SerialNumber"`
	BiosVersion      string            `json:"BiosVersion"`
	MemorySummary    memoryResource    `json:"MemorySummary"`
	ProcessorSummary processorResource `json:"ProcessorSummary"`
	PowerState       string            `json:"PowerState"`
	BootProgress     struct {
		LastState     string `json:"LastState"`
		LastStateTime string `json:"LastStateTime"`
	} `json:"BootProgress"`
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
	Model               string `json:"Model"`
	FirmwareVersion     string `json:"FirmwareVersion"`
	DateTime            string `json:"DateTime"`
	DateTimeLocalOffset string `json:"DateTimeLocalOffset"`
}

type storageResource struct {
	ID                 string               `json:"Id"`
	Manufacturer       string               `json:"Manufacturer"`
	Model              string               `json:"Model"`
	FirmwareVersion    string               `json:"FirmwareVersion"`
	StorageControllers []raidControllerData `json:"StorageControllers"`
	Controllers        json.RawMessage      `json:"Controllers"`
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
	result, err := c.Query(ctx, systemID, managerID)
	if err != nil {
		return Inventory{}, err
	}
	if result.Errors == nil {
		result.Errors = make(map[string]string)
	}
	escapedSystem := url.PathEscape(systemID)
	result.RAIDControllers, err = c.raidControllers(ctx, escapedSystem)
	if err != nil {
		if fatalInventoryError(err) {
			return Inventory{}, err
		}
		markUnavailable(&result, "raid_controllers")
	}
	result.NICs, err = c.nics(ctx, escapedSystem)
	if err != nil {
		if fatalInventoryError(err) {
			return Inventory{}, err
		}
		markUnavailable(&result, "nics")
	}
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

// Query fetches the fast system overview. Unlike Inventory, it does not walk
// the storage and network collections, which can require many serial requests
// on an iDRAC.
func (c *Client) Query(ctx context.Context, systemID, managerID string) (Inventory, error) {
	escapedSystem := url.PathEscape(systemID)
	escapedManager := url.PathEscape(managerID)
	result := Inventory{Errors: make(map[string]string)}

	var system systemResource
	if err := c.getPath(ctx, "/redfish/v1/Systems/"+escapedSystem, &system); err != nil {
		if fatalInventoryError(err) {
			return Inventory{}, err
		}
		markUnavailable(&result, "system", "serial_number", "bios", "memory", "cpu", "status")
	} else {
		result.System = SystemSummary{Manufacturer: system.Manufacturer, Model: system.Model}
		result.SerialNumber = system.SerialNumber
		result.BIOSVersion = system.BiosVersion
		result.Memory = MemorySummary{TotalGiB: system.MemorySummary.TotalSystemMemoryGiB}
		result.CPU = ProcessorSummary{
			Count: system.ProcessorSummary.Count, Model: system.ProcessorSummary.Model,
			Cores: system.ProcessorSummary.CoreCount, Threads: system.ProcessorSummary.LogicalProcessorCount,
		}
		result.Status = SystemStatus{
			PowerState: system.PowerState,
			BootProgress: BootProgress{
				LastState: system.BootProgress.LastState, LastStateTime: system.BootProgress.LastStateTime,
			},
		}
	}

	var manager managerResource
	if err := c.getPath(ctx, "/redfish/v1/Managers/"+escapedManager, &manager); err != nil {
		if fatalInventoryError(err) {
			return Inventory{}, err
		}
		markUnavailable(&result, "idrac", "clock")
	} else {
		result.IDRAC = FirmwareSummary{Model: manager.Model, Version: manager.FirmwareVersion}
		clock, err := summarizeClock(manager.DateTime, manager.DateTimeLocalOffset, time.Now())
		if err != nil {
			markUnavailable(&result, "clock")
		} else {
			result.Clock = clock
		}
	}

	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func markUnavailable(result *Inventory, sections ...string) {
	for _, section := range sections {
		result.Errors[section] = UnableToParseRedfishResponse
	}
}

func fatalInventoryError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)
}

func summarizeClock(value, localOffset string, localTime time.Time) (ClockSummary, error) {
	dracTime, err := parseDRACDateTime(value, localOffset)
	if err != nil {
		return ClockSummary{}, err
	}
	drift := dracTime.Sub(localTime)
	driftSeconds := int64(drift.Round(time.Second) / time.Second)
	return ClockSummary{
		DRACTime: value, LocalTime: localTime.Format(time.RFC3339), DriftSeconds: driftSeconds,
		Significant: absDuration(drift) > SignificantClockDrift,
	}, nil
}

func parseDRACDateTime(value, localOffset string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("iDRAC did not report DateTime")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	if localOffset != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", value+localOffset); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse iDRAC DateTime %q", value)
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
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
	var firstErr error
	for _, member := range members {
		var storage storageResource
		if err := c.memberResource(ctx, member, &storage); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		items := append([]raidControllerData(nil), storage.StorageControllers...)
		if len(items) == 0 {
			linked, err := c.controllerData(ctx, storage.Controllers)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				items = append(items, linked...)
			}
		}
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
	return controllers, firstErr
}

func (c *Client) controllerData(ctx context.Context, raw json.RawMessage) ([]raidControllerData, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '[' {
		var result []raidControllerData
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode storage Controllers array: %w", err)
		}
		return result, nil
	}
	if raw[0] != '{' {
		return nil, errors.New("decode storage Controllers: expected object or array")
	}
	var controllerLink link
	if err := json.Unmarshal(raw, &controllerLink); err != nil {
		return nil, fmt.Errorf("decode storage Controllers link: %w", err)
	}
	if controllerLink.ODataID == "" {
		var item raidControllerData
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("decode embedded storage Controller: %w", err)
		}
		return []raidControllerData{item}, nil
	}
	members, err := c.collectionMembers(ctx, controllerLink.ODataID)
	if err != nil {
		return nil, err
	}
	result := make([]raidControllerData, 0, len(members))
	for _, member := range members {
		var item raidControllerData
		if err := c.memberResource(ctx, member, &item); err != nil {
			return result, err
		}
		result = append(result, item)
	}
	return result, nil
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
	nics = collapseNICPartitions(nics)
	sort.Slice(nics, func(i, j int) bool { return nics[i].ID < nics[j].ID })
	return nics, nil
}

type adapterWithPorts struct {
	networkAdapterResource
	Ports []networkPortResource
}

func (c *Client) networkAdapters(ctx context.Context, systemID string) ([]adapterWithPorts, error) {
	members, err := c.collectionMembers(ctx, "/redfish/v1/Systems/"+systemID+"/NetworkAdapters")
	if isUnsupported(err) {
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
			if err != nil && !isUnsupported(err) {
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
	if isUnsupported(err) {
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

func isUnsupported(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == http.StatusNotFound ||
		httpErr.StatusCode == http.StatusMethodNotAllowed ||
		httpErr.StatusCode == http.StatusNotImplemented
}

// collapseNICPartitions mirrors iDRAC's physical-interface view. When both
// NIC.Slot.1-1 and its first partition NIC.Slot.1-1-1 are present, keep the
// physical FQDD and use the partition to fill in live data that is absent from
// the parent resource. Partition-only interfaces are retained.
func collapseNICPartitions(nics []NIC) []NIC {
	indexes := make(map[string]int, len(nics))
	for index, nic := range nics {
		indexes[nic.ID] = index
	}

	omit := make(map[int]struct{})
	for index, child := range nics {
		parentID, ok := parentNICID(child.ID)
		if !ok {
			continue
		}
		parentIndex, exists := indexes[parentID]
		if !exists {
			continue
		}
		mergeNIC(&nics[parentIndex], child)
		omit[index] = struct{}{}
	}

	result := make([]NIC, 0, len(nics)-len(omit))
	for index, nic := range nics {
		if _, skip := omit[index]; !skip {
			result = append(result, nic)
		}
	}
	return result
}

func parentNICID(id string) (string, bool) {
	dash := strings.LastIndexByte(id, '-')
	if dash < 0 || dash == len(id)-1 {
		return "", false
	}
	for _, character := range id[dash+1:] {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	return id[:dash], true
}

func mergeNIC(parent *NIC, child NIC) {
	if parent.Manufacturer == "" {
		parent.Manufacturer = child.Manufacturer
	}
	if parent.Model == "" {
		parent.Model = child.Model
	}
	if parent.Description == "" {
		parent.Description = child.Description
	}
	if child.Link != "" && (parent.Link == "" || strings.EqualFold(parent.Link, "NoLink")) {
		parent.Link = child.Link
	}
	if parent.SpeedMbps == nil {
		parent.SpeedMbps = child.SpeedMbps
	}
	parent.MACAddresses = uniqueStrings(append(parent.MACAddresses, child.MACAddresses...)...)
	if parent.LLDP == nil {
		parent.LLDP = child.LLDP
	}
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
