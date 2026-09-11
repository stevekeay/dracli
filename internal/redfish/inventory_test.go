package redfish

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInventoryCollectsHardwareNICAndLLDPData(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"/redfish/v1/Systems/System.Embedded.1": `{
			"Manufacturer":"Dell Inc.","Model":"PowerEdge R7615","BiosVersion":"1.6.10",
			"MemorySummary":{"TotalSystemMemoryGiB":96},
			"ProcessorSummary":{"Count":1,"Model":"AMD EPYC 9124","CoreCount":16,"LogicalProcessorCount":32}}`,
		"/redfish/v1/Managers/iDRAC.Embedded.1":                 `{"Model":"iDRAC9","FirmwareVersion":"7.00.60.00","DateTime":"2026-09-10T08:00:00+01:00"}`,
		"/redfish/v1/Systems/System.Embedded.1/Storage":         `{"Members":[{"@odata.id":"/storage/RAID.Slot.6-1"}]}`,
		"/storage/RAID.Slot.6-1":                                `{"Controllers":{"@odata.id":"/storage/RAID.Slot.6-1/Controllers"},"StorageControllers":[{"MemberId":"RAID.Slot.6-1","Manufacturer":"Dell","Model":"PERC H755","FirmwareVersion":"52.21"}]}`,
		"/redfish/v1/Systems/System.Embedded.1/NetworkAdapters": `{"Members":[{"@odata.id":"/adapters/NIC.Slot.1"}]}`,
		"/adapters/NIC.Slot.1":                                  `{"Id":"NIC.Slot.1","Manufacturer":"Broadcom","Model":"57414","NetworkPorts":{"@odata.id":"/adapters/NIC.Slot.1/ports"}}`,
		"/adapters/NIC.Slot.1/ports":                            `{"Members":[{"@odata.id":"/ports/NIC.Slot.1-1"}]}`,
		"/ports/NIC.Slot.1-1":                                   `{"Id":"NIC.Slot.1-1","LinkStatus":"Up","CurrentLinkSpeedMbps":25000,"AssociatedNetworkAddresses":["14:23:F3:F5:25:F0"]}`,
		"/redfish/v1/Systems/System.Embedded.1/NetworkPorts/Oem/Dell/DellSwitchConnections": `{"Members":[{"FQDD":"NIC.Slot.1-1-1","StaleData":"NotStale","SwitchConnectionID":"aa:bb:cc:dd:ee:ff","SwitchPortConnectionID":"Ethernet1/6"}]}`,
		"/redfish/v1/Systems/System.Embedded.1/EthernetInterfaces":                          `{"Members":[{"@odata.id":"/ethernet/NIC.Slot.1-1"},{"@odata.id":"/ethernet/NIC.Slot.1-1-1"}]}`,
		"/ethernet/NIC.Slot.1-1":   `{"Id":"NIC.Slot.1-1","Description":"NIC in Slot 1 Port 1","MACAddress":"14:23:F3:F5:25:F0","SpeedMbps":0}`,
		"/ethernet/NIC.Slot.1-1-1": `{"Id":"NIC.Slot.1-1-1","Description":"Partition 1","MACAddress":"14:23:F3:F5:25:F0","LinkStatus":"Up","SpeedMbps":25000}`,
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, ok := responses[request.URL.Path]
		if !ok {
			return nil, fmt.Errorf("unexpected request %s", request.URL.Path)
		}
		return jsonResponse(http.StatusOK, body), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := client.Inventory(context.Background(), "System.Embedded.1", "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Memory.TotalGiB != 96 || inventory.CPU.Cores != 16 {
		t.Fatalf("unexpected summary: %#v", inventory)
	}
	if len(inventory.RAIDControllers) != 1 || inventory.RAIDControllers[0].Model != "PERC H755" {
		t.Fatalf("unexpected RAID controllers: %#v", inventory.RAIDControllers)
	}
	if len(inventory.NICs) != 1 {
		t.Fatalf("got %d NICs, want 1", len(inventory.NICs))
	}
	nic := inventory.NICs[0]
	if nic.Slot != "1" || nic.Manufacturer != "Broadcom" || nic.Model != "57414" {
		t.Fatalf("unexpected NIC identity: %#v", nic)
	}
	if nic.SpeedMbps == nil || *nic.SpeedMbps != 25000 || nic.Link != "Up" {
		t.Fatalf("unexpected NIC link: %#v", nic)
	}
	if nic.LLDP == nil || nic.LLDP.SwitchPort != "Ethernet1/6" {
		t.Fatalf("unexpected NIC LLDP: %#v", nic)
	}
}

func TestQueryUsesOnlySummaryResourcesAndIncludesSystemStatus(t *testing.T) {
	t.Parallel()

	requests := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/redfish/v1/Systems/System.Embedded.1":
			return jsonResponse(http.StatusOK, `{"Manufacturer":"Dell","Model":"PowerEdge","SerialNumber":"ABC1234D","PowerState":"On","BootProgress":{"LastState":"OSRunning","LastStateTime":"2026-09-10T07:00:00Z"}}`), nil
		case "/redfish/v1/Managers/iDRAC.Embedded.1":
			return jsonResponse(http.StatusOK, `{"Model":"iDRAC9","FirmwareVersion":"7.20","DateTime":"2026-09-10T08:00:00+01:00"}`), nil
		default:
			t.Fatalf("query fetched slow inventory resource %q", request.URL.Path)
			return nil, nil
		}
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), "System.Embedded.1", "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if result.SerialNumber != "ABC1234D" {
		t.Fatalf("serial number = %q", result.SerialNumber)
	}
	if result.IDRAC.Model != "iDRAC9" || result.IDRAC.Version != "7.20" {
		t.Fatalf("iDRAC summary = %#v", result.IDRAC)
	}
	if result.Status.PowerState != "On" || result.Status.BootProgress.LastState != "OSRunning" {
		t.Fatalf("status = %#v", result.Status)
	}
	if result.RAIDControllers != nil || result.NICs != nil {
		t.Fatalf("query unexpectedly populated detailed inventory: %#v", result)
	}
}

func TestQueryReturnsRequestAndHTTPFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport roundTripFunc
		want      string
	}{
		{
			name: "HTTP error",
			transport: func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusServiceUnavailable, `{"error":"temporarily unavailable"}`), nil
			},
			want: "Service Unavailable",
		},
		{
			name: "connection error",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("TLS certificate verification failed")
			},
			want: "TLS certificate verification failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient("https://bmc.example", "root", "secret", &http.Client{Transport: test.transport})
			if err != nil {
				t.Fatal(err)
			}
			client.wait = func(context.Context, time.Duration) error { return nil }
			inventory, err := client.Query(context.Background(), "System.Embedded.1", "iDRAC.Embedded.1")
			if err == nil {
				t.Fatalf("Query() error = nil, inventory = %#v", inventory)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Query() error = %q, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestQueryKeepsDecodeFailureLocalToAffectedSections(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/redfish/v1/Systems/System.Embedded.1":
			return jsonResponse(http.StatusOK, `{`), nil
		case "/redfish/v1/Managers/iDRAC.Embedded.1":
			return jsonResponse(http.StatusOK, `{"Model":"iDRAC9","FirmwareVersion":"7.20","DateTime":"2026-09-10T08:00:00+01:00"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.Path)
		}
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := client.Query(context.Background(), "System.Embedded.1", "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.IDRAC.Version != "7.20" {
		t.Fatalf("successful manager section was lost: %#v", inventory)
	}
	for _, section := range []string{"system", "serial_number", "bios", "memory", "cpu", "status"} {
		if inventory.Errors[section] != UnableToParseRedfishResponse {
			t.Errorf("%s error = %q, want parse marker", section, inventory.Errors[section])
		}
	}
}

func TestInventoryKeepsSuccessfulSectionsWhenStorageCannotBeDecoded(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"/redfish/v1/Systems/System.Embedded.1":                                             `{"Manufacturer":"Dell","Model":"PowerEdge R760"}`,
		"/redfish/v1/Managers/iDRAC.Embedded.1":                                             `{"Model":"iDRAC9","FirmwareVersion":"7.20","DateTime":"2026-09-10T08:00:00+01:00"}`,
		"/redfish/v1/Systems/System.Embedded.1/Storage":                                     `{"Members":[{"@odata.id":"/storage/broken"}]}`,
		"/storage/broken":                                                                   `{"Controllers":"not an object or array"}`,
		"/redfish/v1/Systems/System.Embedded.1/NetworkAdapters":                             `{"Members":[]}`,
		"/redfish/v1/Systems/System.Embedded.1/NetworkPorts/Oem/Dell/DellSwitchConnections": `{"Members":[]}`,
		"/redfish/v1/Systems/System.Embedded.1/EthernetInterfaces":                          `{"Members":[]}`,
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, ok := responses[request.URL.Path]
		if !ok {
			return nil, fmt.Errorf("unexpected request %s", request.URL.Path)
		}
		return jsonResponse(http.StatusOK, body), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := client.Inventory(context.Background(), "System.Embedded.1", "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.System.Model != "PowerEdge R760" || inventory.IDRAC.Version != "7.20" {
		t.Fatalf("successful sections were lost: %#v", inventory)
	}
	if inventory.Errors["raid_controllers"] != UnableToParseRedfishResponse {
		t.Fatalf("RAID error = %q, want parse marker", inventory.Errors["raid_controllers"])
	}
}

func TestControllerDataFollowsModernControllersLink(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/controllers":
			return jsonResponse(http.StatusOK, `{"Members":[{"@odata.id":"/controllers/0"}]}`), nil
		case "/controllers/0":
			return jsonResponse(http.StatusOK, `{"Id":"0","Manufacturer":"Dell","Model":"PERC H965i Front","FirmwareVersion":"8.14"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.Path)
		}
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.controllerData(context.Background(), []byte(`{"@odata.id":"/controllers"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Model != "PERC H965i Front" {
		t.Fatalf("controllers = %#v", items)
	}
}

func TestSummarizeClockAccountsForTimezoneAndWarnsForDrift(t *testing.T) {
	t.Parallel()

	local := time.Date(2026, 9, 10, 7, 0, 30, 0, time.UTC)
	clock, err := summarizeClock("2026-09-10T08:00:00+01:00", "", local)
	if err != nil {
		t.Fatal(err)
	}
	if clock.DriftSeconds != -30 || clock.Significant {
		t.Fatalf("clock = %#v", clock)
	}

	clock, err = summarizeClock("2026-09-10T08:03:00", "+01:00", local)
	if err != nil {
		t.Fatal(err)
	}
	if clock.DriftSeconds != 150 || !clock.Significant {
		t.Fatalf("drifted clock = %#v", clock)
	}
}

func TestCollapseNICPartitionsKeepsPartitionWhenParentIsAbsent(t *testing.T) {
	t.Parallel()

	speed := 10_000
	nics := collapseNICPartitions([]NIC{
		{ID: "NIC.Slot.1-1", MACAddresses: []string{"AA:BB:CC:DD:EE:FF"}},
		{ID: "NIC.Slot.1-1-1", Link: "Up", SpeedMbps: &speed, MACAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
		{ID: "NIC.Slot.2-1-1", Link: "Down"},
	})
	if len(nics) != 2 {
		t.Fatalf("got %d NICs, want 2: %#v", len(nics), nics)
	}
	if nics[0].ID != "NIC.Slot.1-1" || nics[0].Link != "Up" || nics[0].SpeedMbps == nil {
		t.Fatalf("partition data was not merged: %#v", nics[0])
	}
	if nics[1].ID != "NIC.Slot.2-1-1" {
		t.Fatalf("partition-only NIC was removed: %#v", nics[1])
	}
}

func TestSystemStatus(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"PowerState":"On","BootProgress":{"LastState":"OSRunning","LastStateTime":"2026-09-10T07:00:00Z"}}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.SystemStatus(context.Background(), "System.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if status.PowerState != "On" || status.BootProgress.LastState != "OSRunning" {
		t.Fatalf("unexpected status: %#v", status)
	}
}
