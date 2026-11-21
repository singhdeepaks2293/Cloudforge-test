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
//
// MaxLabMinutes, when > 0, is the lab-duration cap in minutes and
// takes precedence over MaxLabHours. It exists so sub-hour caps
// (the lightweight mode's 30-minute labs) can be expressed; the
// hour field cannot. When both are 0 the duration is unlimited.
type Limits struct {
	MaxLabs       int `json:"max_labs"`
	MaxCPU        int `json:"max_cpu"`         // vCPUs per VM; 0 = unlimited
	MaxMemoryMB   int `json:"max_memory_mb"`   // RAM per VM; 0 = unlimited
	MaxDiskGB     int `json:"max_disk_gb"`     // disk per VM; 0 = unlimited
	MaxLabHours   int `json:"max_lab_hours"`   // lab duration cap in hours; 0 = unlimited
	MaxLabMinutes int `json:"max_lab_minutes"` // lab duration cap in minutes; 0 = fall back to MaxLabHours
}

// LiteMaxLabMinutes is the lightweight-mode lab duration: 30 minutes.
const LiteMaxLabMinutes = 30

// LiteLimits returns the per-role limits for lightweight mode
// (CLOUDFORGE_LITE=true): 1 active lab and the Micro profile only
// for learners, short labs, small disks. The intent is a classroom
// laptop (2–4 cores, 4 GB RAM): the platform itself stays under
// ~1.5 GiB and one Micro lab costs ~1 GiB, so a 4 GiB machine can
// still run one lab at a time.
func LiteLimits(role string) Limits {
	switch role {
	case "admin":
		return Limits{MaxLabs: 3, MaxCPU: 2, MaxMemoryMB: 2048, MaxDiskGB: 16, MaxLabMinutes: 120}
	case "instructor":
		return Limits{MaxLabs: 2, MaxCPU: 1, MaxMemoryMB: 1024, MaxDiskGB: 8, MaxLabMinutes: 60}
	default: // learner and anything unknown
		return Limits{MaxLabs: 1, MaxCPU: 1, MaxMemoryMB: 1024, MaxDiskGB: 8, MaxLabMinutes: LiteMaxLabMinutes}
	}
}

// LimitsFor returns LiteLimits when lite is true, DefaultLimits
// otherwise. Callers that resolve limits from a store should cap
// the stored row at LiteLimits in lite mode (see httpapi.roleLimits)
// rather than ignoring the stored row outright.
func LimitsFor(role string, lite bool) Limits {
	if lite {
		return LiteLimits(role)
	}
	return DefaultLimits(role)
}

// CapAt returns l with every positive field capped at cap's
// corresponding field (duration compared in minutes). A zero field
// in cap means "no cap on that dimension" — except MaxLabs, where
// 0 means the built-in default. Used to enforce lightweight mode
// over an already-seeded (full-mode) quota row.
func (l Limits) CapAt(cap Limits) Limits {
	out := l
	if cap.MaxLabs > 0 && out.MaxLabsOrDefault() > cap.MaxLabs {
		out.MaxLabs = cap.MaxLabs
	}
	if cap.MaxCPU > 0 && (out.MaxCPU == 0 || out.MaxCPU > cap.MaxCPU) {
		out.MaxCPU = cap.MaxCPU
	}
	if cap.MaxMemoryMB > 0 && (out.MaxMemoryMB == 0 || out.MaxMemoryMB > cap.MaxMemoryMB) {
		out.MaxMemoryMB = cap.MaxMemoryMB
	}
	if cap.MaxDiskGB > 0 && (out.MaxDiskGB == 0 || out.MaxDiskGB > cap.MaxDiskGB) {
		out.MaxDiskGB = cap.MaxDiskGB
	}
	if capDur := cap.DurationMinutes(); capDur > 0 {
		if lDur := out.DurationMinutes(); lDur == 0 || lDur > capDur {
			out.MaxLabMinutes = capDur
			out.MaxLabHours = 0
		}
	}
	return out
}

// DurationMinutes returns the lab-duration cap in minutes, or 0
// for unlimited (both fields zero).
func (l Limits) DurationMinutes() int {
	if l.MaxLabMinutes > 0 {
		return l.MaxLabMinutes
	}
	if l.MaxLabHours > 0 {
		return l.MaxLabHours * 60
	}
	return 0
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
// definition longer than the cap cannot be started. MaxLabMinutes
// (when set) wins over MaxLabHours; both zero means unlimited.
func (l Limits) CheckLabDuration(durationMin int) error {
	if cap := l.DurationMinutes(); cap > 0 && durationMin > cap {
		return fmt.Errorf("%w: lab duration %d min exceeds the %d min role limit", ErrQuotaExceeded, durationMin, cap)
	}
	return nil
}
