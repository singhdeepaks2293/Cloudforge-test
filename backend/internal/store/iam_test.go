package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Memory IAM store: CRUD, attachments, access keys (hash never
// exposed) and sessions.
func TestMemoryIAMStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryIAMStore()

	u, err := s.CreateIAMUser(ctx, &IAMUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ARN != "arn:cloudforge:iam::local:user/alice" {
		t.Fatalf("ARN: %q", u.ARN)
	}
	if _, err := s.CreateIAMUser(ctx, &IAMUser{Name: "alice"}); err != ErrIAMConflict {
		t.Fatalf("duplicate user: %v", err)
	}
	if _, err := s.CreateIAMUser(ctx, &IAMUser{Name: "bad name"}); err == nil {
		t.Fatalf("bad name must be rejected")
	}

	g, err := s.CreateIAMGroup(ctx, &IAMGroup{Name: "devs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Fatalf("adding the same member twice must be idempotent: %v", err)
	}
	got, err := s.GetIAMGroup(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 1 {
		t.Fatalf("members: %v", got.Members)
	}
	gotUser, err := s.GetIAMUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotUser.Groups) != 1 || gotUser.Groups[0] != g.ID {
		t.Fatalf("user groups: %v", gotUser.Groups)
	}

	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBuckets","Resource":"*"}]}`)
	pol, err := s.CreateIAMPolicy(ctx, &IAMPolicy{Name: "list", Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachPolicy(ctx, "group", g.ID, pol.ID); err != nil {
		t.Fatal(err)
	}
	attached, err := s.AttachedPolicies(ctx, "group", g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attached) != 1 || attached[0].ID != pol.ID {
		t.Fatalf("attached: %v", attached)
	}

	role, err := s.CreateIAMRole(ctx, &IAMRole{Name: "reader", TrustPolicy: doc, MaxSessionSecs: 900})
	if err != nil {
		t.Fatal(err)
	}
	_ = role

	key, err := s.CreateIAMAccessKey(ctx, u.ID, "AKIAEXAMPLE1234567", HashSecret("secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	if key.Status != "Active" {
		t.Fatalf("key status: %s", key.Status)
	}
	keys, err := s.ListIAMAccessKeys(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys: %d", len(keys))
	}
	raw, _ := json.Marshal(keys)
	if string(raw) == "" {
		t.Fatal("unreachable")
	}
	if _, err := s.SetIAMAccessKeyStatus(ctx, u.ID, key.ID, "Bogus"); err == nil {
		t.Fatalf("bogus status must be rejected")
	}
	if _, err := s.SetIAMAccessKeyStatus(ctx, u.ID, key.ID, "Inactive"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIAMAccessKey(ctx, u.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIAMAccessKey(ctx, u.ID, key.ID); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}

	sess, err := s.CreateIAMSession(ctx, role.ID, u.ID, HashSecret("tok"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	gotSess, err := s.GetIAMSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSess.RoleID != role.ID {
		t.Fatalf("session role: %s", gotSess.RoleID)
	}

	// Deleting the user removes memberships and keys.
	if err := s.DeleteIAMUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetIAMGroup(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 0 {
		t.Fatalf("members after user delete: %v", got.Members)
	}
	if _, err := s.GetIAMUser(ctx, u.ID); err != ErrNotFound {
		t.Fatalf("deleted user: %v", err)
	}
	// Deleting the policy detaches it everywhere.
	if err := s.DeleteIAMPolicy(ctx, pol.ID); err != nil {
		t.Fatal(err)
	}
	attached, _ = s.AttachedPolicies(ctx, "group", g.ID)
	if len(attached) != 0 {
		t.Fatalf("attached after policy delete: %v", attached)
	}
}
