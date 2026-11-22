# AWS policy editors survey — 2026-11-22

Read-only survey in the AWS console, region ap-southeast-2, 20:10–20:22 IST. Nothing was created, edited, deleted, launched, submitted, or applied. Two IAM Create-policy drafts were built only to inspect the editor and Review step and were both abandoned with Cancel; no "Create policy" button was clicked; no create wizard for S3, SNS, SQS, KMS, VPC endpoint, or Lambda was opened.

## Headline result

**Visual + JSON editor: IAM Create policy only.** Every other policy editor was either JSON-only read-only (EventBridge default bus) or not observable, because no resource exists in the account to open (S3 bucket, SNS topic, SQS queue, KMS key, VPC endpoint, Lambda function). No "Policy generator" link was seen anywhere.

## 1. IAM > Policies > Create policy

Page structure: breadcrumbs "IAM / Create policy"; left nav Roles, Policies, IAM users, Root access management; heading "Create policy"; steps "Step 1 Specify permissions" and "Step 2 Review and create"; top-right "Actions" dropdown; editor tabs "Visual" (default) and "JSON"; foot "Cancel" / "Next".

Actions dropdown: "Import policy", "Generate CloudFormation template", "Optimize for readability" (disabled), "Optimize for size" (disabled). "Import policy" opens a dialog: "Find policies" search, table of 1,622 policies (Policy name, Used as, Description), pagination 163 pages. There is NO separate "Copy an existing policy" option and NO "Policy generator" link.

### Visual editor
- Initial: one statement block "(Service 1) Select a service" with "Choose a service" dropdown, plus "Add more permissions".
- Service picker: search box, "Commonly used services" (Auto Scaling, CloudFront, EC2, IAM, Lambda, RDS, S3, SNS) and "Other services" (alphabetical, ~250 services).
- After picking S3: block retitled "(Service 1) Service data for S3", "Clone" / "Remove" buttons, "Set permissions for S3".
- "Actions allowed": "Filter Actions" search, Effect radios Allow (default) / Deny, "Manual actions" / "Add actions" links, "All S3 actions (s3:*)" checkbox, "Expand all" / "Collapse all", access-level groups with counts: List (18), Read (66), Write (57), Permissions management (27), Tagging (12). Expanding List shows "All list actions" plus 18 individual checkboxes, each with an Info button. No free-text/wildcard action field on the Visual tab.
- "Resources" (greyed out until an action is selected): "All" / "Specific" radios (Specific default), one sub-section per resource type with Info button, warning, "Add ARNs" button, "Restrict access to any ARN … in this account" checkbox. Choosing "All" warns that `*` may be overly permissive.
- "Request conditions - optional": checkboxes "User is MFA Authenticated" and "Requested from IP", plus "Add another condition" (dialog with Condition key dropdown).
- "Add more permissions": adds "(Service 2) Select a service" block; Clone/Remove buttons appear.
- "Policy Linter" panel below: tabs Security / Errors / Warnings / Suggestions with counts.
- Validation on "Next": action selected but no resource → red banner "No resources specified — No resources are specified. Specify resources for the selected actions." and no advance.

### JSON editor (Step 1)
- Code editor with line numbers, pre-filled with Version 2012-10-17, one statement (Sid "Statement1", Effect Allow, empty Action/Resource arrays). Controls: "Open action palette (F1)", error/warning status, "6042 of 6144 characters remaining", "Expand Policy Editor to fill viewport".
- Right-hand "Edit statement" panel for the selected statement: "Remove", "Add actions" (Choose a service search + All services list), "Add a resource" (disabled until an action is added), "Add a condition - optional" (disabled until an action is added), "Add new statement".
- Same Policy Linter tabs and Cancel / Next below.

### Step 2 — Review and create
- "Policy details": Policy name (max 128 chars, alphanumeric and `+=,.@-_`), Description - optional (max 1000 chars).
- "Permissions defined in this policy": search box, "Allow (1 of 484 services)" with "Show remaining 483 services" toggle, table Service / Access level / Resource / Request condition (draft row: S3 / Limited: List / All resources / None).
- "Add tags": "No tags associated with the resource.", "Add new tag" (up to 50 tags).
- Foot: Cancel / Previous / Create policy. No JSON view on the Review page.

### Existing policy detail page (AdministratorAccess, AWS managed)
- Header: Type "AWS managed - job function", Creation/Edited Feb 07, 2015.
- Tabs: Permissions, Entities attached, Policy versions (1), Last Accessed.
- Permissions: Summary / JSON segmented control (Summary default), "Permissions defined in this policy", search, table Service / Access level / Resource / Request condition. JSON view is a read-only code block with Copy.
- Entities attached: "Attached as a permissions policy (1)" (Attach / Detach, table Entity name / Entity type) and "Attached as a permissions boundary" (empty).
- Policy versions: table Policy version / Creation time, "Version 1 (Default)".
- Last Accessed: activity for last 365 days, table Service / Last accessed / Access by entities ("Allowed services (464)").

## 2. S3 — bucket policy: not observable
Buckets list is empty (0 general purpose / directory / table buckets). The editor lives on a bucket's Permissions tab, "Bucket policy" section with Edit — no bucket to open, and no bucket was created. No Policy generator link seen.

## 3. SNS — topic access policy: not observable
Topics list is empty (0). Policy is on a topic's detail page (Details tab, "Access policy" section, Edit) — no topic to open, create wizard not opened. No Policy generator link seen.

## 4. SQS — queue access policy: not observable
Queues list is empty (0). Policy is on a queue's detail page (Details tab, "Access policy" section, Edit) — no queue to open, create wizard not opened. No Policy generator link seen.

## 5. KMS — key policy: not observable
Customer managed keys (0), AWS managed keys (0). Policy is on a key's detail page under a "Key policy" tab, and in the Create-key wizard's "Define key usage permissions" step — no key to open, wizard not opened. No Policy generator link seen.

## 6. VPC endpoint policy: not observable
Endpoints list: "No endpoint found." The entry point is visible: Actions dropdown contains "Manage policy" (alongside View details, Manage subnets, Manage security groups, Manage route tables, Modify private DNS, Modify IP settings, Manage tags, Delete) — all disabled with no endpoint selected. Create-endpoint wizard not opened. No Policy generator link seen.

## 7. Lambda — resource-based policy: not observable
Functions list is empty (0). Policy lives on a function's Configuration > Permissions tab under "Resource-based policy statements" — no function to open, wizard not opened. No Policy generator link seen.

## 8. EventBridge — default event bus Permissions tab: JSON-only, read-only
Tabs: Rules, Encryption, Permissions, Monitoring, Logs, Archives & Replays, Tags. Permissions tab: heading "Resource-based policy", a "Manage permissions" link (NOT opened), and a read-only text box containing "No resource-based policy." with a disabled Copy button. No Visual editor, no editable JSON editor.

## Summary table

| Policy | Visual + JSON | JSON-only | Not observable |
|---|---|---|---|
| IAM Create policy | Yes | — | — |
| IAM existing policy detail | — | Read-only JSON view (Summary/JSON) | — |
| EventBridge default bus | — | Read-only ("No resource-based policy.") | — |
| S3 bucket policy | — | — | No bucket exists |
| SNS topic policy | — | — | No topic exists |
| SQS queue policy | — | — | No queue exists |
| KMS key policy | — | — | No key exists |
| VPC endpoint policy | — | — | No endpoint exists; "Manage policy" menu visible but disabled |
| Lambda resource-based policy | — | — | No function exists |

## Gaps vs CloudForge v3.1 IAM editor (for follow-up)

CloudForge v3.1 (built 2026-11-22) has Visual + JSON tabs with service/action/resource/condition, live JSON preview, and unsupported-feature warnings — matching IAM's core. AWS has additionally, which CloudForge does not yet: access-level action groups with counts (List/Read/Write/Permissions management/Tagging), All/Specific resources with per-type ARN entry, MFA/IP request conditions (CloudForge only offers StringEquals on aws:username, which is all its backend evaluates), Policy Linter (Security/Errors/Warnings/Suggestions), Import policy dialog (1,622 policies), a Review step with tags, and policy detail tabs (Permissions Summary/JSON, Entities attached, Policy versions, Last Accessed).
