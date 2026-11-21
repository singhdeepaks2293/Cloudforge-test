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
		Service:        "dynamodb",
		Operation:      "CreateTable",
		Implementation: "dynamodb-local",
		Fidelity:       FidelityAPIEmulated,
		Supported:      true,
		Limitations: []string{
			"Backed by DynamoDB Local / an emulator (DYNAMODB_ENDPOINT), not AWS DynamoDB; no provisioned throughput billing, no global tables.",
			"Requires a reachable DynamoDB endpoint (DYNAMODB_ENDPOINT, default http://localhost:8000); otherwise unavailable (503).",
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
			"Requires a reachable DynamoDB endpoint (DYNAMODB_ENDPOINT); otherwise unavailable (503).",
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
			"Requires a reachable DynamoDB endpoint (DYNAMODB_ENDPOINT); otherwise unavailable (503).",
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
			"Requires a reachable DynamoDB endpoint (DYNAMODB_ENDPOINT); otherwise unavailable (503).",
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
		Implementation: "kms-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: keys are AES-256 keys held in process memory only — NOT real AWS KMS, no HSM, no key rotation, keys are lost on restart.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kms",
		Operation:      "Encrypt",
		Implementation: "kms-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: AES-256-GCM with a server-side in-memory key — NOT real AWS KMS envelope encryption; ciphertext is only decryptable by this process.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "kms",
		Operation:      "Decrypt",
		Implementation: "kms-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: decrypts only ciphertext produced by this simulator in this process lifetime — NOT real AWS KMS.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "CreateQueue",
		Implementation: "sqs-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: in-memory queues, lost on restart; no FIFO ordering guarantees, no dead-letter queues, no visibility timeout — ReceiveMessage removes the message.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "SendMessage",
		Implementation: "sqs-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: in-memory queues only; messages never leave this deployment.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sqs",
		Operation:      "ReceiveMessage",
		Implementation: "sqs-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: ReceiveMessage removes messages from the queue (no visibility timeout, no redelivery).",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "CreateTopic",
		Implementation: "sns-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: topics exist only in memory in this deployment; no email/HTTP/Lambda delivery.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "Publish",
		Implementation: "sns-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: delivery is fan-out to subscribed in-memory SQS queues only.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "sns",
		Operation:      "Subscribe",
		Implementation: "sns-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: only in-memory SQS queues can subscribe; no email/HTTP endpoints.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "eventbridge",
		Operation:      "PutEvents",
		Implementation: "eventbridge-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: rules match on source and detail-type only; no full event-pattern language, no AWS service events.",
		},
		Health: HealthUnknown,
	},
	{
		Service:        "eventbridge",
		Operation:      "PutRule",
		Implementation: "eventbridge-simulator",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Educational simulator: rules match on source and detail-type only and deliver to in-memory SQS queues.",
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
		Operation:      "InvokeFunction",
		Implementation: "docker",
		Fidelity:       FidelitySimulated,
		Supported:      true,
		Limitations: []string{
			"Functions run in isolated Docker containers, not AWS Lambda.",
			"No AWS Lambda scaling, VPC attachment or managed runtimes beyond the local images.",
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
			"Not implemented. Optional local Kubernetes cluster is a later milestone.",
		},
		Health: HealthUnavailable,
	},
}
