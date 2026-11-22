package jobs

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// stubAuditStore serves canned entries for the archive test.
type stubAuditStore struct {
	entries []*store.AuditEntry
}

func (s *stubAuditStore) Record(ctx context.Context, e store.AuditEntry) error {
	return nil
}
func (s *stubAuditStore) List(ctx context.Context, limit, offset int) ([]*store.AuditEntry, int, error) {
	return s.entries, len(s.entries), nil
}
func (s *stubAuditStore) ListFiltered(ctx context.Context, f store.AuditFilter) ([]*store.AuditEntry, int, error) {
	var out []*store.AuditEntry
	for _, e := range s.entries {
		if f.Matches(e) {
			out = append(out, e)
		}
	}
	return out, len(out), nil
}
func (s *stubAuditStore) CleanupOlderThan(ctx context.Context, days int) (int64, error) {
	return 0, nil
}

// TestExportAuditArchive verifies the Phase 7 (R10) contract: rows
// older than 90 days are written to a CSV archive (newer rows are
// not), so audit_cleanup never deletes history without an archive.
func TestExportAuditArchive(t *testing.T) {
	old := &store.AuditEntry{ID: "old-1", ActorID: "user-1", Action: "lab_created",
		ResourceType: "lab", ResourceID: "lab-1", CreatedAt: time.Now().AddDate(0, 0, -100)}
	fresh := &store.AuditEntry{ID: "new-1", ActorID: "user-1", Action: "lab_created",
		ResourceType: "lab", ResourceID: "lab-2", CreatedAt: time.Now()}
	dir := t.TempDir()
	if err := exportAuditArchive(context.Background(), &stubAuditStore{entries: []*store.AuditEntry{old, fresh}}, dir); err != nil {
		t.Fatalf("exportAuditArchive: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "audit-archive-*.csv"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("archive files = %v, err %v", matches, err)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(strings.NewReader(string(b)))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 { // header + the one old row
		t.Fatalf("archive rows = %d, want 2 (header + old row): %v", len(rows), rows)
	}
	if rows[1][0] != "old-1" {
		t.Fatalf("archived row id = %q, want old-1", rows[1][0])
	}
}
