package profiles

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"bootimus/internal/models"
)

func TestMatchProfile_RealISOFilenames(t *testing.T) {
	profiles := loadEmbeddedForTest(t)

	tests := []struct {
		filename string
		wantID   string
	}{
		{"ubuntu-24.04.1-desktop-amd64.iso", "ubuntu"},
		{"kubuntu-24.04-desktop-amd64.iso", "ubuntu"},
		{"linuxmint-22-cinnamon-64bit.iso", "mint"},
		{"pop-os_22.04_amd64_intel_54.iso", "popos"},
		{"debian-12.7.0-amd64-netinst.iso", "debian"},
		{"proxmox-ve_8.2-1.iso", "proxmox"},
		{"archlinux-2025.04.01-x86_64.iso", "arch"},
		{"cachyos-desktop-linux-250101.iso", "arch"},
		{"manjaro-kde-24.0.0-240416-linux69.iso", "manjaro"},
		{"Fedora-Workstation-Live-x86_64-41-1.4.iso", "fedora"},
		{"Rocky-9.4-x86_64-minimal.iso", "rocky"},
		{"AlmaLinux-9.4-x86_64-minimal.iso", "alma"},
		{"openSUSE-Leap-15.6-DVD-x86_64-Media.iso", "opensuse"},
		{"alpine-standard-3.20.3-x86_64.iso", "alpine"},
		{"kali-linux-2024.3-installer-amd64.iso", "kali"},
		{"systemrescue-11.00-amd64.iso", "systemrescue"},
		{"Win11_24H2_English_x64.iso", "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, err := matchProfile(profiles, tt.filename)
			if err != nil {
				t.Fatalf("matchProfile(%q) returned error: %v", tt.filename, err)
			}
			if got.ProfileID != tt.wantID {
				t.Errorf("matchProfile(%q) = %s, want %s", tt.filename, got.ProfileID, tt.wantID)
			}
		})
	}
}

func TestMatchProfile_NoMatch(t *testing.T) {
	profiles := loadEmbeddedForTest(t)
	_, err := matchProfile(profiles, "totally-unknown-os-1.0.iso")
	if err == nil {
		t.Fatal("expected error for unknown filename, got nil")
	}
}

func TestMatchProfile_CaseInsensitive(t *testing.T) {
	profiles := loadEmbeddedForTest(t)
	got, err := matchProfile(profiles, "UBUNTU-24.04-DESKTOP.ISO")
	if err != nil {
		t.Fatalf("matchProfile error: %v", err)
	}
	if got.ProfileID != "ubuntu" {
		t.Errorf("got %s, want ubuntu", got.ProfileID)
	}
}

func TestMatchProfile_CustomBeatsBuiltin(t *testing.T) {
	profiles := []*models.DistroProfile{
		{ProfileID: "ubuntu", Custom: false, FilenamePatterns: models.StringSlice{"ubuntu"}},
		{ProfileID: "my-custom-ubuntu", Custom: true, FilenamePatterns: models.StringSlice{"ubuntu"}},
	}
	got, err := matchProfile(profiles, "ubuntu-24.04.iso")
	if err != nil {
		t.Fatalf("matchProfile error: %v", err)
	}
	if got.ProfileID != "my-custom-ubuntu" {
		t.Errorf("got %s, want my-custom-ubuntu (custom must beat built-in)", got.ProfileID)
	}
}

func TestMatchProfile_PatternBeatsIDMatch(t *testing.T) {
	profiles := []*models.DistroProfile{
		{ProfileID: "debian", Custom: false, FilenamePatterns: models.StringSlice{"debian"}},
		{ProfileID: "kali", Custom: false, FilenamePatterns: models.StringSlice{"kali"}},
	}
	got, err := matchProfile(profiles, "kali-debian-derivative-2024.iso")
	if err != nil {
		t.Fatalf("matchProfile error: %v", err)
	}
	if got.ProfileID != "debian" {
		t.Errorf("got %s, want debian (first pattern match in slice wins)", got.ProfileID)
	}
}

func TestMatchProfile_FallsBackToFamily(t *testing.T) {
	profiles := []*models.DistroProfile{
		{ProfileID: "obscure", Family: "redhat", FilenamePatterns: models.StringSlice{"obscure"}},
	}
	got, err := matchProfile(profiles, "some-redhat-derivative.iso")
	if err != nil {
		t.Fatalf("matchProfile error: %v", err)
	}
	if got.ProfileID != "obscure" {
		t.Errorf("got %s, want obscure via family match", got.ProfileID)
	}
}

func TestProxmoxProfile_AutomatedInstallBootParams(t *testing.T) {
	profiles := loadEmbeddedForTest(t)

	var proxmox *models.DistroProfile
	for _, p := range profiles {
		if p.ProfileID == "proxmox" {
			proxmox = p
			break
		}
	}
	if proxmox == nil {
		t.Fatal("expected an embedded 'proxmox' distro profile")
	}

	if !strings.Contains(proxmox.DefaultBootParams, "proxmox-start-auto-installer") {
		t.Errorf("expected default boot params to trigger the automated installer, got %q", proxmox.DefaultBootParams)
	}
	if proxmox.IsoInitrdName == "" {
		t.Error("expected the proxmox profile to chain-load the source ISO as an extra initrd module")
	}
	for _, path := range []string{"/boot/linux26"} {
		if !containsString(proxmox.KernelPaths, path) {
			t.Errorf("expected kernel_paths to contain %q, got %v", path, proxmox.KernelPaths)
		}
	}
	for _, path := range []string{"/boot/initrd.img"} {
		if !containsString(proxmox.InitrdPaths, path) {
			t.Errorf("expected initrd_paths to contain %q, got %v", path, proxmox.InitrdPaths)
		}
	}
}

func TestBuildIsoInitrdNameMapNormalizesIDsAndRejectsUnsafeNames(t *testing.T) {
	got := buildIsoInitrdNameMap([]*models.DistroProfile{
		{ProfileID: "  ProxMoX  ", IsoInitrdName: "proxmox.iso"},
		{ProfileID: "debian", IsoInitrdName: "debian-initrd.img"},
		{ProfileID: "unsafe-space", IsoInitrdName: "proxmox iso"},
		{ProfileID: "unsafe-newline", IsoInitrdName: "proxmox\niso"},
		{ProfileID: "unsafe-punctuation", IsoInitrdName: "proxmox;reboot"},
		{ProfileID: "unsafe-path", IsoInitrdName: "proxmox/iso"},
		{ProfileID: "dot", IsoInitrdName: "."},
		{ProfileID: "dot-dot", IsoInitrdName: ".."},
		{ProfileID: "punctuation-only", IsoInitrdName: "._-"},
		{ProfileID: "hyphen-underscore", IsoInitrdName: "-_"},
		{ProfileID: "max-length", IsoInitrdName: strings.Repeat("a", maxIsoInitrdNameLength)},
		{ProfileID: "over-max-length", IsoInitrdName: strings.Repeat("a", maxIsoInitrdNameLength+1)},
		nil,
	})

	if len(got) != 3 {
		t.Fatalf("expected only safe initrd module names to be cached, got %#v", got)
	}
	if got["proxmox"] != "proxmox.iso" {
		t.Errorf("expected normalized proxmox key with safe name, got %#v", got)
	}
	if got["debian"] != "debian-initrd.img" {
		t.Errorf("expected safe Debian module name, got %#v", got)
	}
	if got["max-length"] != strings.Repeat("a", maxIsoInitrdNameLength) {
		t.Errorf("expected name at maximum length to be accepted, got %#v", got)
	}
}

func TestBuildIsoInitrdNameMapCollisionIsDeterministic(t *testing.T) {
	profiles := []*models.DistroProfile{
		{ProfileID: " Proxmox ", IsoInitrdName: "spaced.iso"},
		{ProfileID: "Proxmox", IsoInitrdName: "mixed.iso"},
		{ProfileID: "proxmox", IsoInitrdName: "canonical.iso"},
		{ProfileID: " Debian ", IsoInitrdName: "debian-spaced.iso"},
		{ProfileID: "DEBIAN", IsoInitrdName: "debian-upper.iso"},
	}

	forward := buildIsoInitrdNameMap(profiles)
	reversed := make([]*models.DistroProfile, len(profiles))
	for i := range profiles {
		reversed[len(profiles)-1-i] = profiles[i]
	}
	backward := buildIsoInitrdNameMap(reversed)

	if !reflect.DeepEqual(forward, backward) {
		t.Fatalf("expected collision handling to be independent of profile order: forward=%v backward=%v", forward, backward)
	}
	if forward["proxmox"] != "canonical.iso" {
		t.Errorf("expected exact normalized profile ID to win collision, got %q", forward["proxmox"])
	}
	if forward["debian"] != "debian-spaced.iso" {
		t.Errorf("expected lexicographically smallest noncanonical ID to win collision, got %q", forward["debian"])
	}
}

func TestNilManagerIsoInitrdAccessors(t *testing.T) {
	var manager *Manager

	if got := manager.IsoInitrdName("proxmox"); got != "" {
		t.Errorf("IsoInitrdName on nil manager = %q, want empty string", got)
	}
	if got := manager.IsoInitrdNameMap(); got != nil {
		t.Errorf("IsoInitrdNameMap on nil manager = %#v, want nil", got)
	}
}

func TestIsoInitrdNameMapReturnsDefensiveCopy(t *testing.T) {
	manager := &Manager{
		isoInitrdNames:  map[string]string{"proxmox": "proxmox.iso"},
		isoInitrdLoaded: true,
	}

	got := manager.IsoInitrdNameMap()
	got["proxmox"] = "unsafe-change.iso"

	if name := manager.IsoInitrdName("proxmox"); name != "proxmox.iso" {
		t.Fatalf("mutating returned map changed cached module name to %q", name)
	}
}

func containsString(haystack models.StringSlice, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func loadEmbeddedForTest(t *testing.T) []*models.DistroProfile {
	t.Helper()
	data, err := embeddedProfiles.ReadFile("distro-profiles.json")
	if err != nil {
		t.Fatalf("read embedded profiles: %v", err)
	}

	var pf ProfileFile
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("parse embedded profiles: %v", err)
	}

	out := make([]*models.DistroProfile, 0, len(pf.Profiles))
	for _, p := range pf.Profiles {
		out = append(out, profileDataToModel(p, pf.Version))
	}
	return out
}
