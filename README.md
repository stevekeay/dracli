# dracli

`dracli` is a small CLI for operational tasks against Dell iDRAC Redfish APIs.

## Build

```sh
/home/steve.keay/.asdf/shims/go build -o dracli ./cmd/dracli
```

## Lifecycle Controller logs

By default, `dracli` derives the BMC password from `BMC_MASTER` using the same
PBKDF2 scheme as `understack_workflows.bmc_password_standard`:

```sh
export BMC_MASTER='...'
./dracli lc-logs --insecure 10.46.96.160
```

The credential precedence is:

1. `--password`
2. `DRAC_PASSWORD`
3. A derived password using `BMC_MASTER`

For example:

```sh
./dracli lc-logs --insecure --password 'plain-text-password' 10.46.96.160
DRAC_PASSWORD='plain-text-password' ./dracli lc-logs --insecure 10.46.96.160
./dracli lc-logs --insecure --output json 10.46.96.160
```

The default username is `root`; override it with `--username` or
`DRAC_USERNAME`. TLS certificates are verified unless `--insecure` is supplied.
Be aware that command-line passwords may be visible to other local users via
the process list; `DRAC_PASSWORD` is preferable for ad-hoc use.
