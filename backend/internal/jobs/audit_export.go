// Audit archive export (Phase 7, risk R10).
//
// audit_cleanup used to delete audit rows older than 90 days with
// no archive step. exportAuditArchive writes those rows to a CSV
// file under dir (one file per cleanup run, named by the cutoff
// date) BEFORE the delete runs. A failed export blocks the delete,
// so audit history is never silently lost. The file is a plain CSV
// an operator can open or ship to object storage; retention of the
// archive files themselves is the operator's backup policy (the
// API-side export jobs in httpapi prune to the newest N).
package jobs

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/store"
)

// exportAuditArchive writes every audit entry older than 90 days to
// dir/audit-archive-<cutoff-date>.csv. It pages through the store
// (1000 rows at a time) so a large audit log cannot exhaust memory.
// When there is nothing to archive it still writes the header-only
// file, so a cleanup run always leaves evidence it ran.
func exportAuditArchive(ctx context.Context, audit store.AuditStore, dir string) error {
	cutoff := time.Now().AddDate(0, 0, -90)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create audit archive dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("audit-archive-%s.csv", cutoff.Format("2006-01-02")))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create audit archive: %w", err)
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	defer cw.Flush()
	_ = cw.Write([]string{"id", "created_at", "actor_id", "action", "resource_type", "resource_id", "request_id"})
	written := 0
	for offset := 0; ; offset += 1000 {
		entries, _, err := audit.ListFiltered(ctx, store.AuditFilter{
			To: cutoff, Limit: 1000, Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list audit for archive: %w", err)
		}
		for _, e := range entries {
			_ = cw.Write([]string{e.ID, e.CreatedAt.Format(time.RFC3339), e.ActorID, e.Action, e.ResourceType, e.ResourceID, e.RequestID})
			written++
		}
		if len(entries) < 1000 {
			break
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write audit archive: %w", err)
	}
	return nil
}
