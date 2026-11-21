package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/providers"
	"github.com/go-chi/chi/v5"
)

// AWS API (Milestone 5). The mutating and evaluate endpoints require
// auth; the read endpoints do not. When a backend is unreachable the
// handlers return 503 with the capability entry so the caller can
// see what is (not) available and why — never a faked success.

// knownAWSServices is the allowlist for /api/v1/aws/{service}/... .
var knownAWSServices = map[string]bool{
	"s3": true, "dynamodb": true, "iam": true, "sts": true, "kms": true,
	"sqs": true, "sns": true, "eventbridge": true, "ec2": true,
	"lambda": true, "vpc": true, "eks": true,
}

// handleAWSCapabilities returns one service's capabilities with
// freshly probed health (never assumed ok). Unknown service -> 404.
func (s *Server) handleAWSCapabilities(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	if !knownAWSServices[service] {
		s.writeError(w, r, http.StatusNotFound, "unknown_service", "unknown AWS service: "+service)
		return
	}
	// Fresh probe on demand (each probe is bounded to 2s and they
	// run concurrently); the result carries checked-at timestamps.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	s.Registry.RefreshHealth(ctx)

	caps := s.Registry.ListByService(service)
	if caps == nil {
		caps = []providers.Capability{}
	}
	health, _ := s.Registry.ServiceHealth(service)
	resp := map[string]any{"service": service, "health": health, "capabilities": caps}
	if service == "ec2" && s.AWS != nil {
		resp["emulator"] = s.AWS.EmulatorStatus(r.Context())
	}
	s.writeJSON(w, r, http.StatusOK, resp)
}

// backendUnavailable writes a 503 with the capability entry for the
// operation so the caller sees the fidelity, limitations and health.
func (s *Server) backendUnavailable(w http.ResponseWriter, r *http.Request, service, operation, detail string) {
	cap, _ := s.Registry.Get(service, operation)
	body := map[string]any{
		"error": map[string]any{
			"code":       "backend_unavailable",
			"message":    detail,
			"request_id": RequestID(r.Context()),
		},
		"capability": cap,
	}
	s.writeJSON(w, r, http.StatusServiceUnavailable, body)
}

func (s *Server) awsUnavailable(w http.ResponseWriter, r *http.Request, service, operation string) bool {
	if s.AWS == nil {
		s.backendUnavailable(w, r, service, operation, "AWS providers are not configured on this server")
		return true
	}
	return false
}

type createBucketRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleS3CreateBucket(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if s.awsUnavailable(w, r, "s3", "CreateBucket") {
		return
	}
	var req createBucketRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "bucket name is required")
		return
	}
	if err := s.AWS.S3.CreateBucket(r.Context(), req.Name); err != nil {
		if errors.Is(err, providers.ErrS3Unavailable) {
			s.backendUnavailable(w, r, "s3", "CreateBucket", err.Error())
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.audit(r, u.ID, "aws_s3_bucket_created", "s3-bucket", req.Name, nil)
	s.writeJSON(w, r, http.StatusCreated, map[string]any{"bucket": req.Name})
}

func (s *Server) handleS3ListBuckets(w http.ResponseWriter, r *http.Request) {
	if s.awsUnavailable(w, r, "s3", "ListBuckets") {
		return
	}
	buckets, err := s.AWS.S3.ListBuckets(r.Context())
	if err != nil {
		if errors.Is(err, providers.ErrS3Unavailable) {
			s.backendUnavailable(w, r, "s3", "ListBuckets", err.Error())
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if buckets == nil {
		buckets = []providers.S3Bucket{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"buckets": buckets})
}

type iamEvaluateRequest struct {
	Policies []providers.Policy `json:"policies"`
	Action   string             `json:"action"`
	Resource string             `json:"resource"`
	Username string             `json:"username"`
}

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
	if len(req.Policies) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "at least one policy is required")
		return
	}
	result := providers.Evaluate(req.Policies, providers.EvalRequest{
		Action: req.Action, Resource: req.Resource, Username: req.Username,
	})
	s.audit(r, u.ID, "aws_iam_evaluated", "iam-policy", "", map[string]any{
		"action": req.Action, "resource": req.Resource, "decision": result.Decision,
	})
	s.writeJSON(w, r, http.StatusOK, result)
}
