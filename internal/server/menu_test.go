package server

import (
	"fmt"
	"strings"
	"testing"

	"bootimus/internal/models"
)

func testMenuBuilder(types map[uint]string) *MenuBuilder {
	return &MenuBuilder{
		macAddress:       "aa:bb:cc:dd:ee:ff",
		serverAddr:       "10.0.0.1",
		httpPort:         8080,
		autoInstallTypes: types,
	}
}

func testBaseURL(mb *MenuBuilder) string {
	return fmt.Sprintf("http://%s:%d", mb.serverAddr, mb.httpPort)
}

func TestBuildNextBootBypassesMenuForGroupedImage(t *testing.T) {
	groupID := uint(3)
	mb := testMenuBuilder(map[uint]string{7: "kickstart"})
	mb.nextBootImageID = 7
	mb.images = []models.Image{
		{
			ID:         7,
			Name:       "AlmaLinux",
			Filename:   "almalinux.iso",
			Enabled:    true,
			BootMethod: "kernel",
			Distro:     "alma",
			GroupID:    &groupID,
		},
	}

	out := mb.Build()
	if !strings.HasPrefix(out, "#!ipxe\n\ngoto iso7\n\n") {
		t.Fatalf("expected next boot to jump directly to the image, got:\n%s", out)
	}
	if !strings.Contains(out, ":iso7\n") {
		t.Fatalf("expected next boot image section to be emitted, got:\n%s", out)
	}
	if strings.Contains(out, ":start\nmenu ") {
		t.Fatalf("expected next boot to bypass the interactive menu, got:\n%s", out)
	}
}

func TestBuildMissingNextBootFallsBackToMenu(t *testing.T) {
	mb := testMenuBuilder(nil)
	mb.nextBootImageID = 99

	out := mb.Build()
	if !strings.Contains(out, ":start\nmenu ") {
		t.Fatalf("expected an unavailable next boot image to fall back to the menu, got:\n%s", out)
	}
}

func TestBuildKernelBootSectionAutoInstallParams(t *testing.T) {
	img := &models.Image{
		ID:         7,
		Name:       "Test Distro",
		Filename:   "test.iso",
		Enabled:    true,
		BootMethod: "kernel",
		Distro:     "ubuntu",
		BootParams: "ip=dhcp",
	}

	cases := []struct {
		scriptType string
	}{
		{"kickstart"},
		{"preseed"},
		{"autoinstall"},
		{"generic"},
	}

	for _, c := range cases {
		mb := testMenuBuilder(map[uint]string{7: c.scriptType})
		baseURL := testBaseURL(mb)
		var want string
		switch c.scriptType {
		case "kickstart":
			want = fmt.Sprintf("inst.ks=%s/autoinstall/test.iso?mac=%s", baseURL, mb.macAddress)
		case "preseed":
			want = fmt.Sprintf("auto=true priority=critical url=%s/autoinstall/test.iso?mac=%s", baseURL, mb.macAddress)
		case "autoinstall":
			want = fmt.Sprintf("autoinstall ds=nocloud-net;s=%s/autoinstall/test.iso/mac/%s/", baseURL, mb.macAddress)
		default:
			want = fmt.Sprintf("autoinstall=%s/autoinstall/test.iso?mac=%s", baseURL, mb.macAddress)
		}
		out := mb.buildKernelBootSection(img, "test.iso", "test")
		if !strings.Contains(out, want) {
			t.Errorf("type %s: expected kernel line to contain %q, got:\n%s", c.scriptType, want, out)
		}
	}
}

func TestBuildKernelBootSectionNoAutoInstall(t *testing.T) {
	img := &models.Image{ID: 7, Filename: "test.iso", Enabled: true, BootMethod: "kernel", BootParams: "ip=dhcp"}

	for _, types := range []map[uint]string{nil, {7: "autounattend"}} {
		mb := testMenuBuilder(types)
		out := mb.buildKernelBootSection(img, "test.iso", "test")
		if strings.Contains(out, "autoinstall") || strings.Contains(out, "inst.ks") {
			t.Errorf("expected no auto-install params for types=%v, got:\n%s", types, out)
		}
	}
}

func TestBuildKernelBootSectionStripsBareNocloudParam(t *testing.T) {
	img := &models.Image{
		ID:         7,
		Filename:   "ubuntu.iso",
		Enabled:    true,
		BootMethod: "kernel",
		Distro:     "ubuntu",
		BootParams: "boot=casper initrd=initrd ds=nocloud ip=dhcp",
	}
	mb := testMenuBuilder(map[uint]string{7: "autoinstall"})

	out := mb.buildKernelBootSection(img, "ubuntu.iso", "ubuntu")
	if !strings.Contains(out, "ds=nocloud-net;s=") {
		t.Fatalf("expected nocloud-net seed param:\n%s", out)
	}
	if strings.Contains(out, " ds=nocloud ") || strings.HasSuffix(strings.TrimSpace(out), "ds=nocloud") {
		t.Errorf("expected bare ds=nocloud to be stripped from boot params:\n%s", out)
	}
	if !strings.Contains(out, "boot=casper") || !strings.Contains(out, "ip=dhcp") {
		t.Errorf("expected remaining boot params to survive:\n%s", out)
	}
}

func TestResolveBootParamsPlaceholders(t *testing.T) {
	img := &models.Image{
		BootParams: "url={{BASE_URL}} host={{SERVER_ADDR}} file={{IMAGE_FILENAME}} legacy={{FILENAME}} cache={{CACHE_DIR}} mac={{MAC}}",
	}
	mb := testMenuBuilder(nil)
	baseURL := testBaseURL(mb)

	got := mb.resolveBootParams(img, baseURL, "test.iso", "test")
	want := fmt.Sprintf("url=%s host=%s file=test.iso legacy=test.iso cache=test mac=%s", baseURL, mb.serverAddr, mb.macAddress)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestBuildWindowsBootSectionDropsPathTokens(t *testing.T) {
	mb := testMenuBuilder(nil)
	img := &models.Image{
		ID:         9,
		Filename:   "Win11_25H2.iso",
		Enabled:    true,
		BootMethod: "kernel",
		Distro:     "windows",
		BootParams: "rawbcd /opt/bootimus/data/isos/Win11_25H2/iso/sources/boot.wim quiet",
	}
	baseURL := testBaseURL(mb)

	out := mb.buildKernelBootSection(img, "Win11_25H2.iso", "Win11_25H2")
	if !strings.Contains(out, fmt.Sprintf("kernel %s/wimboot rawbcd quiet\n", baseURL)) {
		t.Errorf("expected path tokens stripped from the wimboot line, got:\n%s", out)
	}
	if strings.Contains(out, "/opt/bootimus") {
		t.Errorf("server filesystem path leaked into the menu:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("initrd %s/boot/Win11_25H2/iso/sources/boot.wim boot.wim", baseURL)) {
		t.Errorf("expected boot.wim initrd line, got:\n%s", out)
	}
}

func TestBuildWindowsBootSectionBareWimboot(t *testing.T) {
	mb := testMenuBuilder(nil)
	img := &models.Image{
		ID:         9,
		Filename:   "Win11_25H2.iso",
		Enabled:    true,
		BootMethod: "kernel",
		Distro:     "windows",
	}
	baseURL := testBaseURL(mb)

	out := mb.buildKernelBootSection(img, "Win11_25H2.iso", "Win11_25H2")
	if !strings.Contains(out, fmt.Sprintf("kernel %s/wimboot\n", baseURL)) {
		t.Errorf("expected a bare wimboot kernel line, got:\n%s", out)
	}
}

func TestBuildKernelBootSectionChainsIsoInitrdForProxmox(t *testing.T) {
	mb := testMenuBuilder(nil)
	baseURL := testBaseURL(mb)

	img := &models.Image{ID: 7, Filename: "proxmox-ve_9.2-1.iso", Enabled: true, BootMethod: "kernel", Distro: "proxmox"}
	out := mb.buildKernelBootSection(img, "proxmox-ve_9.2-1.iso", "proxmox-ve_9.2-1")

	if !strings.Contains(out, fmt.Sprintf("chain %s/boot/proxmox-ve_9.2-1/proxmox-pxe/boot.ipxe || goto failed\n", baseURL)) {
		t.Errorf("expected the Proxmox PXE helper script to be chained, got:\n%s", out)
	}
}

func TestBuildKernelBootSectionChainsIsoInitrdForNormalizedDistro(t *testing.T) {
	mb := testMenuBuilder(nil)
	baseURL := testBaseURL(mb)

	img := &models.Image{ID: 7, Filename: "proxmox-ve_9.2-1.iso", Enabled: true, BootMethod: "kernel", Distro: "  ProxMox  "}
	out := mb.buildKernelBootSection(img, "proxmox-ve_9.2-1.iso", "proxmox-ve_9.2-1")

	if !strings.Contains(out, fmt.Sprintf("chain %s/boot/proxmox-ve_9.2-1/proxmox-pxe/boot.ipxe || goto failed\n", baseURL)) {
		t.Errorf("expected normalized Proxmox distro to chain the prepared PXE script, got:\n%s", out)
	}
}

func TestBuildKernelBootSectionRejectsUnsafeDirectIsoInitrdName(t *testing.T) {
	mb := testMenuBuilder(nil)
	mb.isoInitrdNames = map[string]string{"other": "other.iso\nreboot"}
	img := &models.Image{ID: 7, Filename: "other.iso", Enabled: true, BootMethod: "kernel", Distro: "other"}

	out := mb.buildKernelBootSection(img, "other.iso", "other")
	if strings.Contains(out, fmt.Sprintf("initrd %s/isos/other.iso ", testBaseURL(mb))) {
		t.Fatalf("unsafe direct iso-initrd value was emitted in menu:\n%s", out)
	}
}

func TestBuildKernelBootSectionNoExtraInitrdWithoutProfile(t *testing.T) {
	mb := testMenuBuilder(nil)
	img := &models.Image{ID: 7, Filename: "ubuntu.iso", Enabled: true, BootMethod: "kernel", Distro: "ubuntu"}

	out := mb.buildKernelBootSection(img, "ubuntu.iso", "ubuntu")
	if strings.Count(out, "initrd ") != 1 {
		t.Errorf("expected no extra initrd module when no profile is configured, got:\n%s", out)
	}
}
