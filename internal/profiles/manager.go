package profiles

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"bootimus/internal/models"
	"bootimus/internal/storage"
)

//go:embed distro-profiles.json
var embeddedProfiles embed.FS

const RemoteProfilesURL = "https://raw.githubusercontent.com/garybowers/bootimus/main/distro-profiles.json"

type ProfileFile struct {
	Version  string        `json:"version"`
	Profiles []ProfileData `json:"profiles"`
}

type ProfileData struct {
	ID                     string       `json:"id"`
	DisplayName            string       `json:"display_name"`
	Family                 string       `json:"family"`
	FilenamePatterns       []string     `json:"filename_patterns"`
	KernelPaths            []string     `json:"kernel_paths"`
	InitrdPaths            []string     `json:"initrd_paths"`
	SquashfsPaths          []string     `json:"squashfs_paths"`
	DefaultBootParams      string       `json:"default_boot_params"`
	BootParamsWithSquashfs string       `json:"boot_params_with_squashfs,omitempty"`
	AutoInstallType        string       `json:"auto_install_type,omitempty"`
	BootMethod             string       `json:"boot_method,omitempty"`
	IsoInitrdName          string       `json:"iso_initrd_name,omitempty"`
	Mirrors                []ISOMirror  `json:"mirrors,omitempty"`
	Releases               []ISORelease `json:"releases,omitempty"`
}

type Manager struct {
	store              storage.Storage
	DisableRemoteCheck bool
	mu                 sync.RWMutex
	loadMu             sync.Mutex
	isoInitrdNames     map[string]string
	isoInitrdLoaded    bool
}

func NewManager(store storage.Storage) *Manager {
	return &Manager{store: store}
}

func (m *Manager) SeedProfiles() error {
	defer m.invalidateIsoInitrdNameCache()

	data, err := embeddedProfiles.ReadFile("distro-profiles.json")
	if err != nil {
		return fmt.Errorf("failed to read embedded profiles: %w", err)
	}

	var pf ProfileFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("failed to parse embedded profiles: %w", err)
	}

	count := 0
	for _, p := range pf.Profiles {
		existing, err := m.store.GetDistroProfile(p.ID)
		if err != nil {
			profile := profileDataToModel(p, pf.Version)
			if err := m.store.SaveDistroProfile(profile); err != nil {
				log.Printf("Profiles: Failed to seed %s: %v", p.ID, err)
			} else {
				count++
			}
		} else if !existing.Custom {
			updated := profileDataToModel(p, pf.Version)
			updated.ID = existing.ID
			updated.CreatedAt = existing.CreatedAt
			if err := m.store.SaveDistroProfile(updated); err != nil {
				log.Printf("Profiles: Failed to update %s: %v", p.ID, err)
			} else {
				count++
			}
		}
	}

	if count > 0 {
		log.Printf("Profiles: Seeded/updated %d distro profiles (version: %s)", count, pf.Version)
	} else {
		log.Printf("Profiles: %d distro profiles loaded (version: %s)", len(pf.Profiles), pf.Version)
	}

	return nil
}

func (m *Manager) UpdateFromRemote() (added int, updated int, version string, err error) {
	if m.DisableRemoteCheck {
		return 0, 0, "", fmt.Errorf("remote profile updates are disabled")
	}
	defer m.invalidateIsoInitrdNameCache()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(RemoteProfilesURL)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to fetch remote profiles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", fmt.Errorf("remote profiles returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to read response: %w", err)
	}

	var pf ProfileFile
	if err := json.Unmarshal(body, &pf); err != nil {
		return 0, 0, "", fmt.Errorf("failed to parse remote profiles: %w", err)
	}

	for _, p := range pf.Profiles {
		existing, err := m.store.GetDistroProfile(p.ID)
		if err != nil {
			profile := profileDataToModel(p, pf.Version)
			if err := m.store.SaveDistroProfile(profile); err == nil {
				added++
			}
		} else if !existing.Custom {
			updatedProfile := profileDataToModel(p, pf.Version)
			updatedProfile.ID = existing.ID
			updatedProfile.CreatedAt = existing.CreatedAt
			if err := m.store.SaveDistroProfile(updatedProfile); err == nil {
				updated++
			}
		}
	}

	log.Printf("Profiles: Remote update complete (version: %s, added: %d, updated: %d)", pf.Version, added, updated)
	return added, updated, pf.Version, nil
}

func (m *Manager) MatchProfile(filename string) (*models.DistroProfile, error) {
	allProfiles, err := m.store.ListDistroProfiles()
	if err != nil {
		return nil, err
	}
	return matchProfile(allProfiles, filename)
}

func matchProfile(profiles []*models.DistroProfile, filename string) (*models.DistroProfile, error) {
	filenameLower := strings.ToLower(filename)

	for _, p := range profiles {
		if p.Custom {
			for _, pattern := range p.FilenamePatterns {
				if strings.Contains(filenameLower, strings.ToLower(pattern)) {
					return p, nil
				}
			}
		}
	}

	for _, p := range profiles {
		if !p.Custom {
			for _, pattern := range p.FilenamePatterns {
				if strings.Contains(filenameLower, strings.ToLower(pattern)) {
					return p, nil
				}
			}
		}
	}

	for _, p := range profiles {
		if strings.Contains(filenameLower, strings.ToLower(p.ProfileID)) {
			return p, nil
		}
	}

	for _, p := range profiles {
		if p.Family != "" && strings.Contains(filenameLower, strings.ToLower(p.Family)) {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no matching profile for %s", filename)
}

func (m *Manager) GetBootParams(distroID string, hasSquashfs bool) string {
	profile, err := m.store.GetDistroProfile(distroID)
	if err != nil {
		return ""
	}

	if hasSquashfs && profile.BootParamsWithSquashfs != "" {
		return profile.BootParamsWithSquashfs
	}
	return profile.DefaultBootParams
}

// IsoInitrdName returns the name under which the original uploaded ISO
// should be chain-loaded as an additional initrd module for the given
// distro profile (e.g. Proxmox VE, whose installer expects the source ISO
// to be available alongside the kernel/initrd at boot time). An empty
// string means no extra initrd module is required.
func (m *Manager) IsoInitrdName(distroID string) string {
	if m == nil {
		return ""
	}
	m.ensureIsoInitrdNameCache()

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isoInitrdNames[NormalizeProfileID(distroID)]
}

func (m *Manager) IsoInitrdNameMap() map[string]string {
	if m == nil {
		return nil
	}
	m.ensureIsoInitrdNameCache()

	m.mu.RLock()
	defer m.mu.RUnlock()

	isoInitrdNames := make(map[string]string, len(m.isoInitrdNames))
	for profileID, initrdName := range m.isoInitrdNames {
		isoInitrdNames[profileID] = initrdName
	}

	return isoInitrdNames
}

func (m *Manager) ensureIsoInitrdNameCache() {
	m.mu.RLock()
	if m.isoInitrdLoaded {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	m.mu.RLock()
	if m.isoInitrdLoaded {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	allProfiles, err := m.store.ListDistroProfiles()
	if err != nil {
		log.Printf("Profiles: Failed to load iso initrd profile cache: %v", err)
		m.mu.Lock()
		m.isoInitrdNames = map[string]string{}
		m.isoInitrdLoaded = true
		m.mu.Unlock()
		return
	}
	isoInitrdNames := buildIsoInitrdNameMap(allProfiles)

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isoInitrdLoaded {
		m.isoInitrdNames = isoInitrdNames
		m.isoInitrdLoaded = true
	}
}

func buildIsoInitrdNameMap(allProfiles []*models.DistroProfile) map[string]string {
	isoInitrdNames := make(map[string]string)
	profileIDs := make(map[string]string)
	for _, p := range allProfiles {
		if p == nil || p.ProfileID == "" || p.IsoInitrdName == "" {
			continue
		}
		normalizedProfileID := NormalizeProfileID(p.ProfileID)
		if normalizedProfileID == "" || !IsSafeIsoInitrdName(p.IsoInitrdName) {
			continue
		}
		currentProfileID, exists := profileIDs[normalizedProfileID]
		if exists && !preferIsoInitrdProfile(p.ProfileID, p.IsoInitrdName, currentProfileID, isoInitrdNames[normalizedProfileID], normalizedProfileID) {
			continue
		}
		isoInitrdNames[normalizedProfileID] = p.IsoInitrdName
		profileIDs[normalizedProfileID] = p.ProfileID
	}
	return isoInitrdNames
}

func preferIsoInitrdProfile(candidateID, candidateName, currentID, currentName, normalizedID string) bool {
	candidateIsCanonical := candidateID == normalizedID
	currentIsCanonical := currentID == normalizedID
	if candidateIsCanonical != currentIsCanonical {
		return candidateIsCanonical
	}
	if candidateID != currentID {
		return candidateID < currentID
	}
	return candidateName < currentName
}

const maxIsoInitrdNameLength = 128

func IsSafeIsoInitrdName(name string) bool {
	if name == "" || len(name) > maxIsoInitrdNameLength {
		return false
	}
	hasAlphanumeric := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			hasAlphanumeric = true
			continue
		}
		if c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return hasAlphanumeric
}

func (m *Manager) invalidateIsoInitrdNameCache() {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.isoInitrdNames = nil
	m.isoInitrdLoaded = false
}

func (m *Manager) InvalidateIsoInitrdNameCache() {
	m.invalidateIsoInitrdNameCache()
}

func NormalizeProfileID(profileID string) string {
	return strings.ToLower(strings.TrimSpace(profileID))
}

func profileDataToModel(p ProfileData, version string) *models.DistroProfile {
	return &models.DistroProfile{
		ProfileID:              p.ID,
		DisplayName:            p.DisplayName,
		Family:                 p.Family,
		FilenamePatterns:       models.StringSlice(p.FilenamePatterns),
		KernelPaths:            models.StringSlice(p.KernelPaths),
		InitrdPaths:            models.StringSlice(p.InitrdPaths),
		SquashfsPaths:          models.StringSlice(p.SquashfsPaths),
		DefaultBootParams:      p.DefaultBootParams,
		BootParamsWithSquashfs: p.BootParamsWithSquashfs,
		AutoInstallType:        p.AutoInstallType,
		BootMethod:             p.BootMethod,
		IsoInitrdName:          p.IsoInitrdName,
		Custom:                 false,
		Version:                version,
	}
}
