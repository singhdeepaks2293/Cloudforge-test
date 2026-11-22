package tflearn

// ProviderResource is one resource type in the documented CloudForge
// Terraform provider surface. The provider is a learning surface:
// cloudforge_* resources map onto real CloudForge APIs (the
// "CloudForge API" column); aws_* resources execute through the
// container runner against the local emulator (M7). Nothing here
// reaches real AWS, and no real AWS credentials are ever required —
// the provider works with the local endpoints only.
type ProviderResource struct {
	Type          string   `json:"type"`
	Description   string   `json:"description"`
	Arguments     []string `json:"arguments"`
	Attributes    []string `json:"attributes"`
	CloudForgeAPI string   `json:"cloudforge_api"`
	Fidelity      string   `json:"fidelity"`
	Example       string   `json:"example"`
}

// ProviderResources is the documented provider surface. Only
// resources with an actual provider + API mapping are listed —
// anything else is absent, not silently claimed.
var ProviderResources = []ProviderResource{
	{
		Type: "cloudforge_vpc", Description: "A virtual private cloud (lab VPC) with a CIDR block.",
		Arguments:     []string{"cidr_block (string, required)", "name (string, optional)", "dns_enabled (bool, optional)"},
		Attributes:    []string{"id", "cidr_block"},
		CloudForgeAPI: "POST /api/v1/labs/{id}/resources (kind vpc) — Phase 2 VPC lifecycle",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n  name       = \"main\"\n}",
	},
	{
		Type: "cloudforge_subnet", Description: "A subnet inside a cloudforge_vpc.",
		Arguments:     []string{"vpc_id (string, required)", "cidr_block (string, required)", "name (string, optional)", "public (bool, optional)"},
		Attributes:    []string{"id", "cidr_block"},
		CloudForgeAPI: "POST /api/v1/labs/{id}/resources (kind subnet) — creates the subnet on the Rust network engine",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_subnet\" \"public\" {\n  vpc_id     = cloudforge_vpc.main.id\n  cidr_block = \"10.0.1.0/24\"\n  public     = true\n}",
	},
	{
		Type: "cloudforge_security_group", Description: "A security group with ingress/egress rules.",
		Arguments:     []string{"vpc_id (string, required)", "name (string, required)", "ingress (block, optional)", "egress (block, optional)"},
		Attributes:    []string{"id"},
		CloudForgeAPI: "POST /api/v1/labs/{id}/resources (kind security-group) + VPC security-group API (Phase 2)",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_security_group\" \"web\" {\n  vpc_id = cloudforge_vpc.main.id\n  name   = \"web\"\n}",
	},
	{
		Type: "cloudforge_instance", Description: "A compute instance (lab VM) in a subnet.",
		Arguments:     []string{"subnet_id (string, required)", "instance_type (string, optional, default micro)", "name (string, optional)"},
		Attributes:    []string{"id", "private_ip"},
		CloudForgeAPI: "POST /api/v1/labs/{id}/resources (kind instance) — host agent CreateVM + StartVM (Phase 2)",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_instance\" \"web\" {\n  subnet_id     = cloudforge_subnet.public.id\n  instance_type = \"micro\"\n}",
	},
	{
		Type: "cloudforge_s3_bucket", Description: "An object-storage bucket backed by local MinIO (full mode).",
		Arguments:     []string{"bucket (string, required)", "versioning (bool, optional)"},
		Attributes:    []string{"id", "arn"},
		CloudForgeAPI: "POST /api/v1/aws/s3/buckets (MinIO) — Phase 2 S3 console; honest 503 when MinIO is down (lite mode)",
		Fidelity:      "PARTIAL",
		Example:       "resource \"cloudforge_s3_bucket\" \"assets\" {\n  bucket = \"my-assets\"\n}",
	},
	{
		Type: "cloudforge_iam_role", Description: "An IAM teaching role with an assume-role policy.",
		Arguments:     []string{"name (string, required)", "assume_role_policy (string, required)"},
		Attributes:    []string{"id", "arn"},
		CloudForgeAPI: "POST /api/v1/aws/iam/roles — Phase 2 IAM CRUD (local ARNs arn:cloudforge:iam::local:role/<name>)",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_iam_role\" \"app\" {\n  name = \"app\"\n}",
	},
	{
		Type: "cloudforge_sqs_queue", Description: "A durable message queue (Postgres-persisted).",
		Arguments:     []string{"name (string, required)", "visibility_timeout_seconds (number, optional)"},
		Attributes:    []string{"id", "url"},
		CloudForgeAPI: "POST /api/v1/aws/sqs/queues — Phase 3 durable SQS (visibility timeout, DLQ, retention)",
		Fidelity:      "API_EMULATED",
		Example:       "resource \"cloudforge_sqs_queue\" \"jobs\" {\n  name = \"jobs\"\n}",
	},
	{
		Type: "cloudforge_dynamodb_table", Description: "A DynamoDB table (DynamoDB Local when up, else the Postgres subset).",
		Arguments:     []string{"name (string, required)", "hash_key (string, required)", "billing_mode (string, optional)"},
		Attributes:    []string{"id", "arn"},
		CloudForgeAPI: "POST /api/v1/aws/dynamodb/tables — Phase 3 DynamoDB",
		Fidelity:      "PARTIAL",
		Example:       "resource \"cloudforge_dynamodb_table\" \"sessions\" {\n  name     = \"sessions\"\n  hash_key = \"id\"\n}",
	},
	{
		Type: "cloudforge_lambda_function", Description: "A serverless function run as a sandboxed subprocess.",
		Arguments:     []string{"function_name (string, required)", "runtime (string, required: python3.9 | nodejs20.x)", "handler (string, required)"},
		Attributes:    []string{"id", "arn"},
		CloudForgeAPI: "POST /api/v1/aws/lambda/functions — Phase 3 Lambda (subprocess, timeout + output cap, not a container)",
		Fidelity:      "PARTIAL",
		Example:       "resource \"cloudforge_lambda_function\" \"hello\" {\n  function_name = \"hello\"\n  runtime       = \"python3.9\"\n  handler       = \"index.handler\"\n}",
	},
}

// ProviderInstallDocs documents installation and versioning of the
// CloudForge provider. It is documentation, not an install that runs
// anything: the provider is a learning surface served by this API.
const ProviderInstallDocs = `CloudForge Terraform provider — installation and versioning

The CloudForge provider is a LEARNING provider. It does not talk to
real AWS and it never needs AWS credentials.

Installation (learning mode — local provider, no registry download):
  1. Create a Terraform Learning project in CloudForge.
  2. Write configuration using the cloudforge_* resource types listed
     in GET /api/v1/terraform-learning/provider/resources. Each type
     names the real CloudForge API it maps to.
  3. Validate and format in the editor (local checks, no Docker).
  4. Preview the plan (local static diff, labelled as such).
  5. Link the project to a lab and run init / plan / apply through
     the container runner (hashicorp/terraform:1.9.8) — aws_* resources
     execute against the local emulator; cloudforge_* resources are
     validated locally and applied through the mapped CloudForge APIs
     by the learning workflow, never against real AWS.

Versioning:
  - Provider version is the CloudForge platform version
    (GET /api/v1/health -> version). There is no separate provider
    release train: the provider surface changes only when the mapped
    CloudForge API changes, and the changelog is MILESTONE_LOG.md.
  - Pinning: required_providers entries for "cloudforge" are
    documentation-only in learning mode; the container runner uses
    the AWS provider (~> 5.0) against the emulator, exactly as in
    Milestone 7.
  - AWS-provider learning mode (aws_* resources) works only when a
    local emulator is configured (CLOUDFORGE_EMULATOR_ENDPOINT,
    default http://localhost:4566). Real AWS credentials are refused
    by ValidateProviderConfig — see internal/terraform.

What is NOT supported (honest limits):
  - No Terraform Cloud / HCP Terraform remote backend or workspaces.
  - No module registry (modules are local paths under modules/).
  - No real AWS provider execution: any configuration referencing
    amazonaws.com or embedding AWS keys is rejected (400).
  - Container execution needs Docker; without it every execution
    honestly returns 503 terraform_unavailable.
`
