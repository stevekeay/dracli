package credentials

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
)

const (
	iterations = 100_000
	keyLength  = 15
)

// Resolve returns an explicitly supplied password first, then DRAC_PASSWORD,
// and otherwise derives the standard password using BMC_MASTER.
func Resolve(ipAddress, explicitPassword string, getenv func(string) string) (string, error) {
	if explicitPassword != "" {
		return explicitPassword, nil
	}
	if password := getenv("DRAC_PASSWORD"); password != "" {
		return password, nil
	}

	master := getenv("BMC_MASTER")
	if master == "" {
		return "", errors.New("BMC_MASTER must be set when --password and DRAC_PASSWORD are not provided")
	}
	return StandardPassword(ipAddress, master)
}

// StandardPassword implements the password derivation used by
// understack_workflows.bmc_password_standard.standard_password.
func StandardPassword(ipAddress, master string) (string, error) {
	ip := net.ParseIP(ipAddress)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("need an IPv4 address, not %q", ipAddress)
	}
	if master == "" {
		return "", errors.New("missing/empty master key")
	}

	derived, err := pbkdf2.Key(sha256.New, master+ipAddress, []byte("NaCl"), iterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("derive BMC password: %w", err)
	}
	return base64.StdEncoding.EncodeToString(derived), nil
}
