# dracli

`dracli` is a small CLI for operational tasks against Dell iDRAC Redfish APIs.

Run `./dracli --help` for the full command guide. TLS certificate verification
is disabled by default because iDRACs commonly use self-signed certificates.
Use the generic `--verify-tls` option before or after any command to validate
the certificate and hostname:

```sh
./dracli --verify-tls status 10.46.96.160
./dracli status --verify-tls 10.46.96.160
```

`--insecure` remains accepted as an explicit spelling of the default.

## Build

```sh
/home/steve.keay/.asdf/shims/go build -o dracli ./cmd/dracli
```

## Lifecycle Controller logs

By default, `dracli` derives the BMC password from `BMC_MASTER` using the same
PBKDF2 scheme as `understack_workflows.bmc_password_standard`:

```sh
export BMC_MASTER='...'
./dracli logs 10.46.96.160
```

The credential precedence is:

1. `--password`
2. `DRAC_PASSWORD`
3. A derived password using `BMC_MASTER`

For example:

```sh
./dracli logs --password 'plain-text-password' 10.46.96.160
DRAC_PASSWORD='plain-text-password' ./dracli logs 10.46.96.160
./dracli logs --output json 10.46.96.160
./dracli logs --all 10.46.96.160
```

`logs` fetches one page by default. At a terminal it offers to fetch the next
page; when output is redirected it stops after page one and prints an example
showing how to add `--all`. JSON output also requires `--all` for multi-page
results so the command emits one valid JSON array.

The default username is `root`; override it with `--username` or
`DRAC_USERNAME`. TLS certificates are only verified when `--verify-tls` is supplied.
Be aware that command-line passwords may be visible to other local users via
the process list; `DRAC_PASSWORD` is preferable for ad-hoc use.

## Inventory and system state

```sh
./dracli query 10.46.96.160
./dracli query --output json 10.46.96.160
./dracli 10.46.96.160 # query is the default command

./dracli status 10.46.96.160
./dracli status --monitor 10.46.96.160
./dracli status --monitor --interval 10s 10.46.96.160
```

`inventory` (also available as `query`) reports the server and iDRAC models,
firmware, BIOS, memory, CPU, RAID controllers, and NIC FQDDs. NIC output includes
make, model, slot, MAC addresses, link and speed when supplied by Redfish, plus
Dell Connection View LLDP switch and port data when enabled by the iDRAC.
It also compares the iDRAC `DateTime` with local system time, accounting for
RFC 3339 timezone offsets, and prints a warning when drift exceeds 60
seconds. If one Redfish section cannot be decoded, that section is marked
`UNABLE TO PARSE REDFISH RESPONSE` while successfully decoded sections remain
visible.

`status --monitor` prints the current power and boot-progress state, then polls
every five seconds by default and prints only changes. JSON monitor output is
newline-delimited JSON, one observation per changed state.

## Settings

```sh
./dracli settings 10.46.96.160
./dracli settings drac 10.46.96.160
./dracli settings drac --all 10.46.96.160
./dracli settings bios --name SecureBoot --name TimeZone 10.46.96.160
```

With no namespace, `settings` reports the curated iDRAC and BIOS attributes.
Select a namespace for only that group, use `--all` for every attribute, or
repeat `--name` to request particular attributes. An unsupported attribute is
shown as `not reported` in text output and `null` in JSON.

To change settings, explicitly select `drac` or `bios` and repeat `--set` as
needed. Values are parsed as JSON when possible, so numbers and booleans keep
their types; ordinary unquoted values remain strings.

```sh
./dracli settings bios --set SecureBoot=Disabled 10.46.96.160
./dracli settings drac --set SNMP.1.AlertPort=161 10.46.96.160
```

Redfish may stage BIOS changes until the next reboot. The command reports that
the controller accepted the update; it does not imply that a pending BIOS
value is already active.
