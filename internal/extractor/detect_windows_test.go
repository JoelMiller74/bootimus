package extractor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kdomanski/iso9660"
)

func TestDetectWindowsRequiresBootWimReturnsSentinel(t *testing.T) {
	e := &Extractor{}

	isoPath := filepath.Join(t.TempDir(), "windows-missing-bootwim.iso")
	writeTestISO(t, isoPath, map[string]string{
		"/BOOT/BCD":      "bcd",
		"/BOOT/BOOT.SDI": "boot-sdi",
	})

	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("open test iso: %v", err)
	}
	defer f.Close()

	img, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("open iso image: %v", err)
	}

	_, err = e.detectWindows(img)
	if err == nil {
		t.Fatal("expected error when boot.wim is missing")
	}
	if !errors.Is(err, ErrNotWindowsISO) {
		t.Fatalf("expected ErrNotWindowsISO, got %v", err)
	}
}
