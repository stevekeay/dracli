# dracli

`dracli` is a small CLI for operational tasks against Dell iDRAC Redfish APIs.

Run `./dracli --help` for the full command guide. TLS certificate verification
is disabled by default because iDRACs commonly use self-signed certificates.
Use the generic `--verify-tls` option before or after any command to validate
the certificate and hostname:

```sh
./dracli --verify-tls status x.x.x.x
./dracli status --verify-tls x.x.x.x
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
./dracli logs x.x.x.x
```

The credential precedence is:

1. `--password`
2. `DRAC_PASSWORD`
3. A derived password using `BMC_MASTER`

For example:

```sh
./dracli logs --password 'plain-text-password' x.x.x.x
DRAC_PASSWORD='plain-text-password' ./dracli logs x.x.x.x
./dracli logs --output json x.x.x.x
./dracli logs --all x.x.x.x
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
./dracli query x.x.x.x
./dracli query --output json x.x.x.x
./dracli x.x.x.x # query is the default command

./dracli status x.x.x.x
./dracli status --monitor x.x.x.x
./dracli status --monitor --interval 10s x.x.x.x
```

`query` is the fast overview: it makes only the system and manager requests and
reports the Dell service tag, server and iDRAC models, firmware, BIOS, memory,
CPU, current power and boot-progress status, and clock agreement. `inventory`
adds the slower RAID and NIC collection traversal. NIC output includes make,
model, slot, MAC addresses,
link and speed when supplied by Redfish, plus Dell Connection View LLDP switch
and port data when enabled by the iDRAC.

The iDRAC hardware version (for example, `iDRAC9` or `16G Monolithic`) comes
from the Manager resource's standard Redfish `Model` property; its software
version comes from `FirmwareVersion`.

The clock comparison accounts for RFC 3339 timezone offsets and prints a warning
when drift exceeds 60 seconds. If one Redfish section cannot be decoded, it is marked
`UNABLE TO PARSE REDFISH RESPONSE` while successfully decoded sections remain
visible.

`status --monitor` prints the current power and boot-progress state, then polls
every five seconds by default and prints only changes. JSON monitor output is
newline-delimited JSON, one observation per changed state.

## Job queue

```sh
./dracli jobs x.x.x.x
./dracli jobs --output json x.x.x.x
./dracli clear-jobs x.x.x.x
```

`jobs` displays current job IDs, state, progress, type, message, and available
timestamps. `clear-jobs` deletes the complete queue using Dell's
`DellJobService.DeleteJobQueue` action with `JID_CLEARALL`; it does not restart
Lifecycle Controller services. Clearing the queue cannot be undone.

## Factory reset

```sh
./dracli factory-reset --yes x.x.x.x
```

`factory-reset` uses Dell's `DellManager.ResetToDefaults` action with reset type
`Default`. This restores iDRAC settings to factory defaults while retaining the
network configuration and user accounts, so the current address and credentials
continue to work. The disruptive operation is rejected unless `--yes` is given.

## Settings

```sh
./dracli settings x.x.x.x
./dracli settings drac x.x.x.x
./dracli settings drac --all x.x.x.x
./dracli settings bios --name SecureBoot --name TimeZone x.x.x.x
```

With no namespace, `settings` reports the curated iDRAC and BIOS attributes.
Select a namespace for only that group, use `--all` for every attribute, or
repeat `--name` to request particular attributes. An unsupported attribute is
shown as `not reported` in text output and `null` in JSON.

To change settings, explicitly select `drac` or `bios` and repeat `--set` as
needed. Values are parsed as JSON when possible, so numbers and booleans keep
their types; ordinary unquoted values remain strings.

```sh
./dracli settings bios --set SecureBoot=Disabled x.x.x.x
./dracli settings drac --set SNMP.1.AlertPort=161 x.x.x.x
```

Redfish may stage BIOS changes until the next reboot. The command reports that
the controller accepted the update; it does not imply that a pending BIOS
value is already active.
