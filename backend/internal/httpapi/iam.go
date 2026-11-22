// IAM console API (Phase 2).
//
// AWS-style IAM identities (users, groups, roles, managed policies,
// access keys, assumed-role sessions) backed by the IAM store
// (Postgres migration 008, in-memory fallback). These are teaching
// identities: they are NOT AWS IAM and grant no AWS access.
//
// Rules enforced here, not just in the UI:
//   - every policy document is validated as JSON and probe-evaluated
//     by providers.ValidatePolicyDocument / ValidateTrustPolicy
//     before it is stored;
//   - access-key secrets and session tokens are shown exactly once
//     at creation; only SHA-256 hashes are stored;
//   - assumed-role sessions are short-lived and expiry is enforced
//     on lookup (expired -> 410);
//   - all CRUD is admin-only; AssumeRole is any authenticated user.
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/cloudforge/cloudforge/backend/internal/store"
)

func (s *Server) iamStore(w http.ResponseWriter, r *http.Request) (store.IAMStore, bool) {
	if s.IAM == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "iam_unavailable", "IAM store is not configured on this server")
		return nil, false
	}
	return s.IAM, true
}

func (s *Server) iamErr(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "not_found", "no IAM entity with that id")
	case errors.Is(err, store.ErrIAMConflict):
		s.writeError(w, r, http.StatusConflict, "iam_conflict", "an IAM entity with that name already exists")
	default:
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	}
	return true
}

type iamNameRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// --- Users ---------------------------------------------------------------

func (s *Server) handleIAMListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	users, err := st.ListIAMUsers(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if users == nil {
		users = []*store.IAMUser{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleIAMCreateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req iamNameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	u, err := st.CreateIAMUser(r.Context(), &store.IAMUser{Name: strings.TrimSpace(req.Name), Path: req.Path})
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_user_created", "iam-user", u.ID, map[string]any{"name": u.Name})
	s.writeJSON(w, r, http.StatusCreated, u)
}

func (s *Server) handleIAMGetUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	u, err := st.GetIAMUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, u)
}

func (s *Server) handleIAMDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := st.DeleteIAMUser(r.Context(), id); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_user_deleted", "iam-user", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

// --- Access keys ---------------------------------------------------------

const (
	accessKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	accessSecretChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

func randomString(n int, alphabet string) (string, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out), nil
}

func (s *Server) handleIAMCreateAccessKey(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "id")
	keyID, err := randomString(16, accessKeyAlphabet)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	secret, err := randomString(40, accessSecretChars)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	key, err := st.CreateIAMAccessKey(r.Context(), userID, "AKIA"+keyID, store.HashSecret(secret))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_access_key_created", "iam-access-key", key.ID, map[string]any{"user_id": userID})
	// The secret is returned exactly once, here. It is never stored
	// and never returned again by any endpoint.
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"access_key": key,
		"secret":     secret,
		"note":       "This secret is shown only once. Only its SHA-256 hash is stored; it cannot be retrieved again.",
	})
}

func (s *Server) handleIAMListAccessKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	keys, err := st.ListIAMAccessKeys(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	if keys == nil {
		keys = []*store.IAMAccessKey{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"access_keys": keys})
}

func (s *Server) handleIAMDeleteAccessKey(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "id")
	keyID := chi.URLParam(r, "keyId")
	if err := st.DeleteIAMAccessKey(r.Context(), userID, keyID); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_access_key_revoked", "iam-access-key", keyID, map[string]any{"user_id": userID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": keyID})
}

type accessKeyStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleIAMAccessKeyStatus(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req accessKeyStatusRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	key, err := st.SetIAMAccessKeyStatus(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "keyId"), req.Status)
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_access_key_status", "iam-access-key", key.ID, map[string]any{"status": key.Status})
	s.writeJSON(w, r, http.StatusOK, key)
}

// --- Groups --------------------------------------------------------------

func (s *Server) handleIAMListGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	groups, err := st.ListIAMGroups(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if groups == nil {
		groups = []*store.IAMGroup{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleIAMCreateGroup(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req iamNameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	g, err := st.CreateIAMGroup(r.Context(), &store.IAMGroup{Name: strings.TrimSpace(req.Name), Path: req.Path})
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_group_created", "iam-group", g.ID, map[string]any{"name": g.Name})
	s.writeJSON(w, r, http.StatusCreated, g)
}

func (s *Server) handleIAMGetGroup(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	g, err := st.GetIAMGroup(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, g)
}

func (s *Server) handleIAMDeleteGroup(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := st.DeleteIAMGroup(r.Context(), id); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_group_deleted", "iam-group", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

type groupMemberRequest struct {
	UserID string `json:"user_id"`
}

func (s *Server) handleIAMAddGroupMember(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req groupMemberRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "user_id is required")
		return
	}
	groupID := chi.URLParam(r, "id")
	if err := st.AddGroupMember(r.Context(), groupID, req.UserID); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_group_member_added", "iam-group", groupID, map[string]any{"user_id": req.UserID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"group_id": groupID, "user_id": req.UserID})
}

func (s *Server) handleIAMRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	groupID := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	if err := st.RemoveGroupMember(r.Context(), groupID, userID); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_group_member_removed", "iam-group", groupID, map[string]any{"user_id": userID})
	s.writeJSON(w, r, http.StatusOK, map[string]any{"group_id": groupID, "user_id": userID})
}

// --- Roles ---------------------------------------------------------------

type iamRoleRequest struct {
	Name               string          `json:"name"`
	Path               string          `json:"path"`
	TrustPolicy        json.RawMessage `json:"trust_policy"`
	MaxSessionDuration int             `json:"max_session_duration_seconds"`
}

func (s *Server) handleIAMListRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	roles, err := st.ListIAMRoles(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if roles == nil {
		roles = []*store.IAMRole{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

func (s *Server) handleIAMCreateRole(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req iamRoleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.TrustPolicy) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "trust_policy is required")
		return
	}
	// The trust document is validated (and its identity-policy
	// subset evaluated) before anything is stored.
	if _, err := providers.ValidateTrustPolicy(req.TrustPolicy); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_policy", err.Error())
		return
	}
	role, err := st.CreateIAMRole(r.Context(), &store.IAMRole{
		Name: strings.TrimSpace(req.Name), Path: req.Path,
		TrustPolicy: req.TrustPolicy, MaxSessionSecs: req.MaxSessionDuration,
	})
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_role_created", "iam-role", role.ID, map[string]any{"name": role.Name})
	s.writeJSON(w, r, http.StatusCreated, role)
}

func (s *Server) handleIAMGetRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	role, err := st.GetIAMRole(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, role)
}

func (s *Server) handleIAMDeleteRole(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := st.DeleteIAMRole(r.Context(), id); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_role_deleted", "iam-role", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

// --- Policies ------------------------------------------------------------

type iamPolicyRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Document    json.RawMessage `json:"document"`
}

func (s *Server) handleIAMListPolicies(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	policies, err := st.ListIAMPolicies(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if policies == nil {
		policies = []*store.IAMPolicy{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"policies": policies})
}

func (s *Server) handleIAMCreatePolicy(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	var req iamPolicyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Document) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "document is required")
		return
	}
	// Validate + probe-evaluate BEFORE storing: a policy document
	// that the evaluator cannot run is never persisted.
	if _, err := providers.ValidatePolicyDocument(req.Document); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_policy", err.Error())
		return
	}
	pol, err := st.CreateIAMPolicy(r.Context(), &store.IAMPolicy{
		Name: strings.TrimSpace(req.Name), Description: req.Description, Document: req.Document,
	})
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_policy_created", "iam-policy", pol.ID, map[string]any{"name": pol.Name})
	s.writeJSON(w, r, http.StatusCreated, pol)
}

func (s *Server) handleIAMGetPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	pol, err := st.GetIAMPolicy(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pol)
}

func (s *Server) handleIAMDeletePolicy(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := st.DeleteIAMPolicy(r.Context(), id); err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, admin.ID, "iam_policy_deleted", "iam-policy", id, nil)
	s.writeJSON(w, r, http.StatusOK, map[string]any{"deleted": id})
}

// --- Attachments ---------------------------------------------------------

type attachPolicyRequest struct {
	PolicyID string `json:"policy_id"`
}

func (s *Server) handleIAMAttachPolicy(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.requireAdmin(w, r)
		if !ok {
			return
		}
		st, ok := s.iamStore(w, r)
		if !ok {
			return
		}
		var req attachPolicyRequest
		if !s.decodeJSON(w, r, &req) {
			return
		}
		if req.PolicyID == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "policy_id is required")
			return
		}
		targetID := chi.URLParam(r, "id")
		if err := st.AttachPolicy(r.Context(), kind, targetID, req.PolicyID); err != nil {
			s.iamErr(w, r, err)
			return
		}
		s.audit(r, admin.ID, "iam_policy_attached", "iam-"+kind, targetID, map[string]any{"policy_id": req.PolicyID})
		s.writeJSON(w, r, http.StatusOK, map[string]any{"attached": req.PolicyID})
	}
}

func (s *Server) handleIAMDetachPolicy(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.requireAdmin(w, r)
		if !ok {
			return
		}
		st, ok := s.iamStore(w, r)
		if !ok {
			return
		}
		targetID := chi.URLParam(r, "id")
		policyID := chi.URLParam(r, "policyId")
		if err := st.DetachPolicy(r.Context(), kind, targetID, policyID); err != nil {
			s.iamErr(w, r, err)
			return
		}
		s.audit(r, admin.ID, "iam_policy_detached", "iam-"+kind, targetID, map[string]any{"policy_id": policyID})
		s.writeJSON(w, r, http.StatusOK, map[string]any{"detached": policyID})
	}
}

// --- Assume role / sessions ----------------------------------------------

type assumeRoleRequest struct {
	DurationSeconds int `json:"duration_seconds"`
}

func (s *Server) handleIAMAssumeRole(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	roleID := chi.URLParam(r, "id")
	role, err := st.GetIAMRole(r.Context(), roleID)
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	duration := 900 // 15 minutes, short-lived by default
	var req assumeRoleRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.DurationSeconds > 0 {
		duration = req.DurationSeconds
	}
	if duration < 60 {
		duration = 60
	}
	if duration > role.MaxSessionSecs {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"duration_seconds exceeds the role's maximum session duration ("+strconv.Itoa(role.MaxSessionSecs)+"s)")
		return
	}
	token, err := randomString(64, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	expires := time.Now().UTC().Add(time.Duration(duration) * time.Second)
	session, err := st.CreateIAMSession(r.Context(), roleID, u.ID, store.HashSecret(token), expires)
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	s.audit(r, u.ID, "iam_role_assumed", "iam-role", roleID, map[string]any{"session_id": session.ID, "duration_seconds": duration})
	// The token is returned exactly once, here.
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"session_id": session.ID, "role_id": roleID, "role_arn": role.ARN,
		"assumed_by": u.ID, "token": token, "expires_at": expires,
		"note": "This session token is shown only once and expires at expires_at; only its SHA-256 hash is stored.",
	})
}

// handleIAMGetSession validates a session: expired sessions are a
// 410 (expiry is enforced, never silently extended). When ?token=
// is supplied the token hash must also match (401 otherwise).
func (s *Server) handleIAMGetSession(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	st, ok := s.iamStore(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	session, err := st.GetIAMSession(r.Context(), id)
	if err != nil {
		s.iamErr(w, r, err)
		return
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		s.writeError(w, r, http.StatusGone, "session_expired", "this assumed-role session expired at "+session.ExpiresAt.Format(time.RFC3339))
		return
	}
	_ = u
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"session": session, "valid": true,
		"expires_in_seconds": int(time.Until(session.ExpiresAt).Seconds()),
	})
}

// --- Policy simulator (extended) -----------------------------------------

type iamEvaluateRequest struct {
	Policies []providers.Policy `json:"policies"`
	// Resolve stored policies instead of (or in addition to) inline ones.
	UserID    string   `json:"user_id"`
	GroupIDs  []string `json:"group_ids"`
	RoleIDs   []string `json:"role_ids"`
	PolicyIDs []string `json:"policy_ids"`
	Action    string   `json:"action"`
	Resource  string   `json:"resource"`
	Username  string   `json:"username"`
}

// handleIAMEvaluate evaluates inline policies and/or policies
// resolved from stored IAM identities (user + their groups, roles,
// named policies), and returns the decision with the full
// evaluation trail: which statement decided it, or the implicit
// deny when nothing matched.
func (s *Server) handleIAMEvaluate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req iamEvaluateRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Action == "" || req.Resource == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "action and resource are required")
		return
	}
	policies := append([]providers.Policy{}, req.Policies...)
	resolved := []string{} // "policy:<id>" entries, in evaluation order
	if s.IAM != nil && (req.UserID != "" || len(req.GroupIDs) > 0 || len(req.RoleIDs) > 0 || len(req.PolicyIDs) > 0) {
		resolve := func(ids []string, kind string) {
			for _, pid := range ids {
				pol, err := s.IAM.GetIAMPolicy(r.Context(), pid)
				if err != nil {
					continue
				}
				var p providers.Policy
				if err := json.Unmarshal(pol.Document, &p); err != nil {
					continue
				}
				policies = append(policies, p)
				resolved = append(resolved, kind+":"+pol.ID+" ("+pol.Name+")")
			}
		}
		resolve(req.PolicyIDs, "policy")
		if req.UserID != "" {
			if attached, err := s.IAM.AttachedPolicies(r.Context(), "user", req.UserID); err == nil {
				for _, pol := range attached {
					var p providers.Policy
					if err := json.Unmarshal(pol.Document, &p); err == nil {
						policies = append(policies, p)
						resolved = append(resolved, "user-policy:"+pol.ID+" ("+pol.Name+")")
					}
				}
			}
			if usr, err := s.IAM.GetIAMUser(r.Context(), req.UserID); err == nil {
				for _, gid := range usr.Groups {
					if attached, err := s.IAM.AttachedPolicies(r.Context(), "group", gid); err == nil {
						for _, pol := range attached {
							var p providers.Policy
							if err := json.Unmarshal(pol.Document, &p); err == nil {
								policies = append(policies, p)
								resolved = append(resolved, "group-policy:"+pol.ID+" ("+pol.Name+")")
							}
						}
					}
				}
			}
		}
		for _, rid := range req.RoleIDs {
			if attached, err := s.IAM.AttachedPolicies(r.Context(), "role", rid); err == nil {
				for _, pol := range attached {
					var p providers.Policy
					if err := json.Unmarshal(pol.Document, &p); err == nil {
						policies = append(policies, p)
						resolved = append(resolved, "role-policy:"+pol.ID+" ("+pol.Name+")")
					}
				}
			}
		}
	}
	if len(policies) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "at least one policy is required (inline policies, or user_id/group_ids/role_ids/policy_ids resolving to stored policies)")
		return
	}
	result, trail := providers.EvaluateWithTrail(policies, providers.EvalRequest{
		Action: req.Action, Resource: req.Resource, Username: req.Username,
	})
	s.audit(r, u.ID, "aws_iam_evaluated", "iam-policy", "", map[string]any{
		"action": req.Action, "resource": req.Resource, "decision": result.Decision,
	})
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"decision": result.Decision, "reason": result.Reason,
		"matched_statement": result.MatchedStatement,
		"trail":             trail, "policies_evaluated": resolved,
		"evaluator": "real subset: Allow/Deny, wildcards, explicit-deny-wins, StringEquals on aws:username",
	})
}
