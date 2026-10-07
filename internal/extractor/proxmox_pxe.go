// Package extractor prepares boot files from installation media.
package extractor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func prepareProxmoxPXEFiles(isoPath, outputDir string) (*BootFiles, error) {
	assistant, err := exec.LookPath("proxmox-auto-install-assistant")
	if err != nil {
		return nil, fmt.Errorf("proxmox PXE extraction requires proxmox-auto-install-assistant and xorriso: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create Proxmox PXE output directory: %w", err)
	}

	args := []string{
		"prepare-iso",
		"--fetch-from", "http",
		"--pxe",
		"--pxe-loader", "ipxe",
		"--output", outputDir,
		"--tmp", outputDir,
		isoPath,
	}
	output, err := exec.Command(assistant, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prepare Proxmox PXE files: %w: %s", err, strings.TrimSpace(string(output)))
	}

	isoStem := strings.TrimSuffix(filepath.Base(isoPath), filepath.Ext(isoPath))
	for _, name := range []string{"vmlinuz", "initrd.img", "boot.ipxe", isoStem + "-auto-from-http.iso"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			return nil, fmt.Errorf("proxmox PXE preparation did not create %s: %w", name, err)
		}
	}

	return &BootFiles{
		Kernel:       filepath.Join(outputDir, "vmlinuz"),
		Initrd:       filepath.Join(outputDir, "initrd.img"),
		Distro:       "proxmox",
		ExtractedDir: outputDir,
	}, nil
}
