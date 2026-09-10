package command

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/stevekeay/dracli/internal/credentials"
	"github.com/stevekeay/dracli/internal/redfish"
)

const usage = `Usage:
  dracli <command> [options] <BMC IPv4 address>

Commands:
	  lc-logs    Fetch Lifecycle Controller log entries
	  inventory  Query a concise hardware and firmware inventory
	  query      Alias for inventory
	  status     Query power and boot progress; optionally monitor changes
	  settings   Show curated iDRAC or BIOS settings

Run "dracli <command> -help" for command options.
`

func Run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}

	switch args[0] {
	case "help", "-help", "--help", "-h":
		_, _ = io.WriteString(stdout, usage)
		return 0
	case "lc-logs":
		if err := runLCLogs(args[1:], stdout, stderr, getenv); err != nil {
			fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "inventory", "query":
		if err := runInventory(args[1:], stdout, stderr, getenv); err != nil {
			fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "status":
		if err := runStatus(args[1:], stdout, stderr, getenv); err != nil {
			fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	case "settings":
		if err := runSettings(args[1:], stdout, stderr, getenv); err != nil {
			fmt.Fprintf(stderr, "dracli: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "dracli: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runLCLogs(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("lc-logs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: dracli lc-logs [options] <BMC IPv4 address>")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	insecure := flags.Bool("insecure", false, "skip TLS certificate verification")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, *insecure, *timeout, getenv)
	if err != nil {
		return err
	}

	entries, err := client.LifecycleLogs(context.Background(), *manager)
	if err != nil {
		return err
	}
	return writeEntries(stdout, entries, *output)
}

func runInventory(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: dracli inventory [options] <BMC IPv4 address>")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	insecure := flags.Bool("insecure", false, "skip TLS certificate verification")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, *insecure, *timeout, getenv)
	if err != nil {
		return err
	}
	inventory, err := client.Inventory(context.Background(), *system, *manager)
	if err != nil {
		return err
	}
	return writeInventory(stdout, inventory, *output)
}

func runStatus(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: dracli status [options] <BMC IPv4 address>")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	insecure := flags.Bool("insecure", false, "skip TLS certificate verification")
	output := flags.String("output", "text", "output format: text or json")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	monitor := flags.Bool("monitor", false, "poll and report power or boot-progress changes")
	interval := flags.Duration("interval", 5*time.Second, "monitor polling interval")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}
	if *interval <= 0 {
		return errors.New("interval must be greater than zero")
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, *insecure, *timeout, getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	current, err := client.SystemStatus(ctx, *system)
	if err != nil {
		return err
	}
	if err := writeStatus(stdout, current, time.Now(), *output, *monitor); err != nil {
		return err
	}
	if !*monitor {
		return nil
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case observedAt := <-ticker.C:
			next, err := client.SystemStatus(ctx, *system)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if next != current {
				if err := writeStatus(stdout, next, observedAt, *output, true); err != nil {
					return err
				}
				current = next
			}
		}
	}
}

func runSettings(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 0 || (args[0] != "drac" && args[0] != "bios") {
		return errors.New("usage: dracli settings <drac|bios> [options] <BMC IPv4 address>")
	}
	kind := args[0]
	flags := flag.NewFlagSet("settings "+kind, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: dracli settings %s [options] <BMC IPv4 address>\n\n", kind)
		flags.PrintDefaults()
	}
	password := flags.String("password", "", "BMC password (otherwise DRAC_PASSWORD or derived using BMC_MASTER)")
	username := flags.String("username", envOrDefault(getenv, "DRAC_USERNAME", "root"), "BMC username")
	insecure := flags.Bool("insecure", false, "skip TLS certificate verification")
	output := flags.String("output", "text", "output format: text or json")
	manager := flags.String("manager", "iDRAC.Embedded.1", "Redfish manager identifier")
	system := flags.String("system", "System.Embedded.1", "Redfish system identifier")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one BMC IPv4 address is required")
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("invalid output format %q: use text or json", *output)
	}

	client, err := newRedfishClient(flags.Arg(0), *username, *password, *insecure, *timeout, getenv)
	if err != nil {
		return err
	}
	var settings redfish.SettingsSelection
	if kind == "drac" {
		settings, err = client.DRACSettings(context.Background(), *manager)
	} else {
		settings, err = client.BIOSSettings(context.Background(), *system)
	}
	if err != nil {
		return err
	}
	return writeSettings(stdout, settings, *output)
}

func newRedfishClient(host, username, password string, insecure bool, timeout time.Duration, getenv func(string) string) (*redfish.Client, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("need an IPv4 address, not %q", host)
	}
	resolvedPassword, err := credentials.Resolve(host, password, getenv)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec -- explicitly requested by the operator
	httpClient := &http.Client{Transport: transport, Timeout: timeout}
	return redfish.NewClient("https://"+host, username, resolvedPassword, httpClient)
}

func writeInventory(output io.Writer, inventory redfish.Inventory, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(inventory); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	lines := []string{
		fmt.Sprintf("System: %s", joinKnown(inventory.System.Manufacturer, inventory.System.Model)),
		fmt.Sprintf("iDRAC: %s", joinKnown(inventory.IDRAC.Model, inventory.IDRAC.Version)),
		fmt.Sprintf("BIOS: %s", known(inventory.BIOSVersion)),
		fmt.Sprintf("Memory: %g GiB", inventory.Memory.TotalGiB),
		fmt.Sprintf("CPU: %d x %s (%d cores, %d threads)", inventory.CPU.Count, known(inventory.CPU.Model), inventory.CPU.Cores, inventory.CPU.Threads),
		"RAID controllers:",
	}
	if len(inventory.RAIDControllers) == 0 {
		lines = append(lines, "  none reported")
	}
	for _, controller := range inventory.RAIDControllers {
		details := joinKnown(controller.Manufacturer, controller.Model)
		if controller.Firmware != "" {
			details += " firmware " + controller.Firmware
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", known(controller.ID), known(details)))
	}
	lines = append(lines, "NICs:")
	if len(inventory.NICs) == 0 {
		lines = append(lines, "  none reported")
	}
	for _, nic := range inventory.NICs {
		parts := []string{joinKnown(nic.Manufacturer, nic.Model)}
		if len(nic.MACAddresses) > 0 {
			parts = append(parts, "MAC "+strings.Join(nic.MACAddresses, ","))
		}
		if nic.Link != "" {
			parts = append(parts, "link "+nic.Link)
		}
		if nic.SpeedMbps != nil {
			parts = append(parts, "speed "+formatSpeed(*nic.SpeedMbps))
		}
		if nic.LLDP != nil {
			parts = append(parts, "LLDP "+joinKnown(nic.LLDP.SwitchID, nic.LLDP.SwitchPort))
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", nic.ID, strings.Join(nonempty(parts), "; ")))
	}
	if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return nil
}

type observedStatus struct {
	ObservedAt   string               `json:"observed_at"`
	PowerState   string               `json:"power_state,omitempty"`
	BootProgress redfish.BootProgress `json:"boot_progress"`
}

func writeStatus(output io.Writer, status redfish.SystemStatus, observedAt time.Time, format string, monitoring bool) error {
	observation := observedStatus{
		ObservedAt:   observedAt.UTC().Format(time.RFC3339),
		PowerState:   status.PowerState,
		BootProgress: status.BootProgress,
	}
	if format == "json" {
		encoder := json.NewEncoder(output)
		if !monitoring {
			encoder.SetIndent("", "  ")
		}
		if err := encoder.Encode(observation); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	bootTime := ""
	if status.BootProgress.LastStateTime != "" {
		bootTime = " boot_time=" + status.BootProgress.LastStateTime
	}
	if _, err := fmt.Fprintf(output, "%s power=%s boot=%s%s\n", observation.ObservedAt, known(status.PowerState), known(status.BootProgress.LastState), bootTime); err != nil {
		return fmt.Errorf("write text output: %w", err)
	}
	return nil
}

func writeSettings(output io.Writer, settings redfish.SettingsSelection, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(settings); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}
	if settings.Hostname != "" {
		if _, err := fmt.Fprintf(output, "Hostname: %s\n", settings.Hostname); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	keys := make([]string, 0, len(settings.Attributes))
	for key := range settings.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := settings.Attributes[key]
		if value == nil {
			value = "not reported"
		}
		if _, err := fmt.Fprintf(output, "%s: %v\n", key, value); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func known(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func joinKnown(values ...string) string {
	joined := strings.Join(nonempty(values), " ")
	return known(joined)
}

func nonempty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && value != "unknown" {
			result = append(result, value)
		}
	}
	return result
}

func formatSpeed(mbps int) string {
	if mbps >= 1_000 && mbps%1_000 == 0 {
		return fmt.Sprintf("%d Gbps", mbps/1_000)
	}
	return fmt.Sprintf("%d Mbps", mbps)
}

func writeEntries(output io.Writer, entries []json.RawMessage, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(entries); err != nil {
			return fmt.Errorf("write JSON output: %w", err)
		}
		return nil
	}

	for _, raw := range entries {
		var entry redfish.LogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode lifecycle log entry: %w", err)
		}
		if _, err := fmt.Fprintf(output, "%s %s\n", entry.Created, entry.Message); err != nil {
			return fmt.Errorf("write text output: %w", err)
		}
	}
	return nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}
