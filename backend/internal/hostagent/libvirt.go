// Libvirt/QEMU host agent.
//
// No libvirt-go CGO dependency is used in this sandbox (libvirt dev
// libraries are absent), so the agent is a typed wrapper behind the
// HostAgent interface: all VM lifecycle logic operates on a typed VM
// record store, and the actual hypervisor call is isolated in the single
// clearly-marked hypervisor method below, which drives `virsh` with
// fixed argv (validated domain name only — never user strings, never a
// shell). When the libvirt socket or virsh is absent, every VM op
// returns a structured Unavailable error and no record is created.
//
// Real-host requirements (see README): qemu-kvm, libvirt-daemon-system,
// virtinst, cloud-image-utils, and the cloudforge bridge network.
package hostagent

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LibvirtHostAgent manages VMs through libvirt (virsh CLI wrapper).
type LibvirtHostAgent struct {
	socketPath    string
	uri           string
	store         *memStore
	LabVolumePath string
	// virshPath is the virsh binary; overridable in tests.
	virshPath string
}

// NewLibvirtHostAgent builds the agent for a libvirt socket/URI.
func NewLibvirtHostAgent(socketPath, uri string) *LibvirtHostAgent {
	if socketPath == "" {
		socketPath = DefaultLibvirtSocket
	}
	if uri == "" {
		uri = DefaultLibvirtURI
	}
	return &LibvirtHostAgent{
		socketPath: socketPath, uri: uri, store: newMemStore(),
		LabVolumePath: LabRoots[0], virshPath: "virsh",
	}
}

func (a *LibvirtHostAgent) Mode() string { return "libvirt" }

func (a *LibvirtHostAgent) Inventory(ctx context.Context) (Inventory, error) {
	return CollectInventory(a.LabVolumePath)
}

// --- hypervisor boundary --------------------------------------------------
//
// hypervisor is the ONLY method that talks to the hypervisor. It checks
// for the libvirt socket and the virsh binary first and returns a
// structured Unavailable error when either is missing. The argv passed
// to virsh is fixed; the only variable part is a domain name that has
// already passed Validate (safe token), and for "define" a domain-XML
// file generated from typed structs by domainXML (no string templates).
func (a *LibvirtHostAgent) hypervisor(ctx context.Context, args ...string) error {
	if _, err := os.Stat(a.socketPath); err != nil {
		return &UnavailableError{Backend: "libvirt", Reason: fmt.Sprintf("libvirt socket %s not present (%v); install qemu-kvm + libvirt-daemon-system and start libvirtd", a.socketPath, err)}
	}
	if _, err := exec.LookPath(a.virshPath); err != nil {
		return &UnavailableError{Backend: "libvirt", Reason: fmt.Sprintf("virsh not found (%v); install libvirt-clients / virtinst", err)}
	}
	full := append([]string{"-c", a.uri}, args...)
	cmd := exec.CommandContext(ctx, a.virshPath, full...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("virsh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// --- domain XML (typed, no template injection) ---------------------------

type domainDisk struct {
	XMLName xml.Name     `xml:"disk"`
	Type    string       `xml:"type,attr"`
	Device  string       `xml:"device,attr"`
	Driver  domainDriver `xml:"driver"`
	Source  domainSource `xml:"source"`
	Target  domainTarget `xml:"target"`
}

type domainDriver struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type domainSource struct {
	File string `xml:"file,attr"`
}

type domainTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type domainInterface struct {
	XMLName xml.Name       `xml:"interface"`
	Type    string         `xml:"type,attr"`
	Source  domainIFSource `xml:"source"`
	Model   domainIFModel  `xml:"model"`
}

type domainIFSource struct {
	Bridge string `xml:"bridge,attr"`
}

type domainIFModel struct {
	Type string `xml:"type,attr"`
}

type domainXML struct {
	XMLName xml.Name      `xml:"domain"`
	Type    string        `xml:"type,attr"`
	Name    string        `xml:"name"`
	Memory  domainMem     `xml:"memory"`
	VCPU    int           `xml:"vcpu"`
	OS      domainOS      `xml:"os"`
	Devices domainDevices `xml:"devices"`
}

type domainMem struct {
	Unit  string `xml:"unit,attr"`
	Value int    `xml:",chardata"`
}

type domainOS struct {
	Type domainOSType `xml:"type"`
}

type domainOSType struct {
	Arch    string `xml:"arch,attr"`
	Machine string `xml:"machine,attr"`
	Value   string `xml:",chardata"`
}

type domainDevices struct {
	Disks      []domainDisk      `xml:"disk"`
	Interfaces []domainInterface `xml:"interface"`
}

// domainXML renders a libvirt domain definition from typed fields.
// Every value comes from a validated profile/record, never from a raw
// string template, so a hostile name cannot inject XML.
func renderDomainXML(v *VM, diskPath string) ([]byte, error) {
	d := domainXML{
		Type:   "kvm",
		Name:   v.Name,
		Memory: domainMem{Unit: "MiB", Value: v.MemoryMB},
		VCPU:   v.CPU,
		OS:     domainOS{Type: domainOSType{Arch: "x86_64", Machine: "pc", Value: "hvm"}},
		Devices: domainDevices{
			Disks: []domainDisk{{
				Type:   "file",
				Device: "disk",
				Driver: domainDriver{Name: "qemu", Type: "qcow2"},
				Source: domainSource{File: diskPath},
				Target: domainTarget{Dev: "vda", Bus: "virtio"},
			}},
			Interfaces: []domainInterface{{
				Type:   "bridge",
				Source: domainIFSource{Bridge: "cloudforge-br0"},
				Model:  domainIFModel{Type: "virtio"},
			}},
		},
	}
	out, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render domain XML: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

// diskPlan describes the qcow2 backing-file plan for a VM.
func diskPlan(labID, name string, diskGB int) (diskPath, plan string) {
	diskPath = fmt.Sprintf("%s/%s/%s-disk.qcow2", LabRoots[0], labID, name)
	plan = fmt.Sprintf("qcow2 %dG at %s with backing file ubuntu-24.04-base.qcow2 (qemu-img create -f qcow2 -b ubuntu-24.04-base.qcow2 -F qcow2 %s %dG)", diskGB, diskPath, diskPath, diskGB)
	return diskPath, plan
}

// --- HostAgent ------------------------------------------------------------

func (a *LibvirtHostAgent) RunOp(ctx context.Context, req OpRequest) (OpResult, error) {
	if err := Validate(req.Op, req); err != nil {
		return OpResult{}, err
	}
	switch req.Op {
	case OpHostInventory:
		inv, err := a.Inventory(ctx)
		if err != nil {
			return OpResult{}, err
		}
		return OpResult{Op: req.Op, Inventory: &inv}, nil
	case OpListVMs:
		return a.listVMs(req)
	case OpCreateVM:
		return a.createVM(ctx, req)
	case OpStartVM:
		return a.startVM(ctx, req)
	case OpStopVM:
		return a.stopVM(ctx, req)
	case OpRebootVM:
		return a.rebootVM(ctx, req)
	case OpDeleteVM:
		return a.deleteVM(ctx, req)
	case OpListContainers, OpCreateContainer, OpStartContainer, OpStopContainer, OpRemoveContainer:
		return OpResult{}, &UnavailableError{Backend: "libvirt", Reason: "the libvirt agent manages VMs only; containers need HOST_AGENT_MODE=docker"}
	case OpExecInLab:
		return OpResult{}, &UnavailableError{Backend: "libvirt", Reason: "ExecInLab is not implemented for VMs in M3 (use the simulated agent or docker exec)"}
	default:
		return OpResult{}, &ValidationError{Op: req.Op, Reason: "unknown operation"}
	}
}

func (a *LibvirtHostAgent) listVMs(req OpRequest) (OpResult, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	var items []map[string]any
	for _, v := range a.store.vms {
		if v.LabID == req.LabID {
			items = append(items, vmItem(v))
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	return OpResult{Op: req.Op, State: "ok",
		Message: fmt.Sprintf("%d VM(s) in lab %s", len(items), req.LabID), Items: items}, nil
}

func (a *LibvirtHostAgent) createVM(ctx context.Context, req OpRequest) (OpResult, error) {
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
	inv, err := a.Inventory(ctx)
	if err != nil {
		return OpResult{}, fmt.Errorf("host inventory for capacity check: %w", err)
	}
	a.store.mu.Lock()
	usedCPU, usedMem, usedDisk := a.store.usedResources()
	if err := CheckCapacity(prof, inv, usedCPU, usedMem, usedDisk); err != nil {
		a.store.mu.Unlock()
		return OpResult{}, err
	}
	if _, exists := a.store.byName["vm:"+req.Name]; exists {
		a.store.mu.Unlock()
		return OpResult{}, fmt.Errorf("VM %q already exists", req.Name)
	}
	diskPath, plan := diskPlan(req.LabID, req.Name, prof.DiskGB)
	v := &VM{
		ID: a.store.nextID("libvirt"), Name: req.Name, LabID: req.LabID, OwnerID: req.OwnerID,
		Profile: prof.Name, State: "defined", CPU: prof.VCPU, MemoryMB: prof.MemoryMB,
		DiskGB: prof.DiskGB, Hostname: hostname, Username: username,
		IP:       a.store.nextIP("10.42.0"),
		DiskPlan: plan,
		NetworkPlan: fmt.Sprintf("bridge cloudforge-br0, isolated lab network for lab %s; console via virsh console %s; SSH %s@%s",
			req.LabID, req.Name, username, "10.42.0.x (assigned on start)"),
		CloudInitYAML: userData,
		CreatedAt:     time.Now().UTC(),
	}
	xmlBytes, err := renderDomainXML(v, diskPath)
	if err != nil {
		a.store.mu.Unlock()
		return OpResult{}, err
	}
	a.store.mu.Unlock()

	// The hypervisor call is the only real-host step. If libvirt is
	// absent it returns Unavailable and no record is created — the VM
	// is not faked into existence.
	tmp, err := os.CreateTemp("", "cloudforge-domain-*.xml")
	if err != nil {
		return OpResult{}, fmt.Errorf("create domain XML temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(xmlBytes); err != nil {
		_ = tmp.Close()
		return OpResult{}, fmt.Errorf("write domain XML: %w", err)
	}
	_ = tmp.Close()
	if err := a.hypervisor(ctx, "define", tmp.Name()); err != nil {
		return OpResult{}, err
	}
	a.store.mu.Lock()
	a.store.vms[v.ID] = v
	a.store.byName["vm:"+v.Name] = v.ID
	a.store.mu.Unlock()
	return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: v.State,
		Message: fmt.Sprintf("VM %s defined from profile %s (libvirt)", v.Name, prof.Name)}, nil
}

func (a *LibvirtHostAgent) startVM(ctx context.Context, req OpRequest) (OpResult, error) {
	a.store.mu.Lock()
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	a.store.mu.Unlock()
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State == "running" {
		return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: v.State, Message: "VM already running"}, nil
	}
	if err := a.hypervisor(ctx, "start", v.Name); err != nil {
		return OpResult{}, err
	}
	a.store.mu.Lock()
	v.State = "running"
	a.store.mu.Unlock()
	return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: v.State, Message: "VM started (libvirt)"}, nil
}

func (a *LibvirtHostAgent) stopVM(ctx context.Context, req OpRequest) (OpResult, error) {
	a.store.mu.Lock()
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	a.store.mu.Unlock()
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State == "stopped" {
		return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: v.State, Message: "VM already stopped"}, nil
	}
	if err := a.hypervisor(ctx, "shutdown", v.Name); err != nil {
		return OpResult{}, err
	}
	a.store.mu.Lock()
	v.State = "stopped"
	a.store.mu.Unlock()
	return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: v.State, Message: "VM stopped (libvirt)"}, nil
}

func (a *LibvirtHostAgent) rebootVM(ctx context.Context, req OpRequest) (OpResult, error) {
	a.store.mu.Lock()
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	a.store.mu.Unlock()
	if v == nil {
		return OpResult{}, fmt.Errorf("VM not found (id=%q name=%q lab=%q)", req.ID, req.Name, req.LabID)
	}
	if v.State != "running" {
		return OpResult{}, fmt.Errorf("cannot reboot VM %q in state %q (must be running)", v.Name, v.State)
	}
	if err := a.hypervisor(ctx, "reboot", v.Name); err != nil {
		return OpResult{}, err
	}
	return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: "running", Message: "VM rebooted (libvirt)"}, nil
}

func (a *LibvirtHostAgent) deleteVM(ctx context.Context, req OpRequest) (OpResult, error) {
	a.store.mu.Lock()
	v := a.store.findVM(req.ID, req.Name, req.LabID)
	a.store.mu.Unlock()
	if v == nil {
		// Double-delete is idempotent.
		return OpResult{Op: req.Op, State: "deleted", Message: "VM already deleted"}, nil
	}
	if err := a.hypervisor(ctx, "destroy", v.Name); err != nil {
		return OpResult{}, err
	}
	if err := a.hypervisor(ctx, "undefine", v.Name, "--remove-all-storage"); err != nil {
		return OpResult{}, err
	}
	a.store.mu.Lock()
	delete(a.store.vms, v.ID)
	delete(a.store.byName, "vm:"+v.Name)
	a.store.mu.Unlock()
	return OpResult{Op: req.Op, ID: v.ID, Name: v.Name, State: "deleted", Message: "VM deleted (libvirt)"}, nil
}

// CleanupLab destroys and undefines every VM of the lab. Idempotent.
func (a *LibvirtHostAgent) CleanupLab(ctx context.Context, labID string) error {
	a.store.mu.Lock()
	var names []string
	for _, v := range a.store.vms {
		if v.LabID == labID {
			names = append(names, v.Name)
		}
	}
	a.store.mu.Unlock()
	for _, name := range names {
		if _, err := a.deleteVM(ctx, OpRequest{Op: OpDeleteVM, LabID: labID, Name: name}); err != nil {
			return err
		}
	}
	return nil
}

// Usage returns per-VM usage from the record store (libvirt per-domain
// stats via virsh domstats are M4 work; running VMs report their
// configured memory and a zero CPU sample rather than a fabricated one).
func (a *LibvirtHostAgent) Usage(ctx context.Context, labID string) ([]ResourceUsage, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	out := []ResourceUsage{}
	for _, v := range a.store.vms {
		if v.LabID == labID {
			out = append(out, ResourceUsage{Kind: "vm", ID: v.ID, Name: v.Name, LabID: v.LabID, State: v.State, CPUPercent: v.CPUPercent, MemoryMB: v.MemoryMB, MemoryUsed: v.MemoryUsed})
		}
	}
	return out, nil
}
