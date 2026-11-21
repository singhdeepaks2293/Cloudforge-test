// VM/container profiles (master prompt §6) and cloud-init generation.
package hostagent

import (
	"fmt"
	"strconv"
	"strings"
)

// Profile is a VM size class.
type Profile struct {
	Name        string `json:"name"`
	VCPU        int    `json:"vcpu"`
	MemoryMB    int    `json:"memory_mb"`
	DiskGB      int    `json:"disk_gb"`
	Description string `json:"description"`
}

// Profiles are the fixed size classes. Micro is 1 vCPU / 1 GiB / 8 GiB,
// inside the spec's 512 MB–1 GiB range.
var Profiles = map[string]Profile{
	"micro":             {Name: "micro", VCPU: 1, MemoryMB: 1024, DiskGB: 8, Description: "1 vCPU, 1 GiB RAM, 8 GiB disk — bastion / tiny service"},
	"small":             {Name: "small", VCPU: 2, MemoryMB: 2048, DiskGB: 16, Description: "2 vCPU, 2 GiB RAM, 16 GiB disk — web / app server"},
	"medium":            {Name: "medium", VCPU: 2, MemoryMB: 4096, DiskGB: 30, Description: "2 vCPU, 4 GiB RAM, 30 GiB disk — app tier"},
	"database":          {Name: "database", VCPU: 2, MemoryMB: 4096, DiskGB: 30, Description: "2 vCPU, 4 GiB RAM, 30 GiB disk — database tier"},
	"network-appliance": {Name: "network-appliance", VCPU: 1, MemoryMB: 1024, DiskGB: 8, Description: "1 vCPU, 1 GiB RAM, 8 GiB disk — router / NAT / firewall"},
}

// ValidateProfile returns the profile for name, or an error for an
// unknown profile. Profile names are case-insensitive.
func ValidateProfile(name string) (Profile, error) {
	p, ok := Profiles[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		names := []string{"micro", "small", "medium", "database", "network-appliance"}
		return Profile{}, fmt.Errorf("unknown VM profile %q (want one of %s)", name, strings.Join(names, ", "))
	}
	return p, nil
}

// ValidateBounds checks explicit vCPU/RAM/disk values against the
// absolute limits (used when a caller overrides a profile).
func ValidateBounds(vcpu, memoryMB, diskGB int) error {
	if vcpu < 1 || vcpu > 32 {
		return fmt.Errorf("vcpu %d out of bounds (1–32)", vcpu)
	}
	if memoryMB < 128 || memoryMB > 65536 {
		return fmt.Errorf("memory_mb %d out of bounds (128–65536)", memoryMB)
	}
	if diskGB < 1 || diskGB > 500 {
		return fmt.Errorf("disk_gb %d out of bounds (1–500)", diskGB)
	}
	return nil
}

// CheckCapacity verifies that adding want to the already-used resources
// fits inside the host inventory. A zero total in the inventory means
// "unknown" and that dimension is skipped (never a false rejection).
func CheckCapacity(want Profile, inv Inventory, usedVCPU, usedMemoryMB, usedDiskGB int) error {
	if inv.CPUCount > 0 && usedVCPU+want.VCPU > inv.CPUCount {
		return fmt.Errorf("insufficient host CPU: need %d vCPU, %d used of %d total", want.VCPU, usedVCPU, inv.CPUCount)
	}
	if inv.MemoryTotalMB > 0 && int64(usedMemoryMB+want.MemoryMB) > inv.MemoryTotalMB {
		return fmt.Errorf("insufficient host memory: need %d MiB, %d MiB used of %d MiB total", want.MemoryMB, usedMemoryMB, inv.MemoryTotalMB)
	}
	if inv.DiskFreeGB > 0 && float64(usedDiskGB+want.DiskGB) > inv.DiskFreeGB {
		return fmt.Errorf("insufficient lab disk: need %d GiB, %d GiB used of %.1f GiB free", want.DiskGB, usedDiskGB, inv.DiskFreeGB)
	}
	return nil
}

// CloudInit is the typed input to cloud-init user-data generation.
// Fields are validated; nothing is passed through a text template, so a
// hostile hostname/username/key cannot inject YAML.
type CloudInit struct {
	Hostname string
	Username string
	SSHKey   string   // optional public key
	Packages []string // optional apt packages (safe tokens only)
}

// GenerateUserData renders cloud-init user-data YAML from ci. All scalar
// values are validated and double-quoted with strconv.Quote, so the
// output is data, never code.
func GenerateUserData(ci CloudInit) (string, error) {
	if ci.Hostname == "" {
		return "", fmt.Errorf("cloud-init hostname is required")
	}
	if !safeToken.MatchString(ci.Hostname) {
		return "", fmt.Errorf("cloud-init hostname %q is invalid", ci.Hostname)
	}
	if ci.Username == "" {
		return "", fmt.Errorf("cloud-init username is required")
	}
	if !safeUser.MatchString(ci.Username) {
		return "", fmt.Errorf("cloud-init username %q is invalid", ci.Username)
	}
	if ci.SSHKey != "" && !safeSSHKey.MatchString(ci.SSHKey) {
		return "", fmt.Errorf("cloud-init ssh key is not well-formed")
	}
	for _, p := range ci.Packages {
		if !safeToken.MatchString(p) {
			return "", fmt.Errorf("cloud-init package %q is invalid", p)
		}
	}
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	fmt.Fprintf(&b, "hostname: %s\n", strconv.Quote(ci.Hostname))
	b.WriteString("manage_etc_hosts: true\n")
	fmt.Fprintf(&b, "users:\n  - name: %s\n    sudo: ALL=(ALL) NOPASSWD:ALL\n    shell: /bin/bash\n", strconv.Quote(ci.Username))
	if ci.SSHKey != "" {
		fmt.Fprintf(&b, "    ssh_authorized_keys:\n      - %s\n", strconv.Quote(ci.SSHKey))
	}
	if len(ci.Packages) > 0 {
		b.WriteString("packages:\n")
		for _, p := range ci.Packages {
			fmt.Fprintf(&b, "  - %s\n", strconv.Quote(p))
		}
	}
	return b.String(), nil
}
