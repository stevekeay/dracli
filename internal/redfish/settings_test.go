package redfish

import (
	"context"
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
