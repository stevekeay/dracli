package redfish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDRACSettingsUsesOEMFallbackAndIncludesMissingKeys(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/redfish/v1/Managers/iDRAC.Embedded.1/Attributes":
			return jsonResponse(http.StatusNotFound, `{}`), nil
		case "/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1":
			return jsonResponse(http.StatusOK, `{"Attributes":{"SNMP.1.AgentEnable":"Enabled"}}`), nil
		case "/redfish/v1/Managers/iDRAC.Embedded.1/EthernetInterfaces":
			return jsonResponse(http.StatusOK, `{"Members":[{"HostName":"idrac-server01"}]}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := client.DRACSettings(context.Background(), "iDRAC.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Hostname != "idrac-server01" {
		t.Fatalf("hostname = %q", settings.Hostname)
	}
	if !settings.IncludeHostname {
		t.Fatal("iDRAC hostname was not enabled for output")
	}
	if settings.Attributes["SNMP.1.AgentEnable"] != "Enabled" {
		t.Fatalf("unexpected attributes: %#v", settings.Attributes)
	}
	if value, exists := settings.Attributes["IPv4.1.DNS1"]; !exists || value != nil {
		t.Fatalf("missing attribute was not represented as nil: %#v", settings.Attributes)
	}
}

func TestBIOSSettingsSelectsOnlyCuratedAttributes(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/redfish/v1/Systems/System.Embedded.1/Bios" {
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"Attributes":{"SecureBoot":"Disabled","HttpDev1TlsMode":"None","Unrelated":"ignored"}}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := client.BIOSSettings(context.Background(), "System.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Attributes) != len(biosSettingNames) {
		t.Fatalf("got %d attributes, want %d", len(settings.Attributes), len(biosSettingNames))
	}
	if settings.Attributes["SecureBoot"] != "Disabled" || settings.Attributes["Unrelated"] != nil {
		t.Fatalf("unexpected attributes: %#v", settings.Attributes)
	}
}

func TestBIOSSettingsSupportsAllAndNamedSelections(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"Attributes":{"SecureBoot":"Disabled","Unrelated":"included"}}`), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	all, err := client.AllBIOSSettings(context.Background(), "System.Embedded.1")
	if err != nil {
		t.Fatal(err)
	}
	if all.Attributes["Unrelated"] != "included" {
		t.Fatalf("all attributes = %#v", all.Attributes)
	}
	selected, err := client.SelectedBIOSSettings(context.Background(), "System.Embedded.1", []string{"Unrelated", "Missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Attributes) != 2 || selected.Attributes["Unrelated"] != "included" || selected.Attributes["Missing"] != nil {
		t.Fatalf("selected attributes = %#v", selected.Attributes)
	}
}

func TestSetBIOSSettingsPatchesPendingSettingsResource(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPatch {
			t.Fatalf("method = %s", request.Method)
		}
		if request.URL.Path != "/redfish/v1/Systems/System.Embedded.1/Bios/Settings" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var got attributesResource
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Attributes["SecureBoot"] != "Disabled" {
			t.Fatalf("payload = %s", body)
		}
		return jsonResponse(http.StatusNoContent, ``), nil
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetBIOSSettings(context.Background(), "System.Embedded.1", map[string]any{"SecureBoot": "Disabled"}); err != nil {
		t.Fatal(err)
	}
}

func TestSetDRACSettingsUsesDiscoveredAttributesResource(t *testing.T) {
	t.Parallel()

	oemPath := "/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/redfish/v1/Managers/iDRAC.Embedded.1/Attributes":
			return jsonResponse(http.StatusNotFound, `{}`), nil
		case request.URL.Path == oemPath && request.Method == http.MethodGet:
			return jsonResponse(http.StatusOK, `{"Attributes":{}}`), nil
		case request.URL.Path == oemPath && request.Method == http.MethodPatch:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"Attributes":{"NTPConfigGroup.1.NTPEnable":"Enabled"}}` {
				t.Fatalf("payload = %s", body)
			}
			return jsonResponse(http.StatusOK, `{}`), nil
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
			return nil, nil
		}
	})}
	client, err := NewClient("https://bmc.example", "root", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetDRACSettings(context.Background(), "iDRAC.Embedded.1", map[string]any{"NTPConfigGroup.1.NTPEnable": "Enabled"}); err != nil {
		t.Fatal(err)
	}
}
