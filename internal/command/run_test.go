package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/stevekeay/dracli/internal/redfish"
)

func TestWriteEntriesText(t *testing.T) {
	t.Parallel()

	entries := []json.RawMessage{
		json.RawMessage(`{"Created":"2026-09-10T07:00:00Z","Message":"Configuration job completed"}`),
	}
	var output bytes.Buffer
	if err := writeEntries(&output, entries, "text"); err != nil {
		t.Fatal(err)
	}
	if want := "2026-09-10T07:00:00Z Configuration job completed\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestMissingMasterIsReported(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"logs", "10.46.96.160"}, &stdout, &stderr, func(string) string { return "" })
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if exitCode := Run([]string{"help"}, &stdout, &stderr, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "Global options:") ||
		!strings.Contains(stdout.String(), "settings bios") ||
		!strings.Contains(stdout.String(), "Password precedence") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHelpFlagAliasesHelpCommand(t *testing.T) {
	t.Parallel()

	var helpOutput bytes.Buffer
	if exitCode := Run([]string{"--help"}, &helpOutput, io.Discard, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	var commandOutput bytes.Buffer
	if exitCode := Run([]string{"help"}, &commandOutput, io.Discard, func(string) string { return "" }); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if helpOutput.String() != commandOutput.String() {
		t.Fatal("--help and help produced different output")
	}
}

func TestInsecureMayPrecedeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "simple command", args: []string{"--insecure", "status", "10.46.96.160"}, want: []string{"status", "--insecure", "10.46.96.160"}},
		{name: "settings subcommand", args: []string{"--insecure", "settings", "bios", "10.46.96.160"}, want: []string{"settings", "bios", "--insecure", "10.46.96.160"}},
		{name: "TLS verification", args: []string{"--verify-tls", "query", "10.46.96.160"}, want: []string{"query", "--verify-tls", "10.46.96.160"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeGlobalArgs(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("normalizeGlobalArgs() = %#v, want %#v", got, test.want)
			}
		})
	}

	var stderr bytes.Buffer
	exitCode := Run([]string{"--insecure", "status", "10.46.96.160"}, io.Discard, &stderr, func(string) string { return "" })
	if exitCode != 1 || !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
}

func TestIPv4AddressWithoutCommandDefaultsToQuery(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := Run([]string{"10.46.96.160"}, io.Discard, &stderr, func(string) string { return "" })
	if exitCode != 1 || !strings.Contains(stderr.String(), "BMC_MASTER must be set") {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}

	args, err := normalizeGlobalArgs([]string{"--verify-tls", "10.46.96.160"})
	if err != nil {
		t.Fatal(err)
	}
	if net.ParseIP(args[0]) == nil {
		t.Fatalf("normalized commandless arguments = %#v", args)
	}
}

func TestPromptForNextLogPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{input: "\n", want: true},
		{input: "anything\n", want: true},
		{input: "Q\n", want: false},
		{input: "", want: false},
	}
	for _, test := range tests {
		var prompt bytes.Buffer
		got := promptForNextLogPage(bufio.NewReader(strings.NewReader(test.input)), &prompt)
		if got != test.want {
			t.Errorf("input %q: got %v, want %v", test.input, got, test.want)
		}
		if !strings.Contains(prompt.String(), "next page") {
			t.Errorf("input %q: prompt = %q", test.input, prompt.String())
		}
	}
}

func TestWriteInventoryReportsPartialFailureAndClockWarning(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System: redfish.SystemSummary{Manufacturer: "Dell", Model: "PowerEdge"},
		Clock:  redfish.ClockSummary{DriftSeconds: -75, Significant: true},
		Errors: map[string]string{"raid_controllers": redfish.UnableToParseRedfishResponse},
	}
	var output bytes.Buffer
	if err := writeInventory(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "System: Dell PowerEdge") ||
		!strings.Contains(output.String(), "*** WARNING:") ||
		!strings.Contains(output.String(), "RAID controllers:\n  UNABLE TO PARSE REDFISH RESPONSE") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestWriteQueryIncludesStatusWithoutDetailedInventory(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System:       redfish.SystemSummary{Manufacturer: "Dell", Model: "PowerEdge"},
		SerialNumber: "ABC1234D",
		IDRAC:        redfish.FirmwareSummary{Model: "iDRAC9", Version: "7.20"},
		Status: redfish.SystemStatus{
			PowerState: "On",
			BootProgress: redfish.BootProgress{
				LastState: "OSRunning", LastStateTime: "2026-09-10T07:00:00Z",
			},
		},
		Clock: redfish.ClockSummary{DriftSeconds: 1},
	}
	var output bytes.Buffer
	if err := writeQuery(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Serial Number: ABC1234D") ||
		!strings.Contains(output.String(), "iDRAC: iDRAC9 7.20") ||
		!strings.Contains(output.String(), "Power state: On") ||
		!strings.Contains(output.String(), "Boot progress: OSRunning at 2026-09-10T07:00:00Z") {
		t.Fatalf("output = %q", output.String())
	}
	if strings.Contains(output.String(), "RAID controllers:") || strings.Contains(output.String(), "NICs:") {
		t.Fatalf("query included slow inventory sections: %q", output.String())
	}
	var jsonOutput bytes.Buffer
	if err := writeQuery(&jsonOutput, inventory, "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"hardware_version": "iDRAC9"`) ||
		!strings.Contains(jsonOutput.String(), `"firmware_version": "7.20"`) {
		t.Fatalf("JSON output = %q", jsonOutput.String())
	}
}

func TestQuerySystemSummaryOrder(t *testing.T) {
	t.Parallel()

	inventory := redfish.Inventory{
		System:       redfish.SystemSummary{Manufacturer: "Dell Inc.", Model: "PowerEdge XE8640"},
		SerialNumber: "ABC1234D",
		Memory:       redfish.MemorySummary{TotalGiB: 2048},
		CPU:          redfish.ProcessorSummary{Count: 2, Model: "Intel Xeon", Cores: 96, Threads: 192},
		BIOSVersion:  "2.8.2",
		IDRAC:        redfish.FirmwareSummary{Model: "16G Monolithic", Version: "7.30.10.50"},
	}
	var output bytes.Buffer
	if err := writeQuery(&output, inventory, "text"); err != nil {
		t.Fatal(err)
	}
	want := []string{"System:", "Serial Number:", "Memory:", "CPU:", "BIOS:", "iDRAC:"}
	previous := -1
	for _, label := range want {
		index := strings.Index(output.String(), label)
		if index <= previous {
			t.Fatalf("%s is out of order in %q", label, output.String())
		}
		previous = index
	}
}

func TestTLSVerificationOverridesInsecureDefault(t *testing.T) {
	t.Parallel()

	if !skipTLSVerification(false, false) {
		t.Fatal("default should skip TLS verification")
	}
	if skipTLSVerification(true, true) {
		t.Fatal("--verify-tls should enable verification")
	}
}

func TestWriteSettingsPreservesCuratedOrderAndReportsHostname(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	settings := redfish.SettingsSelection{
		Attributes:      map[string]any{"second": nil, "first": "value"},
		Order:           []string{"first", "second"},
		IncludeHostname: true,
	}
	if err := writeSettings(&output, settings, "text"); err != nil {
		t.Fatal(err)
	}
	want := "Hostname: unknown\nfirst: value\nsecond: not reported\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestWriteJobs(t *testing.T) {
	t.Parallel()

	jobs := []redfish.Job{{
		ID: "JID_1", State: "Scheduled", Type: "Configuration",
		PercentComplete: 20, Message: "Task successfully scheduled.",
	}}
	var output bytes.Buffer
	if err := writeJobs(&output, jobs, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "JID_1 state=Scheduled progress=20% type=Configuration") ||
		!strings.Contains(output.String(), "message=Task successfully scheduled.") {
		t.Fatalf("output = %q", output.String())
	}
	output.Reset()
	if err := writeJobs(&output, nil, "text"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "No jobs in queue.\n" {
		t.Fatalf("empty output = %q", output.String())
	}
}

func TestParseSettingsAssignments(t *testing.T) {
	t.Parallel()

	values, err := parseSettingsAssignments([]string{
		"SecureBoot=Disabled",
		"SNMP.1.AlertPort=161",
		`Label="maintenance host"`,
		"Enabled=true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["SecureBoot"] != "Disabled" || values["SNMP.1.AlertPort"] != float64(161) ||
		values["Label"] != "maintenance host" || values["Enabled"] != true {
		t.Fatalf("values = %#v", values)
	}
	if _, err := parseSettingsAssignments([]string{"SecureBoot"}); err == nil {
		t.Fatal("assignment without equals sign was accepted")
	}
}

func TestWriteSettingsGroups(t *testing.T) {
	t.Parallel()

	drac := redfish.SettingsSelection{Hostname: "idrac01", IncludeHostname: true, Attributes: map[string]any{"NTP": "Enabled"}}
	bios := redfish.SettingsSelection{Attributes: map[string]any{"SecureBoot": "Disabled"}}
	var output bytes.Buffer
	if err := writeSettingsGroups(&output, drac, bios, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "iDRAC settings:\nHostname: idrac01") ||
		!strings.Contains(output.String(), "BIOS settings:\nSecureBoot: Disabled") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestSettingsRejectsAmbiguousOrConflictingModesBeforeConnecting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"settings", "--set", "SecureBoot=Disabled", "10.46.96.160"}, want: "requires an explicit settings namespace"},
		{args: []string{"settings", "bios", "--all", "--name", "SecureBoot", "10.46.96.160"}, want: "--all and --name cannot be used together"},
	}
	for _, test := range tests {
		var stderr bytes.Buffer
		if exitCode := Run(test.args, io.Discard, &stderr, func(string) string { return "" }); exitCode != 1 {
			t.Fatalf("args %v: exit code = %d", test.args, exitCode)
		}
		if !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("args %v: stderr = %q", test.args, stderr.String())
		}
	}
}
