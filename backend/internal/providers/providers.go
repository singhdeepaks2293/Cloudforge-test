// Package providers is the capability registry (Milestones 1, 4, 5).
//
// Every AWS operation CloudForge exposes is registered here with its
// implementation type and fidelity label. Fidelity is never inflated:
// nothing in this package claims REAL — the local backends are real
// local infrastructure (MinIO, DynamoDB Local) speaking S3/DynamoDB
// APIs, which is API_EMULATED, not AWS; the educational simulators
// are SIMULATED; unimplemented operations are UNSUPPORTED.
//
// Health is never assumed: each capability starts "unknown" and a
// registered probe (see RegisterProbe / RefreshHealth) sets
// ok/degraded/unavailable with a checked-at timestamp. The registry
// is the source of truth for capabilities: the store layer has no
// capability table, so capabilities are NOT seeded into Postgres —
// they are seeded here in memory on every startup and the registry
// is the only place they live (see docs/providers.md).
package providers

import (
	"context"
	"sync"
	"time"
)

// Fidelity labels (see the master spec, section 4).
const (
	FidelityReal        = "REAL"
	FidelityAPIEmulated = "API_EMULATED"
	FidelitySimulated   = "SIMULATED"
	FidelityPartial     = "PARTIAL"
	FidelityUnsupported = "UNSUPPORTED"
)

// Health states. "unknown" means no probe has run yet — never "ok"
// by default.
const (
	HealthOK          = "ok"
	HealthDegraded    = "degraded"
	HealthUnavailable = "unavailable"
	HealthUnknown     = "unknown"
)

// Capability describes one AWS operation and how CloudForge implements it.
type Capability struct {
	Service               string    `json:"service"`
	Operation             string    `json:"operation"`
	Implementation        string    `json:"implementation"`
	Fidelity              string    `json:"fidelity"`
	Supported             bool      `json:"supported"`
	Limitations           []string  `json:"limitations"`
	ConfigurationRequired []string  `json:"configuration_required,omitempty"`
	Health                string    `json:"health"`
	CheckedAt             time.Time `json:"checked_at,omitempty"`
	HealthDetail          string    `json:"health_detail,omitempty"`
}

// HealthProbe checks one service's backend and reports its status.
// It must be quick (callers bound it to a few seconds) and must
// never panic: a probe failure is "unavailable", not a crash.
type HealthProbe func(ctx context.Context) (status, detail string)

// Registry is the in-memory capability registry.
type Registry struct {
	mu     sync.RWMutex
	caps   []Capability
	probes map[string]HealthProbe
}

// NewRegistry returns a registry seeded with the capabilities.
func NewRegistry() *Registry {
	return &Registry{caps: append([]Capability(nil), seeded...), probes: map[string]HealthProbe{}}
}

// List returns all registered capabilities.
func (r *Registry) List() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Capability(nil), r.caps...)
}

// ListByService returns the capabilities for one service, or nil.
func (r *Registry) ListByService(service string) []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Capability
	for _, c := range r.caps {
		if c.Service == service {
			out = append(out, c)
		}
	}
	return out
}

// Get returns the capability for a service+operation, if registered.
func (r *Registry) Get(service, operation string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.caps {
		if c.Service == service && c.Operation == operation {
			return c, true
		}
	}
	return Capability{}, false
}

// RegisterProbe attaches a health probe for a service. Probes run in
// RefreshHealth and update every capability of that service.
func (r *Registry) RegisterProbe(service string, probe HealthProbe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes[service] = probe
}

// RefreshHealth runs every registered probe concurrently (each bound
// to probeTimeout) and updates the matching capabilities' Health,
// CheckedAt and HealthDetail. Services without a probe stay
// "unknown". It never fails: a probe error is "unavailable".
func (r *Registry) RefreshHealth(ctx context.Context) {
	r.mu.RLock()
	probes := make(map[string]HealthProbe, len(r.probes))
	for svc, p := range r.probes {
		probes[svc] = p
	}
	r.mu.RUnlock()
	if len(probes) == 0 {
		return
	}
	type result struct {
		service string
		status  string
		detail  string
	}
	results := make(chan result, len(probes))
	for svc, p := range probes {
		go func(svc string, p HealthProbe) {
			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			status, detail := "unavailable", "probe failed"
			func() {
				defer func() {
					// A panicking probe is "unavailable", never a crash.
					_ = recover()
				}()
				if s, d := p(pctx); s != "" {
					status, detail = s, d
				}
			}()
			results <- result{service: svc, status: status, detail: detail}
		}(svc, p)
	}
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	for range probes {
		res := <-results
		for i := range r.caps {
			if r.caps[i].Service == res.service {
				r.caps[i].Health = res.status
				r.caps[i].CheckedAt = now
				r.caps[i].HealthDetail = res.detail
			}
		}
	}
}

// ServiceHealth aggregates one service's health: unavailable if any
// capability is unavailable, degraded if any is degraded, ok only
// when every capability is ok, otherwise unknown. The second return
// is false when the service has no capabilities at all.
func (r *Registry) ServiceHealth(service string) (string, bool) {
	caps := r.ListByService(service)
	if len(caps) == 0 {
		return HealthUnknown, false
	}
	health := HealthOK
	seen := false
	for _, c := range caps {
		switch c.Health {
		case HealthUnavailable:
			return HealthUnavailable, true
		case HealthDegraded:
			health = HealthDegraded
			seen = true
		case HealthOK:
			seen = true
		}
	}
	if !seen {
		return HealthUnknown, true
	}
	return health, true
}

// probeTimeout bounds each health probe.
const probeTimeout = 2 * time.Second

var seeded = []Capability{
	{
		Service:        "ec2",
		Operation:      "RunInstances",
		Implementation: "aws-emulator",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Emulated API surface only; instances are backed by local KVM VMs, not AWS hardware.",
			"Instance types are local teaching profiles (Micro/Small/Medium), not AWS instance types.",
			"Requires a reachable AWS emulator (AWS_EMULATOR_ENDPOINT); otherwise unavailable.",
		},
		ConfigurationRequired: []string{"AWS_EMULATOR_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "ec2",
		Operation:      "DescribeInstances",
		Implementation: "aws-emulator",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Returns only instances created through this CloudForge deployment.",
			"Requires a reachable AWS emulator (AWS_EMULATOR_ENDPOINT); otherwise unavailable.",
		},
		ConfigurationRequired: []string{"AWS_EMULATOR_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "CreateBucket",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by local MinIO (S3-compatible API), not AWS S3; bucket names are local to this deployment, no global S3 namespace.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT, default localhost:9000); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "ListBuckets",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Returns only buckets in the local MinIO server, not AWS S3.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "PutObject",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Object storage is local MinIO; no S3 versioning, replication or lifecycle unless the MinIO server is configured for them.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "GetObject",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Object storage is local MinIO; objects exist only in this deployment.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},

	{
		Service:        "s3",
		Operation:      "ListObjects",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Objects live only in the local MinIO server, not AWS S3.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "DeleteObject",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Objects live only in the local MinIO server; delete is idempotent.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "GetBucketVersioning",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Versioning is the MinIO bucket versioning flag (Enabled/Suspended); no per-object version listing yet.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "SetBucketVersioning",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Versioning is the MinIO bucket versioning flag (Enabled/Suspended).",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "GetBucketPolicy",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Bucket policies are MinIO bucket policies; ACLs are not exposed by the MinIO adapter.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "SetBucketPolicy",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Bucket policies are MinIO bucket policies; ACLs are not exposed by the MinIO adapter.",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "s3",
		Operation:      "DeleteBucket",
		Implementation: "minio-s3",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Only an empty bucket can be deleted (MinIO/S3 rule).",
			"Requires a reachable MinIO server (MINIO_ENDPOINT); otherwise unavailable (503).",
		},
		ConfigurationRequired: []string{"MINIO_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "CreateTable",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by DynamoDB Local / an emulator (DYNAMODB_ENDPOINT) when it answers, not AWS DynamoDB; no provisioned throughput billing, no global tables.",
			"When DynamoDB Local is unreachable the API falls back to a Postgres-backed subset (hash key only) and says so in the response — it never 503s on a missing endpoint.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "PutItem",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by DynamoDB Local / an emulator, not AWS DynamoDB.",
			"When DynamoDB Local is unreachable the API falls back to a Postgres-backed subset and says so in the response.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "GetItem",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by DynamoDB Local / an emulator, not AWS DynamoDB; only items written through this deployment are visible.",
			"When DynamoDB Local is unreachable the API falls back to a Postgres-backed subset and says so in the response.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "ListTables",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by DynamoDB Local / an emulator, not AWS DynamoDB.",
			"When DynamoDB Local is unreachable the API falls back to a Postgres-backed subset and says so in the response.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "EvaluatePolicy",
		Implementation: "iam-simulator",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real evaluation of a subset of the IAM policy language only: Allow/Deny, wildcard action/resource matching, explicit deny wins, StringEquals on aws:username. No ARNs, principals, policy variables, or other condition operators.",
			"This is not AWS IAM; policies are evaluated locally and never leave this deployment.",
		},
		Health: HealthUnknown,
	},

	{
		Service:        "iam",
		Operation:      "CreateUser",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Teaching IAM identities stored in Postgres (migration 008); they are not AWS IAM and grant no AWS access.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "ListUsers",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Teaching IAM identities stored in Postgres; not AWS IAM.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "DeleteUser",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Deleting a user also deletes its group memberships and access keys.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "CreateGroup",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Teaching IAM groups stored in Postgres; not AWS IAM.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "CreateRole",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Trust policies are recorded, not evaluated: assuming a role is an explicit admin API call; the session is short-lived and expiry-enforced.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "CreatePolicy",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Policy documents are validated as JSON and probe-evaluated by the IAM evaluator before storage; unsupported language (principals, variables, other condition operators) is rejected.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "CreateAccessKey",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"The secret is shown once at creation and only its SHA-256 hash is stored.",
			"Admin-only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "iam",
		Operation:      "AssumeRole",
		Implementation: "cloudforge-iam",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Sessions are short-lived (default 15 min, capped by the role's max session duration) and expiry is enforced on lookup; the token is shown once and only its hash is stored.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudwatch",
		Operation:      "GetMetrics",
		Implementation: "cloudforge-metrics",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Metrics are the API's real Prometheus series (request counts/latency, engine/host-agent health, lab and job events) plus live /readyz component health — not AWS CloudWatch.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudwatch",
		Operation:      "CreateAlarm",
		Implementation: "cloudforge-metrics",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Alarms are evaluated live against current metric values; alarm definitions are stored in memory and lost on restart.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudtrail",
		Operation:      "LookupEvents",
		Implementation: "cloudforge-audit",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Events are CloudForge's own audit log (Postgres), not AWS CloudTrail; admin-only; 90-day retention.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudtrail",
		Operation:      "ExportEvents",
		Implementation: "cloudforge-audit",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Export is CSV or JSON over the same filtered audit log; admin-only; at most 1000 events per export.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sts",
		Operation:      "AssumeRole",
		Implementation: "sts-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: returns locally generated credentials that grant no AWS access and are not accepted by any AWS service.",
			"Credentials live only in memory; no trust-policy or session-policy evaluation.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sts",
		Operation:      "GetCallerIdentity",
		Implementation: "sts-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: returns a local teaching account/user ARN, not a real AWS identity.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kms",
		Operation:      "CreateKey",
		Implementation: "cloudforge-kms",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Durable local keys (Phase 6): AES-256 key material is stored in Postgres wrapped under a master key derived from CLOUDFORGE_KMS_MASTER_KEY (or the JWT secret) — NOT real AWS KMS, no HSM. Rotation creates a new key version; previous versions still decrypt.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kms",
		Operation:      "Encrypt",
		Implementation: "cloudforge-kms",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real AES-256-GCM encryption with the durable local key — NOT real AWS KMS envelope encryption; ciphertext is only decryptable by this deployment.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kms",
		Operation:      "Decrypt",
		Implementation: "cloudforge-kms",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Decrypts ciphertext produced by this deployment's durable keys, including previous key versions after rotation — NOT real AWS KMS.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "CreateQueue",
		Implementation: "cloudforge-sqs",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted queues (migration 009) — NOT the old in-memory simulator: visibility timeout, retention and DLQ redrive are real.",
			"No FIFO queues, no message attributes filtering, no long polling (receive returns immediately).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "SendMessage",
		Implementation: "cloudforge-sqs",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; body capped at 256 KiB.",
			"No FIFO deduplication.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "ReceiveMessage",
		Implementation: "cloudforge-sqs",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; ReceiveMessage hides the message until the visibility timeout (it does NOT delete it); at max_receives the message moves to the DLQ instead of being returned.",
			"No long polling.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "DeleteMessage",
		Implementation: "cloudforge-sqs",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; the receipt handle is the message id.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "CreateTopic",
		Implementation: "cloudforge-sns",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted topics — NOT the old in-memory simulator.",
			"No email/SMS delivery.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "Publish",
		Implementation: "cloudforge-sns",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; fan-out to subscribed SQS queues (durable enqueue), HTTP(S) endpoints (loopback only, SSRF-guarded) and Lambda functions. Every delivery is recorded, success or failure.",
			"No email/SMS delivery.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "Subscribe",
		Implementation: "cloudforge-sns",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; protocols sqs, http, https, lambda. HTTP endpoints must pass the platform SSRF guard (loopback/emulator hosts only).",
			"No email/SMS subscriptions.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "eventbridge",
		Operation:      "PutEvents",
		Implementation: "cloudforge-eventbridge",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted rules; pattern matching is source / detail-type arrays plus nested detail subset matching — not the full AWS event-pattern language.",
			"AWS service events are NOT generated; events arrive through PutEvents only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "eventbridge",
		Operation:      "PutRule",
		Implementation: "cloudforge-eventbridge",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable, Postgres-persisted; targets are SQS queues, SNS topics and Lambda functions (validated at rule creation).",
			"No schedule expressions (no EventBridge Scheduler), no input transformers.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "vpc",
		Operation:      "CreateVpc",
		Implementation: "rust-network-engine",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulation on Linux network namespaces, bridges and nftables — not AWS VPC.",
			"No AWS availability zones; subnets map to local namespaces.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "lambda",
		Operation:      "CreateFunction",
		Implementation: "cloudforge-lambda",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real execution in a sandboxed subprocess (python3 / node): timeout, output cap, prlimit memory cap when available, temp workdir, uid 65534 when the API runs as root. NOT container isolation and NOT AWS Lambda — no VPC attachment, no managed scaling, no layers, no Docker invoker.",
			"Code is a whole program: the event arrives as JSON on stdin, the result is stdout. Do not run untrusted code from strangers.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "lambda",
		Operation:      "InvokeFunction",
		Implementation: "cloudforge-lambda",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real subprocess invocation (see CreateFunction); every invocation is recorded with duration, output, error and the sandbox that actually applied.",
			"Triggers from SQS queues and EventBridge rules are real; API Gateway integrations are real.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "Query",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Hash-key-only query via DynamoDB Local when it answers; the Postgres subset matches the hash key only (no sort key, no secondary indexes).",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "Scan",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Capped at 100 items; pagination is not followed.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "DeleteItem",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Deletes by partition key only (no sort key in the subset).",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "dynamodb",
		Operation:      "DeleteTable",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Deletes the table and (Postgres subset) its items.",
		},
		ConfigurationRequired: []string{"DYNAMODB_ENDPOINT"},
		Health:                HealthUnknown,
	},
	{
		Service:        "rds",
		Operation:      "CreateDBInstance",
		Implementation: "cloudforge-rds",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"A real Postgres database + role on the platform Postgres (PostgreSQL-compatible, not Aurora). Engine postgres only (14/15/16); mysql is rejected, not faked.",
			"The master password is generated, shown once at creation, and only its SHA-256 hash is stored. Start/stop per instance is UNSUPPORTED (shared platform database). No Multi-AZ. No containerised per-instance Postgres (full mode) — not built.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "rds",
		Operation:      "DeleteDBInstance",
		Implementation: "cloudforge-rds",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Drops the real Postgres database and its role on the platform Postgres.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "rds",
		Operation:      "CreateDBSnapshot",
		Implementation: "cloudforge-rds",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Snapshots are real pg_dump -Fc files on the API host. pg_dump must exist on the API host (it does not in the distroless image — snapshots 503 there).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "rds",
		Operation:      "RestoreDBInstanceFromDBSnapshot",
		Implementation: "cloudforge-rds",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Restore creates a NEW instance from the snapshot (real pg_restore into a fresh database + role).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elasticache",
		Operation:      "CreateCacheCluster",
		Implementation: "cloudforge-elasticache",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Lite mode: a real namespaced Redis database on the platform Redis (key prefix + best-effort ACL user). Engine redis only.",
			"Full mode (a dedicated Redis container) is NOT built — there is no Docker in this sandbox to verify container provisioning against; a full-mode create returns an honest 503.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elasticache",
		Operation:      "DeleteCacheCluster",
		Implementation: "cloudforge-elasticache",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Deletes the cluster's prefixed keys and its ACL user (best-effort).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elb",
		Operation:      "CreateLoadBalancer",
		Implementation: "cloudforge-elb",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: in-process round-robin with real HTTP/TCP health probes. No engine RPC, no nginx reconfiguration; the DNS name is a local teaching name, not registered anywhere.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elb",
		Operation:      "CreateListener",
		Implementation: "cloudforge-elb",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: listeners route to a target group; no real socket is opened.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elb",
		Operation:      "CreateTargetGroup",
		Implementation: "cloudforge-elb",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: targets are probed on demand (HTTP GET / TCP dial, 2 s); unprobed targets are never eligible for routing.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "elb",
		Operation:      "RegisterTargets",
		Implementation: "cloudforge-elb",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: targets are http(s) URLs or host:port addresses, not EC2 instance ids.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "route53",
		Operation:      "CreateHostedZone",
		Implementation: "cloudforge-route53",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: durable zones/records, resolvable through the resolve endpoint. No DNS server serves these records to the lab network.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "route53",
		Operation:      "ChangeResourceRecordSets",
		Implementation: "cloudforge-route53",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED: A, CNAME and TXT records only; other types are rejected, not faked.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "apigateway",
		Operation:      "CreateRestApi",
		Implementation: "cloudforge-apigateway",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real routing to Lambda functions and SSRF-guarded HTTP targets, with real request logging. REST and HTTP protocols; WebSocket APIs are not built.",
			"Invoke requires the same auth as the rest of the API (no API keys / usage plans).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "apigateway",
		Operation:      "CreateResource",
		Implementation: "cloudforge-apigateway",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real resource + method + integration records; integrations are lambda or http only (validated at creation).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "apigateway",
		Operation:      "CreateDeployment",
		Implementation: "cloudforge-apigateway",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"A deployment snapshots the current resources into a stage; later edits need a new deploy to take effect.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecr",
		Operation:      "CreateRepository",
		Implementation: "cloudforge-ecr",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real local registry: durable Postgres catalogue + content-addressed blobs on local disk (or a registry:2 container when ECR_REGISTRY_URL is set), speaking the Docker Registry HTTP API v2 subset (catalog, tags, manifests, blobs). No Docker Hub compatibility.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecr",
		Operation:      "DescribeImages",
		Implementation: "cloudforge-ecr",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Images are manifest + layer records in the local catalogue; image scan is an integrity scan (blob digest re-verification), NOT a vulnerability (CVE) scan.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecr",
		Operation:      "PutImage",
		Implementation: "cloudforge-ecr",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Push via the /v2/ registry API (docker CLI, monolithic blob upload) or the API push path (base64 blobs, 10 MiB cap per blob). Manifests are stored only after every referenced blob exists.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecs",
		Operation:      "CreateCluster",
		Implementation: "cloudforge-ecs",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Clusters, task definitions, services and tasks are real records; the runtime is SIMULATED — a task's command runs as a sandboxed subprocess on the API host, no Docker container is started and no image is pulled.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecs",
		Operation:      "RegisterTaskDefinition",
		Implementation: "cloudforge-ecs",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Family + revision versioning is real. Container commands run as argv (no shell); the image field is recorded and matched against the ECR catalogue, not pulled.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecs",
		Operation:      "CreateService",
		Implementation: "cloudforge-ecs",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Desired count is reconciled on every read and when a task stops (replacement tasks start for real). A task-definition update is a real rolling deployment; the previous revision is recorded.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ecs",
		Operation:      "RunTask",
		Implementation: "cloudforge-ecs",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED runtime: the task command runs as a sandboxed subprocess (timeout, output cap, prlimit memory cap when available, uid 65534 when the API runs as root). Task status comes from the real process; logs are real process output.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "fargate",
		Operation:      "RunTask",
		Implementation: "cloudforge-ecs",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Serverless container run on the same sandboxed-subprocess runtime as ECS — no cluster to manage, no Docker container, no AWS Fargate capacity. Labelled Fargate-style, not Fargate.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codebuild",
		Operation:      "CreateProject",
		Implementation: "cloudforge-codebuild",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Projects point at local git repos or directories under CLOUDFORGE_BUILD_SOURCE_ROOT (no remote URLs — no network fetch). Buildspec phases are the only build definition.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codebuild",
		Operation:      "StartBuild",
		Implementation: "cloudforge-codebuild",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real builds: git checkout from a local path, buildspec phases run as real shell commands in a subprocess with timeouts, real logs, artifacts copied to disk. No Docker build environment.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codepipeline",
		Operation:      "CreatePipeline",
		Implementation: "cloudforge-codepipeline",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Stages are source -> build -> deploy. Deploy targets are ECS service update and Lambda code update; Terraform deploy honestly fails without Docker.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codepipeline",
		Operation:      "StartPipelineExecution",
		Implementation: "cloudforge-codepipeline",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Stages run in order; a failed stage stops the execution and later stages never run. Stage status and detail are recorded per stage.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codedeploy",
		Operation:      "CreateApplication",
		Implementation: "cloudforge-codedeploy",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Deployment groups target an ECS service or a Lambda function. No EC2/on-premises instances, no blue/green traffic shifting.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "codedeploy",
		Operation:      "CreateDeployment",
		Implementation: "cloudforge-codedeploy",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Deployments apply the revision for real and wait for the target to reach it (ECS desired count RUNNING on the new revision; Lambda code verified). Rollback deploys the previous revision through the same path.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudformation",
		Operation:      "CreateStack",
		Implementation: "cloudforge-cloudformation",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Own template engine (JSON/YAML) with its own stack state — NOT Terraform. Provisions SQS, SNS, DynamoDB (subset), Lambda, S3 (MinIO) and RDS only; other types are UNSUPPORTED in the change set.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "cloudformation",
		Operation:      "CreateChangeSet",
		Implementation: "cloudforge-cloudformation",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Change sets are a real diff against the stack's current resources (CREATE/UPDATE/DELETE/UNSUPPORTED). Rollback on failure deletes resources created in the failed execution; resources deleted by an update are NOT restored.",
		},
		Health: HealthUnknown,
	},

	// Phase 6: advanced services.
	{
		Service:        "athena",
		Operation:      "StartQueryExecution",
		Implementation: "cloudforge-athena",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"PostgreSQL SQL, not Presto/Athena: read-only SELECT/WITH against the platform Postgres or an RDS instance database. Saved queries and execution history are durable.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "glue",
		Operation:      "StartCrawler",
		Implementation: "cloudforge-glue",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Crawls real sources only: Postgres information_schema, DynamoDB tables, S3 buckets (MinIO). A source that cannot be reached makes the run PARTIAL, never silently skipped.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kinesis",
		Operation:      "PutRecord",
		Implementation: "cloudforge-kinesis",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Durable Postgres-backed streams with sequence numbers and shard assignment by partition-key hash. No enhanced fan-out, no Kinesis Data Analytics.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kinesis",
		Operation:      "GetRecords",
		Implementation: "cloudforge-kinesis",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Records are read back in sequence-number order from Postgres; iterators are after_sequence cursors, not AWS shard iterators.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "firehose",
		Operation:      "PutRecord",
		Implementation: "cloudforge-firehose",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Delivery to S3 (MinIO — honest 503 when MinIO is down) or to a Postgres table as JSONB rows, with a real delivery record per put. No buffering, no Lambda transforms.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "quicksight",
		Operation:      "CreateDashboard",
		Implementation: "cloudforge-quicksight",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Dashboards are a saved Athena query plus a bar/line chart rendered from the live query results — no sample data, no SPICE, no sharing.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sagemaker",
		Operation:      "CreateTrainingJob",
		Implementation: "cloudforge-sagemaker",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real OLS linear regression in Go on a built-in or user-supplied tiny dataset (no scikit-learn in this environment). Metrics (MSE/RMSE/R2) are computed from the training data.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sagemaker",
		Operation:      "InvokeEndpoint",
		Implementation: "cloudforge-sagemaker",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Endpoints serve real predictions from the stored model artifact read off disk. Linear models only; no GPU, no autoscaling.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "bedrock",
		Operation:      "InvokeModel",
		Implementation: "cloudforge-bedrock-playground",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"SIMULATED concept playground only: template responses, no foundation model runs, nothing is sent to any AI provider, no paid third-party API.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "dms",
		Operation:      "CreateReplicationTask",
		Implementation: "cloudforge-dms",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real row copies between the platform Postgres and RDS instance databases (up to 5000 rows per table). Full load only — no CDC, no ongoing replication.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "datasync",
		Operation:      "CreateTask",
		Implementation: "cloudforge-datasync",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real copies between local workdir paths and S3 buckets with SHA-256 verification per file. Local paths are confined to the workdir root; S3 needs MinIO.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "transfer",
		Operation:      "CreateServer",
		Implementation: "none",
		Fidelity:       FidelityUnsupported,
		Supported:      false,
		Limitations: []string{
			"Not implemented. Phase 6 decision: Transfer Family stays UNSUPPORTED — no sshd/sftp-server binary exists on this host to run a real SFTP endpoint against. Use DataSync for file transfer.",
		},
		Health: HealthUnavailable,
	},
	{
		Service:        "guardduty",
		Operation:      "ListFindings",
		Implementation: "cloudforge-guardduty",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Findings are generated only from the real audit log (failed-login patterns: brute force, credential stuffing). No threat-intelligence feeds, no invented threats.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "securityhub",
		Operation:      "GetFindings",
		Implementation: "cloudforge-securityhub",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Aggregation of the real GuardDuty / Inspector / Config / Trusted Advisor findings in the shared findings table. No AWS Security Hub integration.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "inspector",
		Operation:      "StartAssessmentRun",
		Implementation: "cloudforge-inspector",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Real checks against real resources: open security groups, public S3 bucket policies, unencrypted lab instances. No agent-based CVE scanning.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "config",
		Operation:      "PutConfigRule",
		Implementation: "cloudforge-config",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Three rules evaluated against real state: S3 bucket versioning, no open SSH, no open ingress. Configuration history is recorded snapshots of lab resources.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "waf",
		Operation:      "CreateWebACL",
		Implementation: "cloudforge-waf",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Rules are really evaluated, but only by the /evaluate endpoint against a supplied request — they are NOT inline in the API request path, so nothing is live-blocked.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "secretsmanager",
		Operation:      "CreateSecret",
		Implementation: "cloudforge-secretsmanager",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Secrets are envelope-encrypted at rest (AES-256-GCM value, data key wrapped by a KMS key) and versioned. No automatic rotation schedule.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "vpc",
		Operation:      "CreateVpcPeeringConnection",
		Implementation: "cloudforge-vpc-peering",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"No engine peering RPC exists: the peering is real route-table entries in both VPCs' lab state, not kernel peering. CIDRs must not overlap.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "transitgateway",
		Operation:      "CreateTransitGateway",
		Implementation: "cloudforge-transit-gateway",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"No engine transit-gateway RPC exists: attachments are real route-table entries in the attached VPCs' lab state.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "vpc",
		Operation:      "CreateVpcEndpoint",
		Implementation: "cloudforge-vpc-endpoint",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Endpoint records routing lab traffic to platform services (s3, dynamodb, sqs, sns). The DNS name is a local teaching name, not registered.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "networkfirewall",
		Operation:      "CreateFirewall",
		Implementation: "cloudforge-network-firewall",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Rules are really evaluated (5-tuple, first match, default deny) and the policy is sent to the engine's firewall RPC when a lab is attached; engine sync failure is recorded, never claimed.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "vpc",
		Operation:      "CreateFlowLogs",
		Implementation: "cloudforge-flow-logs",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Flow-log records are built ONLY from real engine telemetry snapshots taken by /capture — no per-packet flows are synthesised.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "ssm",
		Operation:      "SendCommand",
		Implementation: "cloudforge-ssm",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Run command goes through the host agent's allowlisted ExecInLab; no host agent is an honest 503. Inventory is the real host-agent inventory.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "trustedadvisor",
		Operation:      "DescribeChecks",
		Implementation: "cloudforge-trusted-advisor",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Five checks against real state: idle labs, unattached/open security groups, quota usage, S3 buckets without versioning. A check with no matching resource creates no finding.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "costexplorer",
		Operation:      "GetCostAndUsage",
		Implementation: "cloudforge-cost-explorer",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Educational cost model at published teaching rates, computed from real usage (lab hours, S3 bytes, SQS messages, Lambda invocations, RDS/cache hours). Never an AWS bill.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "budgets",
		Operation:      "CreateBudget",
		Implementation: "cloudforge-budgets",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Budgets are evaluated against the real educational cost model on every read. No AWS Budgets integration, no email alerts.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "organizations",
		Operation:      "CreateOrganizationalUnit",
		Implementation: "cloudforge-organizations",
		Fidelity:       FidelityPartial,
		Supported:      true,
		Limitations: []string{
			"Organizational units over the real CloudForge user store (members are platform users, not AWS accounts). No service control policies are enforced.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "eks",
		Operation:      "CreateCluster",
		Implementation: "none",
		Fidelity:       FidelityUnsupported,
		Supported:      false,
		Limitations: []string{
			"Not implemented. Phase 4 decision: EKS stays UNSUPPORTED — no local k3s/kind cluster is built (it cannot be verified without Docker/KVM, and a managed control plane would break lite mode). Kubernetes concepts are taught hands-on through the ECS runtime; see GET /api/v1/aws/eks/info.",
		},
		Health: HealthUnavailable,
	},
}
