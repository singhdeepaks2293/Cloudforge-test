#!/usr/bin/env bash
# CloudForge host readiness check (Milestone 1).
#
# Verifies the Linux host can run CloudForge: CPU virtualization, KVM,
# Docker, libvirt, memory and disk. Prints PASS/FAIL per check and exits
# non-zero when KVM is missing — a missing check is reported as FAIL,
# never faked as a pass.
#
# Usage: bash infrastructure/scripts/check-host.sh
# This script only inspects the host. It installs nothing and changes
# no firewall or network configuration.

set -u

FAIL=0
WARN=0

check() { # check <name> <ok: 0|1> <detail>
  if [ "$2" -eq 0 ]; then
    printf 'PASS  %s — %s\n' "$1" "$3"
  else
    printf 'FAIL  %s — %s\n' "$1" "$3"
    FAIL=1
  fi
}

warn_check() { # warn_check <name> <ok: 0|1> <detail>
  if [ "$2" -eq 0 ]; then
    printf 'PASS  %s — %s\n' "$1" "$3"
  else
    printf 'WARN  %s — %s\n' "$1" "$3"
    WARN=1
  fi
}

echo "CloudForge host check — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo

# --- CPU virtualization -------------------------------------------------
if grep -Eq ' (vmx|svm) ' /proc/cpuinfo 2>/dev/null; then
  check "CPU virtualization flags (vmx/svm)" 0 "$(grep -m1 -o 'vmx\|svm' /proc/cpuinfo) present"
else
  check "CPU virtualization flags (vmx/svm)" 1 "no vmx/svm flag in /proc/cpuinfo — KVM cannot run VMs"
fi

# --- KVM device ---------------------------------------------------------
if [ -e /dev/kvm ]; then
  if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then
    check "/dev/kvm" 0 "present and readable/writable"
  else
    check "/dev/kvm" 1 "present but not readable/writable by this user (check kvm group membership)"
  fi
else
  check "/dev/kvm" 1 "missing — load the kvm module / enable virtualization in BIOS"
fi

# --- Docker -------------------------------------------------------------
if command -v docker >/dev/null 2>&1; then
  check "docker CLI" 0 "$(docker --version 2>/dev/null | head -n1)"
  if docker info >/dev/null 2>&1; then
    check "docker daemon reachable" 0 "docker info succeeded"
  else
    check "docker daemon reachable" 1 "docker info failed — is the daemon running and is this user in the docker group?"
  fi
  if docker compose version >/dev/null 2>&1; then
    check "docker compose (v2 plugin)" 0 "$(docker compose version 2>/dev/null | head -n1)"
  else
    check "docker compose (v2 plugin)" 1 "docker compose not available"
  fi
else
  check "docker CLI" 1 "docker not installed"
  check "docker daemon reachable" 1 "skipped — no docker CLI"
  check "docker compose (v2 plugin)" 1 "skipped — no docker CLI"
fi

# --- libvirt / QEMU -----------------------------------------------------
if command -v virsh >/dev/null 2>&1; then
  check "libvirt client (virsh)" 0 "$(virsh --version 2>/dev/null)"
else
  check "libvirt client (virsh)" 1 "virsh not installed (apt install libvirt-daemon-system libvirt-clients)"
fi
if command -v qemu-system-x86_64 >/dev/null 2>&1; then
  check "QEMU (qemu-system-x86_64)" 0 "installed"
else
  check "QEMU (qemu-system-x86_64)" 1 "not installed (apt install qemu-kvm)"
fi
if command -v nft >/dev/null 2>&1; then
  check "nftables (nft)" 0 "installed"
else
  warn_check "nftables (nft)" 1 "not installed — required by the Rust network engine (M4)"
fi

# --- Memory -------------------------------------------------------------
MEM_GB=$(free -g 2>/dev/null | awk '/^Mem:/ {print $2}')
if [ -n "${MEM_GB:-}" ]; then
  if [ "$MEM_GB" -ge 24 ]; then
    check "memory" 0 "${MEM_GB} GB total (recommended 24–32 GB)"
  elif [ "$MEM_GB" -ge 8 ]; then
    warn_check "memory" 1 "${MEM_GB} GB total — minimum is 8 GB, 24–32 GB recommended"
  else
    check "memory" 1 "${MEM_GB} GB total — below the 8 GB minimum"
  fi
else
  check "memory" 1 "could not read memory (free unavailable)"
fi

# --- Disk ---------------------------------------------------------------
DISK_AVAIL_GB=$(df -BG --output=avail "$PWD" 2>/dev/null | tail -n1 | tr -dc '0-9')
if [ -n "${DISK_AVAIL_GB:-}" ]; then
  if [ "$DISK_AVAIL_GB" -ge 100 ]; then
    check "disk free ($PWD)" 0 "${DISK_AVAIL_GB} GB available"
  elif [ "$DISK_AVAIL_GB" -ge 50 ]; then
    warn_check "disk free ($PWD)" 1 "${DISK_AVAIL_GB} GB available — 250 GB SSD recommended"
  else
    check "disk free ($PWD)" 1 "${DISK_AVAIL_GB} GB available — below the 50 GB minimum"
  fi
else
  check "disk free" 1 "could not read disk space (df unavailable)"
fi

# --- OS (informational) -------------------------------------------------
if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  echo "INFO  OS — ${PRETTY_NAME:-unknown}"
  if [ "${ID:-}" = "ubuntu" ] && [ "${VERSION_ID:-}" != "24.04" ]; then
    echo "WARN  OS — target is Ubuntu 24.04 LTS; this host is ${PRETTY_NAME:-unknown}"
    WARN=1
  fi
fi

echo
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: FAIL — fix the FAIL items above before running CloudForge (KVM is required)."
  exit 1
fi
if [ "$WARN" -ne 0 ]; then
  echo "RESULT: PASS with warnings — CloudForge can start, but heed the WARN items above."
  exit 0
fi
echo "RESULT: PASS — host looks ready for CloudForge."
exit 0
