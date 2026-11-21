// Simulated host agent: in-memory containers/VMs with real lifecycle
// state transitions, resource accounting and cleanup. Used for tests
// and in the sandbox, where no Docker or libvirt exists. ExecInLab is
// simulated too: the allowlisted command is validated but never run —
// the result says so explicitly.
package hostagent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SimulatedHostAgent is the in-memory HostAgent.
type SimulatedHostAgent struct {
	store *memStore
	// Capacity overrides the host inventory for accounting. Zero values
	// are filled from CollectInventory at construction; tests may set
	// small values afterwards to exercise capacity rejection.
	TotalCPU      int
	TotalMemoryMB int64
	TotalDiskGB   float64
	LabVolumePath string
}

// NewSimulatedHostAgent builds a simulated agent sized to this host.
func NewSimulatedHostAgent() *SimulatedHostAgent {
	a := &SimulatedHostAgent{
		store:         newMemStore(),
		LabVolumePath: LabRoots[0],
	}
	if inv, err := CollectInventory(LabRoots[0]); err == nil {
		a.TotalCPU = inv.CPUCount
		a.TotalMemoryMB = inv.MemoryTotalMB
		a.TotalDiskGB = inv.DiskFreeGB
	}
	if a.TotalCPU <= 0 {
		a.TotalCPU = 4
	}
	if a.TotalMemoryMB <= 0 {
		a.TotalMemoryMB = 8192
	}
	return a
}

func (a *SimulatedHostAgent) Mode() string { return "simulated" }

func (a *SimulatedHostAgent) Inventory(ctx context.Context) (Inventory, error) {
	inv, err := CollectInventory(a.LabVolumePath)
	if err != nil {
		// Inventory must still report the simulated capacity.
		inv.CPUCount = a.TotalCPU
		inv.MemoryTotalMB = a.TotalMemoryMB
		inv.DiskFreeGB = a.TotalDiskGB
	}
	return inv, nil
}

func (a *SimulatedHostAgent) RunOp(ctx context.Context, req OpRequest) (OpResult, error) {
	if err := Validate(req.Op, req); err != nil {
		return OpResult{}, err
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if res, ok := a.store.idemGet(req); ok {
		return res, nil
	}
	var res OpResult
	var err error
	switch req.Op {
	case OpListContainers:
		res, err = a.listContainers(req)
	case OpCreateContainer:
		res, err = a.createContainer(req)
	case OpStartContainer:
		res, err = a.startContainer(req)
	case OpStopContainer:
		res, err = a.stopContainer(req)
	case OpRemoveContainer:
		res, err = a.removeContainer(req)
	case OpListVMs:
		res, err = a.listVMs(req)
	case OpCreateVM:
		res, err = a.createVM(req)
	case OpStartVM:
		res, err = a.startVM(req)
	case OpStopVM:
		res, err = a.stopVM(req)
	case OpRebootVM:
		res, err = a.rebootVM(req)
	case OpDeleteVM:
		res, err = a.deleteVM(req)
	case OpHostInventory:
		var inv Inventory
		inv, err = a.Inventory(ctx)
		res = OpResult{Op: req.Op, Inventory: &inv}
	case OpExecInLab:
		res = OpResult{Op: req.Op, State: "simulated",
			Message: fmt.Sprintf("simulated execution (no command was run): %s", strings.Join(req.Command, " "))}
	default:
		err = &ValidationError{Op: req.Op, Reason: "unknown operation"}
	}
	if err != nil {
		return OpResult{}, err
	}
	res.Op = req.Op
	a.store.idemPut(req, res)
	return res, nil
}

func (a *SimulatedHostAgent) listContainers(req OpRequest) (OpResult, error) {
	var items []map[string]any
	for _, c := range a.store.containers {
		if c.LabID == req.LabID {
			items = append(items, containerItem(c))
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	return OpResult{State: "ok", Message: fmt.Sprintf("%d container(s) in lab %s", len(items), req.LabID), Items: items}, nil
}

func (a *SimulatedHostAgent) createContainer(req OpRequest) (OpResult, error) {
	cpu := req.CPU
	if cpu == 0 {
		cpu = 1
	}
	mem := req.MemoryMB
	if mem == 0 {
		mem = 512
	}
	usedCPU, usedMem, _ := a.store.usedResources()
	if usedCPU+cpu > a.TotalCPU {
		return OpResult{}, fmt.Errorf("insufficient host CPU: need %d vCPU, %d used of %d total", cpu, usedCPU, a.TotalCPU)
	}
	if int64(usedMem+mem) > a.TotalMemoryMB {
		return OpResult{}, fmt.Errorf("insufficient host memory: need %d MiB, %d MiB used of %d MiB total", mem, usedMem, a.TotalMemoryMB)
	}
	if _, exists := a.store.byName["container:"+req.Name]; exists {
		return OpResult{}, fmt.Errorf("container %q already exists", req.Name)
	}
	c := &Container{
		ID: sID(a), Name: req.Name, Image: req.Image, LabID: req.LabID,
		OwnerID: req.OwnerID, State: "created", CPU: cpu, MemoryMB: mem,
		IP: a.store.nextIP("10.42.1"), CreatedAt: time.Now().UTC(),
	}
	a.store.containers[c.ID] = c
	a.store.byName["container:"+c.Name] = c.ID
	return OpResult{ID: c.ID, Name: c.Name, State: c.State,
		Message: fmt.Sprintf("container %s created (simulated)", c.Name)}, nil
}

func sID(a *SimulatedHostAgent) string { return a.store.nextID("sim") }

func (a *SimulatedHostAgent) startContainer(req OpRequest) (OpResult, error) {
	c := a.store.findContainer(req.ID, req.Name, req.LabID)
	if c == nil {
		return OpResult{}, fmt.Errorf("container not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if c.State == "running" {
		return OpResult{ID: c.ID, Name: c.Name, State: c.State, Message: "container already running"}, nil
	}
	c.State = "running"
	c.CPUPercent = 2.0 + float64(len(c.Name)%5)
	c.MemoryUsed = c.MemoryMB / 10
	return OpResult{ID: c.ID, Name: c.Name, State: c.State, Message: "container started (simulated)"}, nil
}

func (a *SimulatedHostAgent) stopContainer(req OpRequest) (OpResult, error) {
	c := a.store.findContainer(req.ID, req.Name, req.LabID)
	if c == nil {
		return OpResult{}, fmt.Errorf("container not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if c.State == "stopped" {
		return OpResult{ID: c.ID, Name: c.Name, State: c.State, Message: "container already stopped"}, nil
	}
	c.State = "stopped"
	c.CPUPercent = 0
	c.MemoryUsed = 0
	return OpResult{ID: c.ID, Name: c.Name, State: c.State, Message: "container stopped (simulated)"}, nil
}

func (a *SimulatedHostAgent) removeContainer(req OpRequest) (OpResult, error) {
	c := a.store.findContainer(req.ID, req.Name, req.LabID)
	if c == nil {
		// Double-remove is idempotent.
		return OpResult{State: "removed", Message: "container already removed"}, nil
	}
	delete(a.store.containers, c.ID)
	delete(a.store.byName, "container:"+c.Name)
	return OpResult{ID: c.ID, Name: c.Name, State: "removed", Message: "container removed (simulated)"}, nil
}

func (a *SimulatedHostAgent) listVMs(req OpRequest) (OpResult, error) {
	var items []map[string]any
	for _, v := range a.store.vms {
		if v.LabID == req.LabID {
			items = append(items, vmItem(v))
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	return OpResult{State: "ok", Message: fmt.Sprintf("%d VM(s) in lab %s", len(items), req.LabID), Items: items}, nil
}

func (a *SimulatedHostAgent) createVM(req OpRequest) (OpResult, error) {
	prof, err := ValidateProfile(req.Profile)
	if err != nil {
		return OpResult{}, err
	}
	username := req.Username
	if username == "" {
		username = "cloudforge"
	}
	hostname := req.Hostname
	if hostname == "" {
		hostname = req.Name
	}
	userData, err := GenerateUserData(CloudInit{Hostname: hostname, Username: username, SSHKey: req.SSHKey})
	if err != nil {
		return OpResult{}, err
	}
	inv := Inventory{CPUCount: a.TotalCPU, MemoryTotalMB: a.TotalMemoryMB, DiskFreeGB: a.TotalDiskGB}
	usedCPU, usedMem, usedDisk := a.store.usedResources()
	if err := CheckCapacity(prof, inv, usedCPU, usedMem, usedDisk); err != nil {
		return OpResult{}, err
	}
	if _, exists := a.store.byName["vm:"+req.Name]; exists {
		return OpResult{}, fmt.Errorf("VM %q already exists", req.Name)
	}
	v := &VM{
		ID: sID(a), Name: req.Name, LabID: req.LabID, OwnerID: req.OwnerID,
		Profile: prof.Name, State: "defined", CPU: prof.VCPU, MemoryMB: prof.MemoryMB,
		DiskGB: prof.DiskGB, Hostname: hostname, Username: username,
		IP:            a.store.nextIP("10.42.0"),
		DiskPlan:      fmt.Sprintf("qcow2 %dG with backing file ubuntu-24.04-base.qcow2 at %s/%s/%s-disk.qcow2", prof.DiskGB, LabRoots[0], req.LabID, req.Name),
		NetworkPlan:   fmt.Sprintf("bridge cloudforge-br0, isolated lab network for lab %s", req.LabID),
		CloudInitYAML: userData,
		CreatedAt:     time.Now().UTC(),
	}
	a.store.vms[v.ID] = v
	a.store.byName["vm:"+v.Name] = v.ID
	return OpResult{ID: v.ID, Name: v.Name, State: v.State,
		Message: fmt.Sprintf("VM %s created from profile %s (simulated)", v.Name, prof.Name)}, nil
}

func (a *SimulatedHostAgent) startVM(req OpRequest) (OpResult, error) {
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State == "running" {
		return OpResult{ID: v.ID, Name: v.Name, State: v.State, Message: "VM already running"}, nil
	}
	v.State = "running"
	v.CPUPercent = 3.0 + float64(len(v.Name)%4)
	v.MemoryUsed = v.MemoryMB / 8
	return OpResult{ID: v.ID, Name: v.Name, State: v.State, Message: "VM started (simulated)"}, nil
}

func (a *SimulatedHostAgent) stopVM(req OpRequest) (OpResult, error) {
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State == "stopped" {
		return OpResult{ID: v.ID, Name: v.Name, State: v.State, Message: "VM already stopped"}, nil
	}
	v.State = "stopped"
	v.CPUPercent = 0
	v.MemoryUsed = 0
	return OpResult{ID: v.ID, Name: v.Name, State: v.State, Message: "VM stopped (simulated)"}, nil
}

func (a *SimulatedHostAgent) rebootVM(req OpRequest) (OpResult, error) {
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State != "running" {
		return OpResult{}, fmt.Errorf("cannot reboot VM %q in state %q (must be running)", v.Name, v.State)
	}
	// Reboot: running -> running (state is preserved; a real reboot is
	// observed as a brief stop/start, which the simulated agent models
	// as an uninterrupted running state with a fresh boot timestamp).
	v.State = "running"
	return OpResult{ID: v.ID, Name: v.Name, State: v.State, Message: "VM rebooted (simulated)"}, nil
}

func (a *SimulatedHostAgent) deleteVM(req OpRequest) (OpResult, error) {
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	if v == nil {
		// Double-delete is idempotent.
		return OpResult{State: "deleted", Message: "VM already deleted"}, nil
	}
	delete(a.store.vms, v.ID)
	delete(a.store.byName, "vm:"+v.Name)
	return OpResult{ID: v.ID, Name: v.Name, State: "deleted", Message: "VM deleted (simulated)"}, nil
}

// CleanupLab removes every container and VM of the lab. Idempotent.
func (a *SimulatedHostAgent) CleanupLab(ctx context.Context, labID string) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	for id, c := range a.store.containers {
		if c.LabID == labID {
			delete(a.store.containers, id)
			delete(a.store.byName, "container:"+c.Name)
		}
	}
	for id, v := range a.store.vms {
		if v.LabID == labID {
			delete(a.store.vms, id)
			delete(a.store.byName, "vm:"+v.Name)
		}
	}
	return nil
}

// Usage returns per-resource cpu/mem usage from simulated accounting.
func (a *SimulatedHostAgent) Usage(ctx context.Context, labID string) ([]ResourceUsage, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	var out []ResourceUsage
	for _, c := range a.store.containers {
		if c.LabID == labID {
			out = append(out, ResourceUsage{Kind: "container", ID: c.ID, Name: c.Name, LabID: c.LabID, State: c.State, CPUPercent: c.CPUPercent, MemoryMB: c.MemoryMB, MemoryUsed: c.MemoryUsed})
		}
	}
	for _, v := range a.store.vms {
		if v.LabID == labID {
			out = append(out, ResourceUsage{Kind: "vm", ID: v.ID, Name: v.Name, LabID: v.LabID, State: v.State, CPUPercent: v.CPUPercent, MemoryMB: v.MemoryMB, MemoryUsed: v.MemoryUsed})
		}
	}
	if out == nil {
		out = []ResourceUsage{}
	}
	return out, nil
}
