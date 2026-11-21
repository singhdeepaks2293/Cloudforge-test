// Host inventory and monitoring: real /proc parsing plus disk stats.
package hostagent

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Inventory is a typed snapshot of the host.
type Inventory struct {
	Hostname        string    `json:"hostname"`
	CPUCount        int       `json:"cpu_count"`
	CPUModel        string    `json:"cpu_model"`
	MemoryTotalMB   int64     `json:"memory_total_mb"`
	MemoryAvailable int64     `json:"memory_available_mb"`
	DiskTotalGB     float64   `json:"disk_total_gb"`
	DiskFreeGB      float64   `json:"disk_free_gb"`
	Load1           float64   `json:"load_1"`
	Load5           float64   `json:"load_5"`
	Load15          float64   `json:"load_15"`
	LabVolumePath   string    `json:"lab_volume_path"`
	CollectedAt     time.Time `json:"collected_at"`
}

// ParseCPUInfo reads /proc/cpuinfo: the processor count and the first
// model name line.
func ParseCPUInfo(r io.Reader) (count int, model string, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "processor") {
			count++
		}
		if model == "" && strings.HasPrefix(line, "model name") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				model = strings.TrimSpace(v)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return 0, "", fmt.Errorf("reading cpuinfo: %w", err)
	}
	if count == 0 {
		return 0, "", fmt.Errorf("cpuinfo contains no processors")
	}
	return count, model, nil
}

// ParseMemInfo reads /proc/meminfo MemTotal/MemAvailable (kB -> MiB).
func ParseMemInfo(r io.Reader) (totalMB, availMB int64, err error) {
	sc := bufio.NewScanner(r)
	var total, avail int64
	var sawTotal bool
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, perr := strconv.ParseInt(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, sawTotal = v/1024, true
		case "MemAvailable:":
			avail = v / 1024
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, fmt.Errorf("reading meminfo: %w", err)
	}
	if !sawTotal {
		return 0, 0, fmt.Errorf("meminfo contains no MemTotal")
	}
	return total, avail, nil
}

// ParseLoadAvg reads /proc/loadavg (1/5/15 minute load).
func ParseLoadAvg(r io.Reader) (l1, l5, l15 float64, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("reading loadavg: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("loadavg has %d fields, want at least 3", len(fields))
	}
	parse := func(s string) (float64, error) {
		return strconv.ParseFloat(s, 64)
	}
	if l1, err = parse(fields[0]); err != nil {
		return 0, 0, 0, fmt.Errorf("loadavg field 1: %w", err)
	}
	if l5, err = parse(fields[1]); err != nil {
		return 0, 0, 0, fmt.Errorf("loadavg field 2: %w", err)
	}
	if l15, err = parse(fields[2]); err != nil {
		return 0, 0, 0, fmt.Errorf("loadavg field 3: %w", err)
	}
	return l1, l5, l15, nil
}

// DiskUsage returns total/free GiB for the filesystem holding path.
func DiskUsage(path string) (totalGB, freeGB float64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	const gib = 1024 * 1024 * 1024
	totalGB = float64(st.Blocks*uint64(st.Bsize)) / gib
	freeGB = float64(st.Bavail*uint64(st.Bsize)) / gib
	return totalGB, freeGB, nil
}

// CollectInventoryFrom is the testable seam: it parses the given proc
// files and stats the lab volume path.
func CollectInventoryFrom(cpuinfoPath, meminfoPath, loadavgPath, labVolumePath string) (Inventory, error) {
	inv := Inventory{LabVolumePath: labVolumePath, CollectedAt: time.Now().UTC()}
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	inv.Hostname = host

	f, err := os.Open(cpuinfoPath)
	if err != nil {
		return inv, fmt.Errorf("open cpuinfo: %w", err)
	}
	inv.CPUCount, inv.CPUModel, err = ParseCPUInfo(f)
	_ = f.Close()
	if err != nil {
		return inv, err
	}

	f, err = os.Open(meminfoPath)
	if err != nil {
		return inv, fmt.Errorf("open meminfo: %w", err)
	}
	inv.MemoryTotalMB, inv.MemoryAvailable, err = ParseMemInfo(f)
	_ = f.Close()
	if err != nil {
		return inv, err
	}

	f, err = os.Open(loadavgPath)
	if err != nil {
		return inv, fmt.Errorf("open loadavg: %w", err)
	}
	inv.Load1, inv.Load5, inv.Load15, err = ParseLoadAvg(f)
	_ = f.Close()
	if err != nil {
		return inv, err
	}

	// Disk is best-effort: a missing lab volume must not fail inventory.
	// Callers treat DiskFreeGB == 0 as "unknown" (capacity checks skip it).
	if labVolumePath != "" {
		if total, free, derr := DiskUsage(labVolumePath); derr == nil {
			inv.DiskTotalGB, inv.DiskFreeGB = total, free
		}
	}
	return inv, nil
}

// CollectInventory reads the live /proc files and stats the lab volume.
func CollectInventory(labVolumePath string) (Inventory, error) {
	inv, err := CollectInventoryFrom("/proc/cpuinfo", "/proc/meminfo", "/proc/loadavg", labVolumePath)
	if err != nil {
		// Fall back to runtime facts so inventory is still useful on
		// hosts without /proc (never fabricate the missing fields).
		if inv.CPUCount == 0 {
			inv.CPUCount = runtime.NumCPU()
		}
		return inv, err
	}
	return inv, nil
}

func socketExistsOS(p string) (bool, error) {
	_, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return true, nil
}
