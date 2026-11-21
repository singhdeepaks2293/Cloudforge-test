# CloudForge AWS Providers (Milestone 5)

How CloudForge implements AWS services locally, what fidelity each
operation honestly has, and what was actually verified. Nothing in
this document claims CloudForge *is* AWS.

## Fidelity labels

| Label | Meaning |
|---|---|
| `REAL` | Real AWS, or real local infrastructure doing the real job. **Nothing in M5 claims REAL.** |
| `API_EMULATED` | A real local server speaks the AWS API (MinIO speaks S3; DynamoDB Local speaks DynamoDB). The API is real; the service behind it is not AWS. |
| `SIMULATED` | Behaviour is simulated in-process for teaching (STS/KMS/SQS/SNS/EventBridge, VPC networking, Lambda-in-Docker). |
| `PARTIAL` | Genuinely implemented, but only a subset (IAM policy evaluation). |
| `UNSUPPORTED` | Registered explicitly so callers get a clear "not implemented" instead of silence (EKS). |

Health is never assumed: every capability starts `unknown` and a
probe sets `ok` / `degraded` / `unavailable` with a `checked_at`
timestamp (`Registry.RefreshHealth`, `GET /api/v1/aws/{service}/capabilities`
re-probes on demand; the API also probes once at startup). The
capability registry is the source of truth — the store layer has no
capability table, so capabilities are seeded in memory on every
startup (`backend/internal/providers/providers.go`) and are **not**
persisted to Postgres. That is a deliberate M5 decision, documented
here so a later milestone can add a table if it needs one.

## Per-service implementation, fidelity and limitations

| Service | Operations | Implementation | Fidelity | Limitations |
|---|---|---|---|---|
| s3 | CreateBucket, ListBuckets, PutObject, GetObject | MinIO (`minio-go/v7` v7.0.95) at `MINIO_ENDPOINT` (default `localhost:9000`) | API_EMULATED | Local MinIO only, no global S3 namespace; versioning/replication/lifecycle only if the MinIO server is configured for them; unreachable MinIO → 503 with capability info. |
| dynamodb | CreateTable, PutItem, GetItem, ListTables | Raw HTTP JSON API (`X-Amz-Target: DynamoDB_20120810.*`) at `DYNAMODB_ENDPOINT` (default `http://localhost:8000`) | API_EMULATED | DynamoDB Local / emulator only, not AWS DynamoDB; no provisioned-throughput billing, no global tables; unreachable endpoint → 503. |
| iam | EvaluatePolicy | Real evaluation of a subset, in `backend/internal/providers/iam.go` | PARTIAL | Allow/Deny, wildcard action/resource matching (`*`, `?`; actions case-insensitive, resources case-sensitive), explicit deny wins, `StringEquals` on `aws:username` only. No principals, `NotAction`/`NotResource`, policy variables, or other condition operators — a statement carrying one never matches. Not AWS IAM. |
| sts | AssumeRole, GetCallerIdentity | In-memory simulator | SIMULATED | Credentials are locally generated (`ASIA…`, 1h expiry) and grant no AWS access; no trust/session-policy evaluation. |
| kms | CreateKey, Encrypt, Decrypt | In-memory simulator: AES-256-GCM, server-side key | SIMULATED | Educational only — **NOT real KMS**: no HSM, no rotation, keys live in process memory and are lost on restart; ciphertext is only decryptable by this process. |
| sqs | CreateQueue, SendMessage, ReceiveMessage | In-memory simulator | SIMULATED | Queues lost on restart; no FIFO ordering guarantees, no DLQ, no visibility timeout — `ReceiveMessage` **removes** messages (no redelivery). |
| sns | CreateTopic, Publish, Subscribe | In-memory simulator | SIMULATED | Fan-out to subscribed in-memory SQS queues only; no email/HTTP/Lambda delivery. |
| eventbridge | PutEvents, PutRule | In-memory simulator | SIMULATED | Rules match exact `source` + `detail-type` only; no full event-pattern language, no AWS service events; targets are in-memory SQS queues. |
| ec2 | RunInstances, DescribeInstances | AWS emulator (`AWS_EMULATOR_ENDPOINT`) | API_EMULATED | Emulated API only; instances are local KVM teaching profiles (Micro/Small/Medium), not AWS hardware; unreachable emulator → `unavailable`. |
| lambda | InvokeFunction | Docker containers | SIMULATED | No Lambda scaling, VPC attachment, or managed runtimes beyond local images. |
| vpc | CreateVpc | Rust network engine | SIMULATED | Linux namespaces/bridges/nftables simulation, not AWS VPC; no AZs. |
| eks | CreateCluster | none | UNSUPPORTED | Not implemented; optional local Kubernetes is a later milestone. |

## API

| Method & path | Auth | Notes |
|---|---|---|
| `GET /api/v1/aws/{service}/capabilities` | — | Per-service capabilities + freshly probed `health`; `service` ∈ s3, dynamodb, iam, sts, kms, sqs, sns, eventbridge, ec2, lambda, vpc, eks; unknown service → 404. For `ec2` the response also carries the live `emulator` probe result. |
| `POST /api/v1/aws/s3/buckets` | Bearer | `{name}` → 201; MinIO unreachable → 503 with `capability` info; bad name → 400. |
| `GET /api/v1/aws/s3/buckets` | — | MinIO unreachable → 503 with `capability` info. |
| `POST /api/v1/aws/iam/evaluate` | Bearer | `{policies, action, resource, username?}` → `{decision: Allow\|Deny, reason, matched_statement?}`. |
| `GET /api/v1/providers/capabilities` | — | Unchanged: the full registry (Redis-cached 60s when available). |

## Emulator integration (LocalStack / Floci)

`AWS_EMULATOR_ENDPOINT` (default `http://localhost:4566`) is
health-probed at startup and on demand. The probe is
`GET {endpoint}/_localstack/health`, which LocalStack answers with
`{"services": {"s3": "available", …}, "version": "…"}`; the probe
records per-service availability in its detail. If nothing answers,
every emulator-backed operation (currently EC2) reports
`unavailable` with the reason — never silently ok.

### What was actually checked (2026-11-21)

Web research done for this milestone (sources: LocalStack official
docs at `docs.localstack.cloud`, LocalStack pricing page via
third-party coverage, Floci project site `floci.io`, the Floci
chronicle "State of the Flock #005", and community write-ups):

- **LocalStack — licensing changed in March 2026.** The old
  Community Edition (`localstack/localstack`) was sunset; LocalStack
  now ships a unified distribution that requires an account and an
  auth token (`LOCALSTACK_AUTH_TOKEN`, or the `lstk` CLI) even for
  the free Hobby plan. The free Hobby plan lists 30+ emulated
  services; Base is $39/license/month (annual billing) with 55+
  services; Ultimate lists 110+ services. Installation is Docker
  (`localstack/localstack`, gateway on port 4566, plus 4510–4559 for
  external services).
- **LocalStack — coverage conflict, reported honestly.** LocalStack's
  own material lists core services (S3, Lambda, DynamoDB, SQS, SNS,
  EC2, IAM) in the free tier, while several 2026 third-party
  write-ups (MiniStack, fakecloud) claim core services moved behind
  the paid plan after March 2026. CloudForge does **not** resolve
  that conflict here: check the current feature-coverage page
  (`docs.localstack.cloud` → feature coverage) before depending on
  any service being in the free tier. CloudForge's own emulator
  integration does not depend on the answer — it probes whatever is
  actually running at `AWS_EMULATOR_ENDPOINT` and reports what it
  finds.
- **Floci — active MIT-licensed alternative.** `floci/floci`
  (Java, `github.com/floci-io/floci`), a drop-in LocalStack
  replacement on the same port 4566 with no account, token, or
  feature gates. Reported as ~29,600–32,000 GitHub stars and 125–135
  AWS services in November 2026, with releases 2.4.0/2.5.0 on a
  twice-monthly train and a web console at
  `http://localhost:4566/_floci/ui`. Floci's own chronicle also notes
  that AWS released its own free local emulator ("AWS Emulator",
  CC0) on 2026-11-11 — that claim comes from Floci's blog, not from
  an AWS source checked here, and CloudForge has not verified it.
- **What CloudForge does NOT do with an emulator yet:** EC2
  operations are registered against the emulator endpoint but no
  EC2 API calls are issued in M5 — the emulator probe only drives
  health reporting. Wiring `RunInstances`/`DescribeInstances`
  through to a live emulator is follow-up work, gated on having a
  reachable emulator to test against (none was reachable in this
  sandbox; see below).

### Sandbox status (2026-11-21)

In this sandbox **nothing emulator-like is reachable**: `curl` to
`localhost:9000` (MinIO), `localhost:8000` (DynamoDB Local) and
`localhost:4566/_localstack/health` (LocalStack/Floci) all fail to
connect, and no `minio`, `localstack` or `docker` binary is
installed. So in this sandbox every S3/DynamoDB/EC2 operation
returns 503/unavailable with capability info, the IAM/STS/KMS/SQS/
SNS/EventBridge simulators work in-process, and the capability
health for s3/dynamodb/ec2 is `unavailable` — all verified by the
test suite (`TestAWSWiringRegistersProbes`,
`TestS3EndpointsReturn503WhenMinIOUnreachable`) and by the live
`/api/v1/aws/*` routes. To verify against real backends, run MinIO
(`docker run -p 9000:9000 minio/minio server /data`), DynamoDB Local
(`docker run -p 8000:8000 amazon/dynamodb-local`), or Floci/
LocalStack on 4566 and re-check `GET /api/v1/aws/{service}/capabilities`.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO S3 endpoint (host:port) |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | `minioadmin` / `minioadmin` | MinIO credentials (dev defaults; secret never logged) |
| `MINIO_USE_SSL` | `false` | Use https for MinIO |
| `DYNAMODB_ENDPOINT` | `http://localhost:8000` | DynamoDB Local / emulator URL |
| `AWS_EMULATOR_ENDPOINT` | `http://localhost:4566` | LocalStack/Floci endpoint URL |

## Tests

`backend/internal/providers/` — IAM evaluation (allow/wildcards/
explicit-deny-wins/implicit deny/`StringEquals` username/unsupported
conditions), S3 adapter against a fake HTTP S3 test double (labelled
as such in `s3_test.go`; it decodes minio-go's aws-chunked streaming
signatures) plus unreachable-backend cases, DynamoDB adapter against
a fake HTTP DynamoDB test double, simulator round-trips (STS creds,
KMS AES-GCM encrypt/decrypt, SQS/SNS fan-out, EventBridge matching),
emulator probe (unreachable + LocalStack health payload), registry
health refresh (including a panicking probe → `unavailable`), and the
no-REAL fidelity audit. `backend/internal/httpapi/aws_test.go` covers
the new routes (capabilities per service, 503-with-capability for
S3 without a backend, IAM evaluate allow/deny/auth).
