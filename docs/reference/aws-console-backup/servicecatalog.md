# Service Catalog — CloudForge reference notes (Workstream 4, v3 backup)

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
| Service id | `servicecatalog` |
| Name | Service Catalog |
| Category | Additional |
| Registry route | `/services/servicecatalog` |
| Availability today | unavailable |
| Fidelity today | **UNSUPPORTED** |
| CloudForge console today | No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens |
| Resource types | portfolio, product |
| Learning tracks linked | none linked |
| Build-later scope | **medium** |
| Lite feasibility | see §5 |

## 1. Purpose

Service Catalog is the governed self-service provisioning service: administrators publish approved products (CloudFormation templates or Terraform configurations) into portfolios, grant IAM principals access, and attach constraints (launch roles, tag options, template rules); end users then launch products without holding the underlying permissions. A learner meets it as the answer to 'how do teams deploy safely without waiting on the platform team?' — governance as a product catalogue.

## 2. Real console layout (reference — mirror the structure, not the brand)

**Left nav:** Products, Portfolios, Provisioned products, TagOptions, Constraints (per portfolio), (Admin view vs End-user view toggle).
**Home — Products list (end-user view):** cards/table — columns Product name, Type (CloudFormation / Terraform / External), Owner, Created, Versions. Click a product for its detail (description, versions, launch button).
**Portfolios list (admin view):** columns Portfolio name, Description, Products (count), Created. Portfolio detail tabs: Products (associated products table + Associate product), Principals (IAM users/groups/roles with access + Grant access), Constraints (launch / notification / template / stack set constraints + Create constraint), TagOptions (key/value pairs + Associate), Sharing (accounts/organizations).
**Provisioned products list:** columns Name, Product, Status (AVAILABLE / UNDER_CHANGE / TAINTED / ERROR), Created, Last changed. Detail: outputs, resources (the underlying stack), events, parameters.
**Wizard — Create product:** 1. Product details (name, description, owner, distributor). 2. Version details (template: CloudFormation file/S3 URL or Terraform configuration; version title). 3. Review.
**Wizard — Create portfolio:** name, description, owner; then associate products, grant principals, add constraints.
**Wizard — Launch product:** provisioned product name, product version, parameters (from the template), tags; review and Launch.
**Actions:** Create product/portfolio, Associate product, Grant principal access, Create constraint, Launch product, Update provisioned product, Terminate provisioned product.

## 3. CloudForge fidelity today

- **Fidelity: UNSUPPORTED** (availability: unavailable). Registry description: _Portfolios and products._
- **What the current console shows (registry mirrors note):** (registry records no mirrors/differs note for this service)
- **How it differs / what is missing (registry differs note):** Not started.
- **Console today:** No — Generic service console only (frontend/src/pages/ServiceDetail.tsx) — operations table with fidelity badges, no dedicated screens. Where there is no dedicated console, the service is reached through the generic service console (operations table with fidelity badges; unsupported operations shown disabled).

## 4. What would be needed to build it later

**Backend endpoints (new):** portfolios CRUD `POST/GET/DELETE /api/v1/aws/servicecatalog/portfolios`, products CRUD `/api/v1/aws/servicecatalog/products` + versions, associations `/api/v1/aws/servicecatalog/portfolios/{id}/products`, principals `/api/v1/aws/servicecatalog/portfolios/{id}/principals`, constraints `/api/v1/aws/servicecatalog/portfolios/{id}/constraints`, tag options `/api/v1/aws/servicecatalog/tagoptions`, provisioned products `POST /api/v1/aws/servicecatalog/provisioned-products` (launch), `GET ...`, `POST .../{id}/terminate`, `POST .../{id}/update`.
**Provisioning reuse (the key design decision):** a product's template is a CloudFormation template or a Terraform configuration. Launching a CloudFormation product creates a CloudFormation stack through the existing engine (backend/internal/phase4/phase4.go) with the launch constraint's IAM role recorded (the role is a CloudForge IAM role; enforce that the launching user may not hold the underlying permissions — the launch runs as the constraint role, which is the whole point). Launching a Terraform product creates a Terraform run through the existing M7 runner (containerised, honest 503 without Docker). The provisioned product IS the stack/run — no duplicate state; the provisioned-product record stores the stack id / run id and mirrors its status. Constraints enforced at launch: launch role (assume the role), template constraints (parameter rules evaluated against the template parameters), tag options (only allowed tag values). Portfolio sharing across accounts = sharing with other CloudForge users (grantee user ids).
**Data model:** Postgres `pX_sc_portfolios` (id, name, description, owner, created_at) + `pX_sc_products` (id, name, type, owner, distributor, created_at) + `pX_sc_product_versions` (id, product_id, version_title, template_location, template_type, created_at) + `pX_sc_portfolio_products` (portfolio_id, product_id) + `pX_sc_principals` (id, portfolio_id, principal_type, principal_id) + `pX_sc_constraints` (id, portfolio_id, product_id, type, value jsonb) + `pX_sc_tagoptions` (id, key, values text[]) + `pX_sc_provisioned` (id, name, product_id, version_id, portfolio_id, stack_id, run_id, status, created_by, created_at).
**Labs / lessons:** one lesson in the `terraform`/governance area: publish a "standard VPC" product, grant a learner access, launch it as the learner, then try to launch with a disallowed parameter and watch the constraint refuse. Reuse the CloudFormation/Terraform lessons.
**Lite feasibility:** Yes for CloudFormation products (the engine is lite); Terraform products need Docker (full mode) and are disabled in lite with the reason.

## 5. Lite feasibility and scope

- **Scope: medium.** Portfolio/product/provisioned-product model provisioning through the CloudFormation + Terraform engines.
- **Lite feasibility:** see the build notes in §4 (the lite paragraph at the end of each build section states whether the work fits lite mode, full mode only, or is capability-gated).
- **Fidelity rule for the build:** label every operation with its real fidelity (REAL / API_EMULATED / SIMULATED / PARTIAL / UNSUPPORTED), disable unsupported operations with the reason, and never show a resource as READY until its operation actually succeeded (see `docs/SERVICE_FIDELITY.md` §3).
- **Verification for the build:** backend + frontend + tests + a live walk (create → list → detail tabs → action → delete) against a running stack, including a failure case (backend dependency down → honest failure, not READY).

## Sources

- `backend/internal/httpapi/services.go` (service registry row for `servicecatalog`)
- `docs/SERVICE_FIDELITY.md`, `docs/IMPLEMENTATION_STATUS.md`
- Public console structure of the equivalent service (docs + console walkthroughs); re-check before building.
