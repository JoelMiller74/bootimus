package extractor

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeFSReader struct {
	exists map[string]bool
}

func (f fakeFSReader) FileExists(path string) bool {
	return f.exists[path]
}

func (f fakeFSReader) ExtractFile(_, _ string) error {
	return nil
}

func (f fakeFSReader) ExtractAll(_ string) error {
	return nil
}

func (f fakeFSReader) ReadFileContent(_ string) string {
	return ""
}

func (f fakeFSReader) ListDirectory(_ string) ([]DirEntry, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestDetectWindowsUnifiedFindsBootFilesExplicitly(t *testing.T) {
	e := &Extractor{}
	r := fakeFSReader{exists: map[string]bool{
		"/BOOT/BCD":            true,
		"/BOOT/BOOT.SDI":       true,
		"/SOURCES/BOOT.WIM":    true,
		"/SOURCES/INSTALL.WIM": true,
	}}

	files, err := e.detectWindowsUnified(r)
	if err != nil {
		t.Fatalf("detectWindowsUnified returned error: %v", err)
	}
	if files.Distro != "windows" {
		t.Fatalf("expected distro windows, got %q", files.Distro)
	}
	if files.Kernel != "/BOOT/BCD" {
		t.Fatalf("expected BCD kernel path, got %q", files.Kernel)
	}
	if files.Initrd != "/BOOT/BOOT.SDI" {
		t.Fatalf("expected boot.sdi initrd path, got %q", files.Initrd)
	}
	if files.BootWim != "/SOURCES/BOOT.WIM" {
		t.Fatalf("expected boot.wim path, got %q", files.BootWim)
	}
	if files.InstallWim != "/SOURCES/INSTALL.WIM" {
		t.Fatalf("expected install.wim path, got %q", files.InstallWim)
	}
}

func TestDetectWindowsUnifiedRequiresBootWim(t *testing.T) {
	e := &Extractor{}
	r := fakeFSReader{exists: map[string]bool{
		"/BOOT/BCD":      true,
		"/BOOT/BOOT.SDI": true,
	}}

	_, err := e.detectWindowsUnified(r)
	if err == nil {
		t.Fatal("expected error when boot.wim is missing")
	}
	if !errors.Is(err, ErrNotWindowsISO) {
		t.Fatalf("expected ErrNotWindowsISO, got %v", err)
	}
	if !strings.Contains(err.Error(), "boot.wim") {
		t.Fatalf("expected error to identify missing boot.wim, got %v", err)
	}
}
