# DynamoDB — CloudForge reference notes (Workstream 4, v3 backup)

> **Reference notes only — not a built console.** Created 2026-11-22 by Workstream 4 from the
> service registry (`backend/internal/httpapi/services.go`), `docs/SERVICE_FIDELITY.md` and
> `docs/IMPLEMENTATION_STATUS.md`, plus the public console structure of the equivalent
> service. CloudForge branding only: no AWS logos, no AWS trademarks, no copied
> assets or screenshots, and no claim this is AWS. A later builder should be able to
> work from this file alone; re-check the live service's console before building,
> because console layouts change.

## Status

| Field | Value |
|---|---|
| Service id | `dynamodb` |
| Name | DynamoDB |
| Category | Databases |
| Registry route | `/services/dynamodb` |
| Availability today | available |
| Fidelity today | **PARTIAL** |
| CloudForge console today | Yes — frontend/src/pages/DynamoDbConsole.tsx, route `/services/dynamodb` |
| Resource types | table, item |
| Learning tracks linked | databases |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

DynamoDB is the managed key-value / NoSQL database: tables with a partition key (and optional sort key), single-digit-millisecond reads and writes at any scale, secondary indexes, streams, and on-demand or provisioned capacity. A learner meets it as the default NoSQL choice for serverless applications, and as the data source behind Lambda, AppSync and Step Functions exercises.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Tables, Explore items, PartiQL editor, Backups, Exports to S3, Streams, Reserved capacity, Settings.
**Home — Tables list:** columns Name, Status (Active / Creating / Deleting), Partition key, Sort key, Indexes (count), Table size, Item count, Created. Filters: search, status.
**Table detail tabs:** Overview (table details, capacity mode, TTL, PITR, deletion protection, tags), Explore table items (items table with Put item / query-scan builder: partition key value, sort key condition, filters, Scan/Query toggle, results table, edit/delete item), Indexes (GSI/LSI table: name, partition/sort key, projected attributes, status), Monitor (read/write capacity, throttled requests, latency graphs), Backups (on-demand backups, PITR), Additional settings (TTL, streams, alarms).
**PartiQL editor:** SQL-like statement box (SELECT/INSERT/UPDATE/DELETE), Run, results table.
**Wizard — Create table:** 1. Table details (table name, partition key + type String/Number/Binary, sort key optional). 2. Table settings (default/customize: capacity mode on-demand/provisioned with read/write units, auto scaling, secondary indexes, TTL, PITR, deletion protection, encryption, tags). 3. Review.
**Actions:** Create table, Delete table, Create item, Edit item, Delete item, Run query/scan, Create index, Create backup, Restore table, Export to S3.

## 3. CloudForge fidelity today

- **Fidelity: PARTIAL** (availability: available). Registry description: _Key-value / NoSQL tables — DynamoDB Local when it answers, else a Postgres-backed subset._
- **What the current console shows (registry mirrors note):** Tables list; table detail tabs Overview / Items / Indexes / Monitoring; Create-table wizard; Explore-items (Put/Get/Query/Scan/Delete)
- **How it differs / what is missing (registry differs note):** API_EMULATED via DynamoDB Local when reachable (real DynamoDB JSON API: CreateTable/PutItem/GetItem/Query/Scan/DeleteItem/DeleteTable); otherwise the Postgres-backed subset (PARTIAL, hash key only, no sort key/secondary indexes, Scan capped at 100). Never full DynamoDB; no provisioned throughput billing, no global tables.
- **Console today:** Yes — frontend/src/pages/DynamoDbConsole.tsx, route `/services/dynamodb`. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend exists — add sort keys, indexes and streams, do not rebuild the table layer.** Current: DynamoDB Local when it answers (real DynamoDB JSON API: CreateTable/PutItem/GetItem/Query/Scan/DeleteItem/DeleteTable) else the Postgres-backed subset (hash key only, no sort key/secondary indexes, Scan capped at 100) (backend/internal/providers/dynamodb.go, httpapi/phase3.go). Console: DynamoDbConsole.tsx. To build later, in priority order:
1. **Sort keys + Query with sort-key conditions:** extend the Postgres subset tables (`p3_dynamodb_tables`, `p3_dynamodb_items`) with a `sort_key` column and a composite primary key; Query supports partition-key equality plus sort-key operators (`=`, `<`, `<=`, `>`, `>=`, `BETWEEN`, `begins_with`). When DynamoDB Local is present, it already supports this — the gap is only the fallback.
2. **Global secondary indexes:** `pX_dynamodb_indexes` (table, index_name, partition_key, sort_key, projection) + Query/Scan by index (implemented as a Postgres index on the projected key columns). LSI is out of scope (same limitation as the fallback's hash-only start).
3. **PartiQL:** a small parser for SELECT/INSERT/UPDATE/DELETE with `WHERE` on keys, translated to the same item operations; anything untranslatable returns a clear error.
4. **Streams:** a change log (`pX_dynamodb_stream_records`: table, sequence, event INSERT/MODIFY/REMOVE, old/new image) fed by item writes; a `GetRecords`-style endpoint for Lambda triggers (the Lambda trigger path already exists for SQS/EventBridge — wire DynamoDB Streams as a third trigger source).
5. **TTL + PITR:** TTL = a worker sweep deleting expired items (attribute name per table); PITR = the existing Postgres backup story, not continuous restore — label it as backup-based.
**Data model (extend):** add sort_key to `p3_dynamodb_items`, new tables above.
**Labs / lessons:** add one lesson to the `databases` track: design a table with partition + sort key, add a GSI, query both access patterns, enable a stream and watch a Lambda trigger. Reuse the existing DynamoDB lesson.
**Lite feasibility:** Yes — Postgres subset already works in lite; DynamoDB Local is a full-mode optional service. Sort keys/indexes/streams are all Postgres work.

## 5. Lite feasibility and scope

- **Scope: medium.** Sort keys + GSIs + PartiQL + Streams on the existing DynamoDB subset.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `dynamodb`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
