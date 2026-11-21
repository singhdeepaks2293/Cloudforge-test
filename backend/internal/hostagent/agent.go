// Shared in-memory record store used by the simulated and libvirt
// agents. All lifecycle logic operates on these typed records; only the
// backend call (Docker API, libvirt hypervisor) differs.
package hostagent

import (
	"fmt"
	"sync"
	"time"
)

// memStore holds containers/VMs plus idempotency results.
type memStore struct {
	mu         sync.Mutex
	containers map[string]*Container
	vms        map[string]*VM
	byName     map[string]string // kind:name -> id ("container:<name>", "vm:<name>")
	idem       map[string]OpResult
	ctr        int
}

func newMemStore() *memStore {
	return &memStore{
		containers: map[string]*Container{},
		vms:        map[string]*VM{},
		byName:     map[string]string{},
		idem:       map[string]OpResult{},
	}
}

func (s *memStore) nextID(prefix string) string {
	s.ctr++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano()/1000, s.ctr)
}

// nextIP assigns a deterministic lab IP from a counter.
func (s *memStore) nextIP(base string) string {
	s.ctr++
	return fmt.Sprintf("%s.%d", base, 100+(s.ctr%100))
}

func (s *memStore) idemGet(req OpRequest) (OpResult, bool) {
	if req.IdempotencyKey == "" {
		return OpResult{}, false
	}
	key := req.LabID + "|" + string(req.Op) + "|" + req.IdempotencyKey
	res, ok := s.idem[key]
	if ok {
		res.Replayed = true
	}
	return res, ok
}

func (s *memStore) idemPut(req OpRequest, res OpResult) {
	if req.IdempotencyKey == "" {
		return
	}
	key := req.LabID + "|" + string(req.Op) + "|" + req.IdempotencyKey
	s.idem[key] = res
}

// usedResources sums vCPU / memory / disk over live (non-removed)
// resources. Disk only counts VMs.
func (s *memStore) usedResources() (vcpu, memMB, diskGB int) {
	for _, c := range s.containers {
		vcpu += c.CPU
		memMB += c.MemoryMB
	}
	for _, v := range s.vms {
		vcpu += v.CPU
		memMB += v.MemoryMB
		diskGB += v.DiskGB
	}
	return vcpu, memMB, diskGB
}

func (s *memStore) findContainer(id, name, labID string) *Container {
	if id != "" {
		if c, ok := s.containers[id]; ok && (labID == "" || c.LabID == labID) {
			return c
		}
	}
	if name != "" {
		if cid, ok := s.byName["container:"+name]; ok {
			if c, ok := s.containers[cid]; ok && (labID == "" || c.LabID == labID) {
				return c
			}
		}
	}
	return nil
}

func (s *memStore) findVM(id, name, labID string) *VM {
	if id != "" {
		if v, ok := s.vms[id]; ok && (labID == "" || v.LabID == labID) {
			return v
		}
	}
	if name != "" {
		if vid, ok := s.byName["vm:"+name]; ok {
			if v, ok := s.vms[vid]; ok && (labID == "" || v.LabID == labID) {
				return v
			}
		}
	}
	return nil
}

func containerItem(c *Container) map[string]any {
	return map[string]any{
		"id": c.ID, "name": c.Name, "image": c.Image, "lab_id": c.LabID,
		"state": c.State, "cpu": c.CPU, "memory_mb": c.MemoryMB, "ip": c.IP,
	}
}

func vmItem(v *VM) map[string]any {
	return map[string]any{
		"id": v.ID, "name": v.Name, "profile": v.Profile, "lab_id": v.LabID,
		"state": v.State, "cpu": v.CPU, "memory_mb": v.MemoryMB,
		"disk_gb": v.DiskGB, "hostname": v.Hostname, "ip": v.IP,
		"disk_plan": v.DiskPlan, "network_plan": v.NetworkPlan,
	}
}
