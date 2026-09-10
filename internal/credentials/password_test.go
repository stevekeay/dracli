package credentials

import "testing"

func TestStandardPasswordMatchesPythonImplementation(t *testing.T) {
	t.Parallel()

	got, err := StandardPassword("10.3.2.30", "ultra-secret string")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Vbyf7AFhiY2phtD1vcF0"; got != want {
		t.Fatalf("StandardPassword() = %q, want %q", got, want)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		return map[string]string{
			"DRAC_PASSWORD": "from-environment",
			"BMC_MASTER":    "master",
		}[key]
	}

	got, err := Resolve("10.3.2.30", "from-flag", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-flag" {
		t.Fatalf("Resolve() = %q, want explicit password", got)
	}

	got, err = Resolve("10.3.2.30", "", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-environment" {
		t.Fatalf("Resolve() = %q, want DRAC_PASSWORD", got)
	}
}

func TestResolveRequiresMasterForDerivation(t *testing.T) {
	t.Parallel()

	_, err := Resolve("10.3.2.30", "", func(string) string { return "" })
	if err == nil {
		t.Fatal("Resolve() returned no error")
	}
}

func TestStandardPasswordRejectsNonIPv4(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"elephants", "2001:db8::1"} {
		if _, err := StandardPassword(address, "master"); err == nil {
			t.Errorf("StandardPassword(%q) returned no error", address)
		}
	}
}
