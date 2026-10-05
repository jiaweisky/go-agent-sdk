package installation

import "testing"

func TestInstallationDefaults(t *testing.T) {
	if InstallationVersion == "" {
		t.Fatal("InstallationVersion is empty")
	}
	if InstallationChannel == "" {
		t.Fatal("InstallationChannel is empty")
	}
	if InstallationLocal != (InstallationChannel == "local") {
		t.Fatalf("InstallationLocal = %v, channel = %q", InstallationLocal, InstallationChannel)
	}
}
