// Package quota enforces per-user resource quotas (Milestone 1
// lab count, extended in Milestone 9 Part 3 with per-role limits
// for labs, VM size and lab duration).
package quota

import (
	"errors"
	"fmt"
)

// DefaultMaxActiveLabs is the M1 per-user limit on concurrently active labs.
const DefaultMaxActiveLabs = 3

var ErrQuotaExceeded = errors.New("lab quota exceeded")

// ActiveStates are the lab states that count against the active-lab quota.
func IsActive(state string) bool {
	switch state {
	case "CREATED", "PROVISIONING", "READY", "RUNNING", "VALIDATING", "STOPPING":
		return true
	default:
		return false
	}
}

// Check returns ErrQuotaExceeded when activeCount has reached limit.
func Check(activeCount, limit int) error {
	if limit <= 0 {
		limit = DefaultMaxActiveLabs
	}
	if activeCount >= limit {
		return ErrQuotaExceeded
	}
	return nil
}

// --- Per-role limits (Milestone 9, Part 3) --------------------------------

// Limits is the configurable per-role quota set, persisted by the
// store.QuotaStore (admin API) and enforced by the HTTP layer.
// A zero value means "no limit" except MaxLabs, where 0 means the
// built-in default (DefaultMaxActiveLabs) — an unlimited lab count
// is not a meaningful configuration.
type Limits struct {
	MaxLabs     int `json:"max_labs"`
	MaxCPU      int `json:"max_cpu"`       // vCPUs per VM; 0 = unlimited
	MaxMemoryMB int `json:"max_memory_mb"` // RAM per VM; 0 = unlimited
	MaxDiskGB   int `json:"max_disk_gb"`   // disk per VM; 0 = unlimited
	MaxLabHours int `json:"max_lab_hours"` // lab duration cap; 0 = unlimited
}

// ValidRoles are the roles a quota set can be configured for.
var ValidRoles = []string{"learner", "instructor", "admin"}

// DefaultLimits returns the built-in per-role limits used when no
// row is configured (and seeded by migration 006).
func DefaultLimits(role string) Limits {
	switch role {
	case "admin":
		return Limits{MaxLabs: 10, MaxCPU: 16, MaxMemoryMB: 32768, MaxDiskGB: 200, MaxLabHours: 24}
	case "instructor":
		return Limits{MaxLabs: 5, MaxCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 100, MaxLabHours: 12}
	default: // learner and anything unknown
		return Limits{MaxLabs: DefaultMaxActiveLabs, MaxCPU: 4, MaxMemoryMB: 8192, MaxDiskGB: 50, MaxLabHours: 8}
	}
}

// MaxLabsOrDefault resolves MaxLabs, treating 0 as the built-in default.
func (l Limits) MaxLabsOrDefault() int {
	if l.MaxLabs <= 0 {
		return DefaultMaxActiveLabs
	}
	return l.MaxLabs
}

// CheckActiveLabs enforces the lab-count limit for a role.
func (l Limits) CheckActiveLabs(activeCount int) error {
	return Check(activeCount, l.MaxLabsOrDefault())
}

// CheckProfile enforces the per-VM size limits for a role: a VM
// profile that needs more vCPUs, RAM or disk than the role allows
// is refused before anything is provisioned.
func (l Limits) CheckProfile(cpu, memoryMB, diskGB int) error {
	if l.MaxCPU > 0 && cpu > l.MaxCPU {
		return fmt.Errorf("%w: profile needs %d vCPU, role allows %d", ErrQuotaExceeded, cpu, l.MaxCPU)
	}
	if l.MaxMemoryMB > 0 && memoryMB > l.MaxMemoryMB {
		return fmt.Errorf("%w: profile needs %d MiB RAM, role allows %d", ErrQuotaExceeded, memoryMB, l.MaxMemoryMB)
	}
	if l.MaxDiskGB > 0 && diskGB > l.MaxDiskGB {
		return fmt.Errorf("%w: profile needs %d GiB disk, role allows %d", ErrQuotaExceeded, diskGB, l.MaxDiskGB)
	}
	return nil
}

// CheckLabDuration enforces the lab-duration cap for a role: a lab
// definition longer than the cap cannot be started.
func (l Limits) CheckLabDuration(durationMin int) error {
	if l.MaxLabHours > 0 && durationMin > l.MaxLabHours*60 {
		return fmt.Errorf("%w: lab duration %d min exceeds the %d h role limit", ErrQuotaExceeded, durationMin, l.MaxLabHours)
	}
	return nil
}
