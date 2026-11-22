// Phase 1 — service registry + resource explorer endpoints.
//
// GET /api/v1/services lists every CloudForge service from ONE backend
// source of truth. The metadata table below (names, categories, routes,
// fidelity, learning tracks) is console metadata only; the OPERATIONS
// and their fidelity come from the existing providers.Registry — this
// endpoint reuses that registry and never duplicates it. A service
// whose operations have no HTTP route is honest about it: the operation
// row carries http_route "" and the frontend renders it disabled.
//
// GET /api/v1/explorer/resources lists REAL backend resources only
// (labs + lab resources + courses + users where permitted) for the
// Resource Explorer. No fake resources are ever synthesised.
package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cloudforge/cloudforge/backend/internal/providers"
)

// --- Service registry ------------------------------------------------------

// ServiceOperation is one AWS operation of a service, derived from the
// providers registry. HTTPRoute is the CloudForge HTTP route that serves
// it ("" when no route exists — the operation is registry-only).
type ServiceOperation struct {
	Operation   string   `json:"operation"`
	Fidelity    string   `json:"fidelity"`
	Supported   bool     `json:"supported"`
	HTTPRoute   string   `json:"http_route,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}

// ServiceInfo is one entry of GET /api/v1/services.
type ServiceInfo struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Category      string             `json:"category"`
	Description   string             `json:"description"`
	Icon          string             `json:"icon"`
	Route         string             `json:"route"`
	Availability  string             `json:"availability"` // available | partial | registry-only | unavailable
	Fidelity      string             `json:"fidelity"`
	Operations    []ServiceOperation `json:"operations"`
	ResourceTypes []string           `json:"resource_types"`
	DocsURL       string             `json:"docs_url"`
	Learning      []string           `json:"learning_exercises"`
	// Mirrors names the real AWS console screens this service's console
	// mirrors ("" when it has no dedicated console yet).
	Mirrors string `json:"mirrors,omitempty"`
	Differs string `json:"differs,omitempty"`
}

// httpRouteFor maps a (service, operation) to the CloudForge HTTP route
// that actually serves it today. Empty means no route exists.
func httpRouteFor(service, operation string) string {
	switch service + "/" + operation {
	case "s3/CreateBucket":
		return "POST /api/v1/aws/s3/buckets"
	case "s3/ListBuckets":
		return "GET /api/v1/aws/s3/buckets"
	case "s3/DeleteBucket":
		return "DELETE /api/v1/aws/s3/buckets/{bucket}"
	case "s3/ListObjects":
		return "GET /api/v1/aws/s3/buckets/{bucket}/objects"
	case "s3/PutObject":
		return "POST /api/v1/aws/s3/buckets/{bucket}/objects"
	case "s3/GetObject":
		return "GET /api/v1/aws/s3/buckets/{bucket}/objects/{key}"
	case "s3/DeleteObject":
		return "DELETE /api/v1/aws/s3/buckets/{bucket}/objects/{key}"
	case "s3/GetBucketVersioning":
		return "GET /api/v1/aws/s3/buckets/{bucket}/versioning"
	case "s3/SetBucketVersioning":
		return "PUT /api/v1/aws/s3/buckets/{bucket}/versioning"
	case "s3/GetBucketPolicy":
		return "GET /api/v1/aws/s3/buckets/{bucket}/policy"
	case "s3/SetBucketPolicy":
		return "PUT /api/v1/aws/s3/buckets/{bucket}/policy"
	case "iam/EvaluatePolicy":
		return "POST /api/v1/aws/iam/evaluate"
	case "iam/CreateUser":
		return "POST /api/v1/aws/iam/users"
	case "iam/ListUsers":
		return "GET /api/v1/aws/iam/users"
	case "iam/DeleteUser":
		return "DELETE /api/v1/aws/iam/users/{id}"
	case "iam/CreateGroup":
		return "POST /api/v1/aws/iam/groups"
	case "iam/CreateRole":
		return "POST /api/v1/aws/iam/roles"
	case "iam/CreatePolicy":
		return "POST /api/v1/aws/iam/policies"
	case "iam/CreateAccessKey":
		return "POST /api/v1/aws/iam/users/{id}/access-keys"
	case "iam/AssumeRole":
		return "POST /api/v1/aws/iam/roles/{id}/assume"
	case "cloudwatch/GetMetrics":
		return "GET /api/v1/monitoring"
	case "cloudwatch/CreateAlarm":
		return "POST /api/v1/monitoring/alarms"
	case "cloudtrail/LookupEvents":
		return "GET /api/v1/audit"
	case "cloudtrail/ExportEvents":
		return "GET /api/v1/audit/export"
	// Phase 6: advanced services.
	case "athena/StartQueryExecution":
		return "POST /api/v1/aws/athena/queries/{id}/execute"
	case "glue/StartCrawler":
		return "POST /api/v1/aws/glue/crawlers/{id}/run"
	case "kinesis/PutRecord":
		return "POST /api/v1/aws/kinesis/streams/{id}/records"
	case "kinesis/GetRecords":
		return "GET /api/v1/aws/kinesis/streams/{id}/records"
	case "firehose/PutRecord":
		return "POST /api/v1/aws/firehose/streams/{id}/records"
	case "quicksight/CreateDashboard":
		return "POST /api/v1/aws/quicksight/dashboards"
	case "sagemaker/CreateTrainingJob":
		return "POST /api/v1/aws/sagemaker/training-jobs"
	case "sagemaker/InvokeEndpoint":
		return "POST /api/v1/aws/sagemaker/endpoints/{id}/invoke"
	case "bedrock/InvokeModel":
		return "POST /api/v1/aws/bedrock/invoke"
	case "dms/CreateReplicationTask":
		return "POST /api/v1/aws/dms/tasks"
	case "datasync/CreateTask":
		return "POST /api/v1/aws/datasync/tasks"
	case "guardduty/ListFindings":
		return "GET /api/v1/aws/guardduty/findings"
	case "securityhub/GetFindings":
		return "GET /api/v1/aws/securityhub/findings"
	case "inspector/StartAssessmentRun":
		return "POST /api/v1/aws/inspector/assessments"
	case "config/PutConfigRule":
		return "POST /api/v1/aws/config/rules"
	case "waf/CreateWebACL":
		return "POST /api/v1/aws/waf/acls"
	case "kms/CreateKey":
		return "POST /api/v1/aws/kms/keys"
	case "kms/Encrypt":
		return "POST /api/v1/aws/kms/keys/{id}/encrypt"
	case "kms/Decrypt":
		return "POST /api/v1/aws/kms/keys/{id}/decrypt"
	case "secretsmanager/CreateSecret":
		return "POST /api/v1/aws/secretsmanager/secrets"
	case "vpc/CreateVpcPeeringConnection":
		return "POST /api/v1/aws/vpc/peerings"
	case "vpc/CreateVpcEndpoint":
		return "POST /api/v1/aws/vpc/endpoints"
	case "vpc/CreateFlowLogs":
		return "POST /api/v1/aws/vpc/flow-logs"
	case "transitgateway/CreateTransitGateway":
		return "POST /api/v1/aws/transit-gateways"
	case "networkfirewall/CreateFirewall":
		return "POST /api/v1/aws/network-firewall/firewalls"
	case "ssm/SendCommand":
		return "POST /api/v1/aws/ssm/commands"
	case "trustedadvisor/DescribeChecks":
		return "POST /api/v1/aws/trustedadvisor/run"
	case "costexplorer/GetCostAndUsage":
		return "GET /api/v1/aws/costexplorer/costs"
	case "budgets/CreateBudget":
		return "POST /api/v1/aws/costexplorer/budgets"
	case "organizations/CreateOrganizationalUnit":
		return "POST /api/v1/aws/organizations/units"
	// Phase 3: data, messaging, serverless.
	case "dynamodb/CreateTable":
		return "POST /api/v1/aws/dynamodb/tables"
	case "dynamodb/ListTables":
		return "GET /api/v1/aws/dynamodb/tables"
	case "dynamodb/DeleteTable":
		return "DELETE /api/v1/aws/dynamodb/tables/{table}"
	case "dynamodb/PutItem":
		return "POST /api/v1/aws/dynamodb/tables/{table}/items"
	case "dynamodb/GetItem":
		return "GET /api/v1/aws/dynamodb/tables/{table}/items/{key}"
	case "dynamodb/Query":
		return "GET /api/v1/aws/dynamodb/tables/{table}/query"
	case "dynamodb/Scan":
		return "GET /api/v1/aws/dynamodb/tables/{table}/items"
	case "dynamodb/DeleteItem":
		return "DELETE /api/v1/aws/dynamodb/tables/{table}/items/{key}"
	case "rds/CreateDBInstance":
		return "POST /api/v1/aws/rds/instances"
	case "rds/DeleteDBInstance":
		return "DELETE /api/v1/aws/rds/instances/{id}"
	case "rds/CreateDBSnapshot":
		return "POST /api/v1/aws/rds/instances/{id}/snapshots"
	case "rds/RestoreDBInstanceFromDBSnapshot":
		return "POST /api/v1/aws/rds/snapshots/{id}/restore"
	case "elasticache/CreateCacheCluster":
		return "POST /api/v1/aws/elasticache/clusters"
	case "elasticache/DeleteCacheCluster":
		return "DELETE /api/v1/aws/elasticache/clusters/{id}"
	case "elb/CreateLoadBalancer":
		return "POST /api/v1/aws/elb/load-balancers"
	case "elb/CreateListener":
		return "POST /api/v1/aws/elb/load-balancers/{id}/listeners"
	case "elb/CreateTargetGroup":
		return "POST /api/v1/aws/elb/target-groups"
	case "elb/RegisterTargets":
		return "POST /api/v1/aws/elb/target-groups/{id}/targets"
	case "route53/CreateHostedZone":
		return "POST /api/v1/aws/route53/zones"
	case "route53/ChangeResourceRecordSets":
		return "POST /api/v1/aws/route53/zones/{id}/records"
	case "sqs/CreateQueue":
		return "POST /api/v1/aws/sqs/queues"
	case "sqs/SendMessage":
		return "POST /api/v1/aws/sqs/queues/{id}/messages"
	case "sqs/ReceiveMessage":
		return "POST /api/v1/aws/sqs/queues/{id}/receive"
	case "sqs/DeleteMessage":
		return "DELETE /api/v1/aws/sqs/queues/{id}/messages/{receipt}"
	case "sns/CreateTopic":
		return "POST /api/v1/aws/sns/topics"
	case "sns/Publish":
		return "POST /api/v1/aws/sns/topics/{id}/publish"
	case "sns/Subscribe":
		return "POST /api/v1/aws/sns/topics/{id}/subscribe"
	case "eventbridge/PutRule":
		return "POST /api/v1/aws/events/rules"
	case "eventbridge/PutEvents":
		return "POST /api/v1/aws/events/put-events"
	case "lambda/CreateFunction":
		return "POST /api/v1/aws/lambda/functions"
	case "lambda/InvokeFunction":
		return "POST /api/v1/aws/lambda/functions/{id}/invoke"
	case "apigateway/CreateRestApi":
		return "POST /api/v1/aws/apigateway/apis"
	case "apigateway/CreateResource":
		return "POST /api/v1/aws/apigateway/apis/{id}/resources"
	case "apigateway/CreateDeployment":
		return "POST /api/v1/aws/apigateway/apis/{id}/deploy"
	// Phase 4: containers and developer tools.
	case "ecr/CreateRepository":
		return "POST /api/v1/aws/ecr/repositories"
	case "ecr/DescribeImages":
		return "GET /api/v1/aws/ecr/repositories/{id}/images"
	case "ecr/PutImage":
		return "POST /api/v1/aws/ecr/repositories/{id}/images"
	case "ecs/CreateCluster":
		return "POST /api/v1/aws/ecs/clusters"
	case "ecs/RegisterTaskDefinition":
		return "POST /api/v1/aws/ecs/task-definitions"
	case "ecs/CreateService":
		return "POST /api/v1/aws/ecs/services"
	case "ecs/RunTask":
		return "POST /api/v1/aws/ecs/tasks/run"
	case "fargate/RunTask":
		return "POST /api/v1/aws/ecs/tasks/run"
	case "codebuild/CreateProject":
		return "POST /api/v1/aws/codebuild/projects"
	case "codebuild/StartBuild":
		return "POST /api/v1/aws/codebuild/projects/{id}/builds"
	case "codepipeline/CreatePipeline":
		return "POST /api/v1/aws/codepipeline/pipelines"
	case "codepipeline/StartPipelineExecution":
		return "POST /api/v1/aws/codepipeline/pipelines/{id}/executions"
	case "codedeploy/CreateApplication":
		return "POST /api/v1/aws/codedeploy/applications"
	case "codedeploy/CreateDeployment":
		return "POST /api/v1/aws/codedeploy/applications/{id}/deployments"
	case "cloudformation/CreateStack":
		return "POST /api/v1/aws/cloudformation/stacks"
	case "cloudformation/CreateChangeSet":
		return "POST /api/v1/aws/cloudformation/stacks/{id}/changesets"
	// v3 exam services (SAA-C03 / SAP-C02) in lite mode.
	case "autoscaling/CreateAutoScalingGroup":
		return "POST /api/v1/aws/autoscaling/groups"
	case "autoscaling/DescribeAutoScalingGroups":
		return "GET /api/v1/aws/autoscaling/groups"
	case "autoscaling/UpdateAutoScalingGroup":
		return "POST /api/v1/aws/autoscaling/groups/{id}/scale"
	case "autoscaling/DeleteAutoScalingGroup":
		return "DELETE /api/v1/aws/autoscaling/groups/{id}"
	case "globalaccelerator/CreateAccelerator":
		return "POST /api/v1/aws/globalaccelerator/accelerators"
	case "globalaccelerator/DescribeAccelerator":
		return "GET /api/v1/aws/globalaccelerator/accelerators"
	case "globalaccelerator/UpdateAccelerator":
		return "POST /api/v1/aws/globalaccelerator/accelerators/{id}/failover"
	case "globalaccelerator/DeleteAccelerator":
		return "DELETE /api/v1/aws/globalaccelerator/accelerators/{id}"
	case "directconnect/CreateConnection":
		return "POST /api/v1/aws/directconnect/connections"
	case "directconnect/DescribeConnections":
		return "GET /api/v1/aws/directconnect/connections"
	case "directconnect/ConfirmConnection":
		return "POST /api/v1/aws/directconnect/connections/{id}/confirm"
	case "directconnect/DeleteConnection":
		return "DELETE /api/v1/aws/directconnect/connections/{id}"
	case "controltower/CreateLandingZone":
		return "POST /api/v1/aws/controltower/landing-zones"
	case "controltower/DescribeLandingZone":
		return "GET /api/v1/aws/controltower/landing-zones"
	case "controltower/EnableGuardrail":
		return "POST /api/v1/aws/controltower/landing-zones/{id}/guardrails"
	case "controltower/DeleteLandingZone":
		return "DELETE /api/v1/aws/controltower/landing-zones/{id}"
	case "identitycenter/CreatePermissionSet":
		return "POST /api/v1/aws/identitycenter/permission-sets"
	case "identitycenter/DescribePermissionSet":
		return "GET /api/v1/aws/identitycenter/permission-sets"
	case "identitycenter/CreateAccountAssignment":
		return "POST /api/v1/aws/identitycenter/permission-sets/{id}/assignments"
	case "identitycenter/DeletePermissionSet":
		return "DELETE /api/v1/aws/identitycenter/permission-sets/{id}"
	case "ram/CreateResourceShare":
		return "POST /api/v1/aws/ram/resource-shares"
	case "ram/DescribeResourceShares":
		return "GET /api/v1/aws/ram/resource-shares"
	case "ram/AssociateResourceShare":
		return "POST /api/v1/aws/ram/resource-shares/{id}/principals"
	case "ram/DeleteResourceShare":
		return "DELETE /api/v1/aws/ram/resource-shares/{id}"
	case "storagegateway/CreateGateway":
		return "POST /api/v1/aws/storagegateway/gateways"
	case "storagegateway/DescribeGateway":
		return "GET /api/v1/aws/storagegateway/gateways"
	case "storagegateway/ActivateGateway":
		return "POST /api/v1/aws/storagegateway/gateways/{id}/activate"
	case "storagegateway/DeleteGateway":
		return "DELETE /api/v1/aws/storagegateway/gateways/{id}"
	case "snowball/CreateJob":
		return "POST /api/v1/aws/snowball/jobs"
	case "snowball/DescribeJob":
		return "GET /api/v1/aws/snowball/jobs"
	case "snowball/UpdateJob":
		return "POST /api/v1/aws/snowball/jobs/{id}/advance"
	case "snowball/DeleteJob":
		return "DELETE /api/v1/aws/snowball/jobs/{id}"
	case "lakeformation/GrantPermissions":
		return "POST /api/v1/aws/lakeformation/permissions"
	case "lakeformation/ListPermissions":
		return "GET /api/v1/aws/lakeformation/permissions"
	case "lakeformation/RevokePermissions":
		return "DELETE /api/v1/aws/lakeformation/permissions/{id}"
	case "macie/CreateClassificationJob":
		return "POST /api/v1/aws/macie/jobs"
	case "macie/DescribeClassificationJob":
		return "GET /api/v1/aws/macie/jobs"
	case "macie/ListFindings":
		return "GET /api/v1/aws/macie/findings"
	case "macie/DeleteClassificationJob":
		return "DELETE /api/v1/aws/macie/jobs/{id}"
	case "computeoptimizer/GetEC2InstanceRecommendations":
		return "POST /api/v1/aws/computeoptimizer/recommendations/refresh"
	case "computeoptimizer/DeleteRecommendation":
		return "DELETE /api/v1/aws/computeoptimizer/recommendations/{id}"
	case "savingsplans/CreateSavingsPlan":
		return "POST /api/v1/aws/savingsplans/plans"
	case "savingsplans/DescribeSavingsPlans":
		return "GET /api/v1/aws/savingsplans/plans"
	case "savingsplans/GetSavingsPlanUtilization":
		return "GET /api/v1/aws/savingsplans/plans/{id}/utilization"
	case "savingsplans/DeleteSavingsPlan":
		return "DELETE /api/v1/aws/savingsplans/plans/{id}"
	case "auditmanager/CreateAssessment":
		return "POST /api/v1/aws/auditmanager/assessments"
	case "auditmanager/DescribeAssessment":
		return "GET /api/v1/aws/auditmanager/assessments"
	case "auditmanager/CollectEvidence":
		return "POST /api/v1/aws/auditmanager/assessments/{id}/collect-evidence"
	case "auditmanager/DeleteAssessment":
		return "DELETE /api/v1/aws/auditmanager/assessments/{id}"
	case "artifact/ListReports":
		return "GET /api/v1/aws/artifact/reports"
	case "artifact/CreateAgreement":
		return "POST /api/v1/aws/artifact/agreements"
	case "artifact/DescribeAgreements":
		return "GET /api/v1/aws/artifact/agreements"
	case "artifact/DeleteAgreement":
		return "DELETE /api/v1/aws/artifact/agreements/{id}"
	case "aurora/CreateDBCluster":
		return "POST /api/v1/aws/aurora/clusters"
	case "aurora/DescribeDBClusters":
		return "GET /api/v1/aws/aurora/clusters"
	case "aurora/FailoverDBCluster":
		return "POST /api/v1/aws/aurora/clusters/{id}/failover"
	case "aurora/DeleteDBCluster":
		return "DELETE /api/v1/aws/aurora/clusters/{id}"
	case "backup/CreateBackupPlan":
		return "POST /api/v1/aws/backup/plans"
	case "backup/CreateBackupVault":
		return "POST /api/v1/aws/backup/vaults"
	case "backup/StartBackupJob":
		return "POST /api/v1/aws/backup/plans/{id}/run"
	case "backup/DeleteBackupPlan":
		return "DELETE /api/v1/aws/backup/plans/{id}"
	case "mgn/CreateSourceServer":
		return "POST /api/v1/aws/mgn/source-servers"
	case "mgn/DescribeSourceServers":
		return "GET /api/v1/aws/mgn/source-servers"
	case "mgn/StartReplication":
		return "POST /api/v1/aws/mgn/source-servers/{id}/start-replication"
	case "mgn/DeleteSourceServer":
		return "DELETE /api/v1/aws/mgn/source-servers/{id}"
	case "fsx/CreateFileSystem":
		return "POST /api/v1/aws/fsx/file-systems"
	case "fsx/DescribeFileSystems":
		return "GET /api/v1/aws/fsx/file-systems"
	case "fsx/CreateMountTarget":
		return "POST /api/v1/aws/fsx/file-systems/{id}/mount-targets"
	case "fsx/DeleteFileSystem":
		return "DELETE /api/v1/aws/fsx/file-systems/{id}"
	case "shield/CreateProtection":
		return "POST /api/v1/aws/shield/protections"
	case "shield/DescribeProtections":
		return "GET /api/v1/aws/shield/protections"
	case "shield/DeleteProtection":
		return "DELETE /api/v1/aws/shield/protections/{id}"
	case "ebs/CreateVolume":
		return "POST /api/v1/aws/ebs/volumes"
	case "ebs/DescribeVolumes":
		return "GET /api/v1/aws/ebs/volumes"
	case "ebs/AttachVolume":
		return "POST /api/v1/aws/ebs/volumes/{id}/attach"
	case "ebs/DeleteVolume":
		return "DELETE /api/v1/aws/ebs/volumes/{id}"
	case "efs/CreateFileSystem":
		return "POST /api/v1/aws/efs/file-systems"
	case "efs/DescribeFileSystems":
		return "GET /api/v1/aws/efs/file-systems"
	case "efs/CreateMountTarget":
		return "POST /api/v1/aws/efs/file-systems/{id}/mount-targets"
	case "efs/DeleteFileSystem":
		return "DELETE /api/v1/aws/efs/file-systems/{id}"
	case "cloudfront/CreateDistribution":
		return "POST /api/v1/aws/cloudfront/distributions"
	case "cloudfront/DescribeDistributions":
		return "GET /api/v1/aws/cloudfront/distributions"
	case "cloudfront/CreateInvalidation":
		return "POST /api/v1/aws/cloudfront/distributions/{id}/invalidations"
	case "cloudfront/DeleteDistribution":
		return "DELETE /api/v1/aws/cloudfront/distributions/{id}"
	case "vpn/CreateCustomerGateway":
		return "POST /api/v1/aws/vpn/customer-gateways"
	case "vpn/CreateVpnGateway":
		return "POST /api/v1/aws/vpn/vpn-gateways"
	case "vpn/CreateVpnConnection":
		return "POST /api/v1/aws/vpn/connections"
	case "vpn/DeleteVpnConnection":
		return "DELETE /api/v1/aws/vpn/connections/{id}"
	case "nacl/CreateNetworkAcl":
		return "POST /api/v1/aws/nacl/acls"
	case "nacl/DescribeNetworkAcls":
		return "GET /api/v1/aws/nacl/acls"
	case "nacl/CreateNetworkAclEntry":
		return "POST /api/v1/aws/nacl/acls/{id}/rules"
	case "nacl/EvaluateNetworkAcl":
		return "POST /api/v1/aws/nacl/acls/{id}/evaluate"
	}
	return ""
}

// serviceMeta is console metadata for one service. Operations are NOT
// listed here — they are derived from the providers registry at serve
// time, so the registry stays the single source of truth for what an
// operation is and how faithful it is.
type serviceMeta struct {
	id, name, category, description, icon, route, availability, fidelity string
	resourceTypes                                                        []string
	learning                                                             []string
	mirrors, differs                                                     string
}

func svc(id, name, category, description, icon, route, availability, fidelity string, resourceTypes, learning []string, mirrors, differs string) serviceMeta {
	return serviceMeta{id: id, name: name, category: category, description: description, icon: icon, route: route,
		availability: availability, fidelity: fidelity, resourceTypes: resourceTypes, learning: learning,
		mirrors: mirrors, differs: differs}
}

// serviceCatalog is the console metadata table. Adding a service means
// adding one row here (and, when it gains real operations, registry
// entries in internal/providers) — the frontend renders from
// GET /api/v1/services and needs no code change.
var serviceCatalog = []serviceMeta{
	// Compute
	svc("ec2", "EC2", "Compute", "Lab sessions as instances — launch, view state, open the workstation.", "server", "/services/ec2", "available", "PARTIAL",
		[]string{"instance", "lab"}, []string{"ec2"},
		"Instances table (Instance ID / Name / State / Type / IPs / VPC / Subnet / AZ / Launch time); detail tabs Details / Security / Networking / Storage / Monitoring / Status Checks / Logs / Tags / Events; Launch instance wizard; Start/Stop/Reboot/Terminate actions",
		"Local teaching profiles Micro/Small/Medium, not AWS instance types; AZ is a local label (cf-local-1); no real AWS hardware, AMIs, or billing; instances are lab sessions; Start re-provisions a stopped lab (READY only on success); Reboot is a real host-agent op and is unavailable (503) when no host agent/VM exists."),
	svc("lambda", "Lambda", "Compute", "Serverless functions — real subprocess execution (python3/node).", "zap", "/services/lambda", "available", "PARTIAL",
		[]string{"function"}, []string{"serverless"},
		"Functions list; detail tabs Code / Configuration / Triggers / Permissions / Monitoring; Create-function wizard; Test/Invoke action",
		"Real execution in a sandboxed subprocess (timeout, output cap, prlimit memory cap when available, uid 65534 when the API runs as root) — NOT container isolation and NOT AWS Lambda; no Docker invoker (the old registry claim is corrected in Phase 3); no VPC attachment, managed scaling, layers, or provisioned concurrency; triggers from SQS queues and EventBridge rules are real."),
	svc("ecs", "ECS", "Compute", "Container clusters, task definitions, services and tasks — sandboxed-subprocess runtime.", "layers", "/services/ecs", "available", "PARTIAL",
		[]string{"cluster", "task-definition", "service", "task"}, []string{"containers"},
		"Clusters / Task definitions / Services / Tasks screens; Run-task wizard; service desired-count and rolling deployments",
		"Clusters, task definitions, services and tasks are real records; the runtime is SIMULATED — a task's command runs as a sandboxed subprocess on the API host (timeout, output cap, prlimit memory cap, uid 65534 when root), no Docker container is started and no image is pulled. Desired count is reconciled on every read and when a task stops; task status comes from the real process; logs are real process output."),
	svc("eks", "EKS", "Compute", "Managed Kubernetes clusters — UNSUPPORTED by Phase 4 decision; Kubernetes concepts are taught on the ECS runtime.", "layers", "/services/eks", "unavailable", "UNSUPPORTED",
		[]string{"cluster", "node-group"}, nil,
		"Cluster list / Node groups / Add-ons screens are NOT built — the console is a Kubernetes-concepts learning path with working ECS exercises",
		"Phase 4 decision: EKS stays UNSUPPORTED. No local k3s/kind cluster is built — it cannot be verified in this sandbox (no Docker/KVM), and a managed control plane would break lite mode. The console maps Pod/Deployment/Service/ConfigMap/Namespace/Job onto the ECS/ELB/CodeBuild runtimes that do exist, with working try-it actions."),
	svc("fargate", "Fargate", "Compute", "Serverless container runs — same sandboxed runtime as ECS, no cluster to manage.", "layers", "/services/ecs", "available", "SIMULATED",
		[]string{"task"}, []string{"containers"},
		"Serverless task launch (no server management) — served by the ECS console's Fargate section",
		"SIMULATED: Fargate-style runs use the ECS sandboxed-subprocess runtime with launch type FARGATE and no cluster. No Docker container, no AWS Fargate capacity; the image is recorded, not pulled."),
	svc("beanstalk", "Elastic Beanstalk", "Compute", "Application environments.", "layers", "/services/beanstalk", "unavailable", "UNSUPPORTED",
		[]string{"application", "environment"}, nil, "", "Not started."),
	svc("batch", "Batch", "Compute", "Batch job queues, definitions and jobs.", "layers", "/services/batch", "unavailable", "UNSUPPORTED",
		[]string{"job-queue", "job-definition", "job"}, nil, "", "Not started."),
	// Storage
	svc("s3", "S3", "Storage", "Object storage buckets, backed by local MinIO.", "database", "/services/s3", "available", "PARTIAL",
		[]string{"bucket", "object"}, []string{"storage"},
		"Buckets list; bucket detail tabs Objects / Properties / Permissions / Management / Metrics; Create-bucket wizard",
		"Buckets are local to this deployment (no global S3 namespace); backed by local MinIO, not AWS S3; objects upload/download/delete through the backend (10 MiB cap); versioning toggle and bucket policy are real MinIO operations; object ACLs are not exposed by the MinIO adapter and are shown as unavailable."),
	svc("ebs", "EBS", "Storage", "Block storage volumes — durable records with attach/detach against real labs.", "hard-drive", "/services/ebs", "available", "API_EMULATED",
		[]string{"volume"}, []string{"aws-saa"},
		"Volumes list; Create-volume wizard; Attach/Detach actions",
		"Volumes are durable records, not real block devices: nothing is provisioned, formatted or mounted. Attach validates that the lab exists and records the attachment; the lab's disk is unchanged. In-use volumes cannot be deleted."),
	svc("efs", "EFS", "Storage", "Elastic file systems and mount targets — durable records.", "hard-drive", "/services/efs", "available", "API_EMULATED",
		[]string{"file-system"}, []string{"aws-saa"},
		"File systems list; Create-file-system wizard; Mount targets",
		"No NFS endpoint exists and nothing can be mounted; mount targets are recorded against teaching subnet names."),
	svc("fsx", "FSx", "Storage", "Managed file systems (Lustre, Windows, ONTAP, OpenZFS) — durable records.", "hard-drive", "/services/fsx", "available", "API_EMULATED",
		[]string{"file-system"}, []string{"aws-saa", "aws-sap"},
		"File systems list; Create-file-system wizard; Mount targets",
		"No Lustre/Windows/ONTAP volume exists and nothing can be mounted; mount targets are recorded against teaching subnet names."),
	svc("glacier", "S3 Glacier", "Storage", "Archive storage and retrieval.", "hard-drive", "/services/glacier", "unavailable", "UNSUPPORTED",
		[]string{"archive"}, []string{"storage"}, "", "Not started."),
	// Networking
	svc("vpc", "VPC", "Networking", "Virtual private clouds, subnets, route tables, gateways and security groups — as lab resources on the Rust network engine.", "network", "/services/vpc", "available", "PARTIAL",
		[]string{"vpc", "subnet", "route-table", "gateway", "security-group"}, []string{"networking", "fundamentals"},
		"Your VPCs / Subnets / Route Tables / Internet Gateways / NAT Gateways / Security Groups / Network ACLs left nav; VPC detail tabs; resource tables with the same columns as the AWS console",
		"No AWS availability zones (subnets map to local namespaces); CIDRs are local (10.x); no real AWS VPC, no VPC Flow Logs; VPCs/subnets/route tables/gateways/security groups are created as lab resources with a create wizard (CIDR validated, subnets must sit inside the VPC and not overlap); routes and NAT go through the Rust engine; NACLs are UNSUPPORTED — the engine has no NACL RPC."),
	svc("subnet", "Subnets", "Networking", "Subnets inside a lab VPC.", "layers", "/services/vpc", "available", "SIMULATED",
		[]string{"subnet"}, []string{"networking"},
		"Subnets table (Subnet ID / VPC / CIDR / AZ / Available IPs); Create-subnet flow via lab resources",
		"AZ is a local label; subnets live inside labs."),
	svc("routetable", "Route Tables", "Networking", "Route tables and routes inside a lab network.", "route", "/services/vpc", "available", "SIMULATED",
		[]string{"route-table"}, []string{"networking"},
		"Route Tables list; Routes tab (Destination / Target / Status); Edit-routes flow",
		"Targets are local gateways/NAT (simulated)."),
	svc("igw", "Internet Gateway", "Networking", "Internet gateways attached to lab VPCs.", "network", "/services/vpc", "available", "SIMULATED",
		[]string{"gateway"}, []string{"networking"},
		"Internet Gateways list; Attach/Detach actions",
		"Simulated gateway, not an AWS IGW."),
	svc("nat", "NAT Gateway", "Networking", "NAT gateways for private subnets.", "network", "/services/vpc", "available", "SIMULATED",
		[]string{"gateway"}, []string{"networking"},
		"NAT Gateways list (ID / Subnet / Connectivity / State)",
		"Simulated NAT; no Elastic IP allocation."),
	svc("sg", "Security Groups", "Networking", "Firewall rules for lab resources.", "shield", "/services/vpc", "available", "SIMULATED",
		[]string{"security-group"}, []string{"networking"},
		"Security Groups list; Inbound/Outbound rules tabs; Edit-rules flow",
		"Pure rule evaluation; default-deny; no AWS SG referencing."),
	svc("nacl", "Network ACLs", "Networking", "Stateless network ACLs — durable records with real rule evaluation.", "shield", "/services/nacl", "available", "API_EMULATED",
		[]string{"network-acl"}, []string{"aws-saa", "aws-sap"},
		"Network ACLs list; Numbered rules; Evaluate-packet flow",
		"Rules are really evaluated (lowest number first match, stateless, default deny) by the evaluate endpoint — but the ACL is not attached to any subnet and filters no live traffic."),
	svc("elb", "Elastic Load Balancing", "Networking", "Load balancers, listeners and target groups — in-process round-robin with real health probes.", "scale", "/services/elb", "available", "SIMULATED",
		[]string{"load-balancer", "target-group"}, []string{"networking"},
		"Load Balancers list; Listeners / Target Groups tabs; health-check action; round-robin route preview",
		"SIMULATED: no engine RPC and no nginx reconfiguration; the DNS name is a local teaching name; only targets whose last probe said healthy are eligible; the round-robin counter is process-local."),
	svc("route53", "Route 53", "Networking", "Hosted zones and DNS records — durable, resolvable via the resolve endpoint.", "network", "/services/route53", "available", "SIMULATED",
		[]string{"hosted-zone", "record"}, []string{"networking"},
		"Hosted Zones / Records; Create-record wizard; Resolve action",
		"SIMULATED: no DNS server serves these records to the lab network; A, CNAME and TXT only; resolution is against the stored records via the resolve endpoint."),
	svc("cloudfront", "CloudFront", "Networking", "Content delivery distributions — durable records with invalidations.", "network", "/services/cloudfront", "available", "API_EMULATED",
		[]string{"distribution"}, []string{"aws-saa"},
		"Distributions list; Create-distribution wizard; Invalidations",
		"No edge location caches anything and the domain name is a teaching name, not registered or served. Invalidations are recorded COMPLETED; nothing is actually invalidated at an edge."),
	svc("vpcpeering", "VPC Peering", "Networking", "Peering connections between lab VPCs — real route-table entries.", "network", "/services/vpcpeering", "available", "SIMULATED",
		[]string{"peering-connection"}, []string{"networking"},
		"Peering connections list; Create-peering wizard (CIDR overlap rejected); Delete removes routes",
		"SIMULATED: no engine peering RPC exists — a peering is real route-table entries in both VPCs' lab state, not kernel peering. CIDRs must not overlap."),
	svc("transitgateway", "Transit Gateway", "Networking", "Transit gateways and VPC attachments — real route-table entries.", "network", "/services/transitgateway", "available", "SIMULATED",
		[]string{"transit-gateway", "attachment"}, []string{"networking"},
		"Transit gateways list; Attach-VPC flow; Detach; Delete (blocked while attached)",
		"SIMULATED: no engine transit-gateway RPC exists — attachments are real route-table entries in the attached VPCs' lab state."),
	svc("vpcendpoints", "VPC Endpoints", "Networking", "PrivateLink-style endpoints to platform services.", "network", "/services/vpcendpoints", "available", "SIMULATED",
		[]string{"endpoint"}, []string{"networking"},
		"Endpoints list; Create-endpoint flow (s3, dynamodb, sqs, sns)",
		"Endpoint records routing lab traffic to platform services. The DNS name is a local teaching name, not registered."),
	svc("networkfirewall", "Network Firewall", "Networking", "Stateful firewall policies — real 5-tuple evaluation.", "shield", "/services/networkfirewall", "available", "SIMULATED",
		[]string{"firewall", "rule"}, []string{"networking"},
		"Firewalls list; Rules; Evaluate-packet flow",
		"Rules are really evaluated (5-tuple, first match, default deny) and the policy is sent to the engine's firewall RPC when a lab is attached; engine sync failure is recorded, never claimed."),
	svc("flowlogs", "VPC Flow Logs", "Networking", "Flow logs built only from real engine telemetry.", "activity", "/services/flowlogs", "available", "SIMULATED",
		[]string{"flow-log"}, []string{"networking"},
		"Flow log groups; Capture action; Records list",
		"Records are built ONLY from real engine telemetry snapshots taken by /capture — no per-packet flows are synthesised."),
	svc("vpn", "VPN", "Networking", "Site-to-site VPN: customer gateways, VPN gateways and connections — durable records.", "network", "/services/vpn", "available", "API_EMULATED",
		[]string{"vpn-connection"}, []string{"aws-sap"},
		"Customer gateways / VPN gateways / Connections; Tunnel activate/deactivate",
		"Tunnel state is a teaching field: activating a tunnel records it UP, but no IPsec tunnel exists and no traffic flows. Direct Connect is a separate service entry."),
	// Databases
	svc("dynamodb", "DynamoDB", "Databases", "Key-value / NoSQL tables — DynamoDB Local when it answers, else a Postgres-backed subset.", "table", "/services/dynamodb", "available", "PARTIAL",
		[]string{"table", "item"}, []string{"databases"},
		"Tables list; table detail tabs Overview / Items / Indexes / Monitoring; Create-table wizard; Explore-items (Put/Get/Query/Scan/Delete)",
		"API_EMULATED via DynamoDB Local when reachable (real DynamoDB JSON API: CreateTable/PutItem/GetItem/Query/Scan/DeleteItem/DeleteTable); otherwise the Postgres-backed subset (PARTIAL, hash key only, no sort key/secondary indexes, Scan capped at 100). Never full DynamoDB; no provisioned throughput billing, no global tables."),
	svc("rds", "RDS", "Databases", "Managed relational databases — real Postgres databases + roles on the platform Postgres.", "database", "/services/rds", "available", "PARTIAL",
		[]string{"database", "snapshot"}, []string{"databases"},
		"Databases list (DB identifier / Engine / Status / Endpoint); detail tabs Connectivity & security / Monitoring / Logs & events / Configuration / Maintenance & backups; Create-database wizard; Snapshots",
		"PostgreSQL-compatible, not Aurora: each instance is a real Postgres database + role on the platform Postgres (engine postgres 14/15/16 only; mysql rejected). Master password generated, shown once, hash at rest. Start/stop per instance is UNSUPPORTED (shared platform database). Snapshots are real pg_dump -Fc files; restore creates a new instance. No Multi-AZ; no containerised per-instance Postgres (not built)."),
	svc("aurora", "Aurora", "Databases", "Aurora clusters — durable records with writer/reader instances and failover.", "database", "/services/aurora", "available", "API_EMULATED",
		[]string{"cluster"}, []string{"aws-saa", "aws-sap"},
		"Clusters list; Cluster detail (instances, endpoints); Failover action",
		"No Aurora storage engine runs and no data is replicated; instances are teaching records and endpoints are teaching names. Failover swaps the recorded writer/reader roles. Use RDS for a real Postgres database."),
	svc("elasticache", "ElastiCache", "Databases", "In-memory caches — real namespaced Redis databases on the platform Redis (lite mode).", "database", "/services/elasticache", "available", "PARTIAL",
		[]string{"cluster"}, []string{"databases"},
		"Clusters list; cluster detail (node info, keys browser with TTL, flush, metrics)",
		"Lite mode only: a namespaced Redis database (key prefix + best-effort ACL user) on the platform Redis. A dedicated Redis container (full mode) is NOT built — no Docker here to verify it against; full-mode creates return an honest 503. Memory/hit-rate metrics are platform-Redis-wide; keys-in-cluster is prefix-only."),
	svc("redshift", "Redshift", "Databases", "Data warehouse clusters and query editor.", "database", "/services/redshift", "unavailable", "UNSUPPORTED",
		[]string{"cluster"}, nil, "", "Not started (Phase 5 decision)."),
	svc("neptune", "Neptune", "Databases", "Graph database clusters.", "database", "/services/neptune", "unavailable", "UNSUPPORTED",
		[]string{"cluster"}, nil, "", "Not started."),
	svc("documentdb", "DocumentDB", "Databases", "Document database clusters.", "database", "/services/documentdb", "unavailable", "UNSUPPORTED",
		[]string{"cluster"}, nil, "", "Not started."),
	// Security, Identity & Compliance
	svc("iam", "IAM", "Security and Identity", "Identity and access management — CloudForge roles plus a real IAM policy evaluator (subset).", "key", "/services/iam", "available", "PARTIAL",
		[]string{"user", "group", "role", "policy"}, []string{"iam"},
		"Users / Groups / Roles / Policies / Access Analyzer / Policy Simulator left nav; Create-user wizard; Attach-policy flow; Policy Simulator (evaluate)",
		"Subset only: Allow/Deny, wildcards, explicit-deny-wins, StringEquals on aws:username; no ARNs, principals, policy variables, or other condition operators; Users/Groups/Roles/Policies CRUD is real (admin-only, Postgres-backed) with policy documents validated + probe-evaluated before storage, access-key secrets shown once (hash at rest), short-lived AssumeRole sessions with enforced expiry, and a Policy Simulator with a full evaluation trail; CloudForge roles (learner/instructor/admin) are not AWS IAM; Access Analyzer is still not built."),
	svc("sts", "STS", "Security and Identity", "Temporary security credentials (educational simulator).", "key", "/services/sts", "registry-only", "SIMULATED",
		[]string{"credential"}, []string{"iam"}, "", "In-memory simulator only, no HTTP route; credentials are locally generated, grant no AWS access, and live only in memory."),
	svc("kms", "KMS", "Security and Identity", "Customer managed keys — durable local keys, envelope encryption.", "lock", "/services/kms", "available", "PARTIAL",
		[]string{"key"}, []string{"iam"},
		"Customer managed keys list; key detail (aliases, rotation); Encrypt/Decrypt actions",
		"Durable local keys (Phase 6): AES-256 key material stored wrapped under a master key derived from CLOUDFORGE_KMS_MASTER_KEY (or the JWT secret); real AES-256-GCM encrypt/decrypt with rotation that keeps previous versions decryptable. NOT real AWS KMS, no HSM. The old in-memory simulator is replaced."),
	svc("secretsmanager", "Secrets Manager", "Security and Identity", "Secrets storage — envelope-encrypted at rest, versioned.", "lock", "/services/secretsmanager", "available", "PARTIAL",
		[]string{"secret"}, []string{"iam"},
		"Secrets list; secret detail (versions); rotation screens",
		"Secrets are envelope-encrypted at rest (AES-256-GCM value, data key wrapped by a KMS key) and versioned; values are decrypted on read and never stored in plaintext. No automatic rotation schedule."),
	svc("cognito", "Cognito", "Security and Identity", "User pools and identity pools.", "key", "/services/cognito", "unavailable", "UNSUPPORTED",
		[]string{"user-pool", "identity-pool"}, nil, "", "Not started — local JWT auth is the CloudForge model, not Cognito."),
	svc("waf", "WAF", "Security and Identity", "Web application firewall rules — real rule evaluation.", "shield", "/services/waf", "available", "SIMULATED",
		[]string{"web-acl", "rule"}, []string{"iam"},
		"Web ACLs list; Rules; Evaluate-request flow",
		"SIMULATED: rules are really evaluated (first match, path/header/query/body/IP matchers) but only by the /evaluate endpoint against a request you supply — they are NOT inline in the API request path, so nothing is live-blocked."),
	svc("shield", "Shield", "Security and Identity", "DDoS protection records — which resources are covered.", "shield", "/services/shield", "available", "API_EMULATED",
		[]string{"protection"}, []string{"aws-saa"},
		"Protections list; Create-protection flow",
		"Protections are records only: no traffic is inspected, no attack is mitigated and no Shield Advanced subscription exists."),
	svc("guardduty", "GuardDuty", "Security and Identity", "Threat detection — findings from the real audit log.", "shield", "/services/guardduty", "available", "PARTIAL",
		[]string{"finding"}, []string{"iam"},
		"Findings list (severity / status); Detectors; Run-scan action",
		"Findings are generated only from real audit-log evidence (failed-login patterns: brute force, credential stuffing). No threat-intelligence feeds and no invented threats; a scan with no matching log evidence creates nothing."),
	svc("securityhub", "Security Hub", "Security and Identity", "Central findings aggregation across GuardDuty, Inspector, Config and Advisor.", "shield", "/services/securityhub", "available", "PARTIAL",
		[]string{"finding"}, []string{"iam"},
		"Findings summary (by severity / source / status); Findings list with status workflow",
		"Aggregation of the real findings in the shared findings table — nothing is counted that was not created by GuardDuty, Inspector, Config or Trusted Advisor. No AWS Security Hub integration."),
	svc("inspector", "Inspector", "Security and Identity", "Assessments against real resources.", "shield", "/services/inspector", "available", "PARTIAL",
		[]string{"assessment", "finding"}, []string{"iam"},
		"Assessment runs list; Assessment detail with findings",
		"Real checks against real resources: open security groups, public S3 bucket policies, unencrypted lab instances. No agent-based CVE scanning."),
	svc("acm", "Certificate Manager", "Security and Identity", "TLS certificates.", "lock", "/services/acm", "unavailable", "UNSUPPORTED",
		[]string{"certificate"}, nil, "", "Not started."),
	// Application Integration
	svc("sqs", "SQS", "Application Integration", "Message queues — durable, Postgres-persisted.", "layers", "/services/sqs", "available", "PARTIAL",
		[]string{"queue"}, []string{"messaging"},
		"Queues list; queue detail (Send/Receive, message inspection, DLQ view); Create-queue wizard; Purge; Redrive",
		"Durable Postgres queues: visibility timeout, retention, DLQ redrive after max receives are real. No FIFO queues, no long polling. The old in-memory simulator is replaced."),
	svc("sns", "SNS", "Application Integration", "Topics and subscriptions — durable, Postgres-persisted, with real fan-out.", "layers", "/services/sns", "available", "PARTIAL",
		[]string{"topic", "subscription"}, []string{"messaging"},
		"Topics list; Subscriptions; Publish flow; delivery records",
		"Durable Postgres topics; fan-out to SQS queues (durable enqueue), HTTP(S) endpoints (loopback only, SSRF-guarded) and Lambda functions, with every delivery recorded. No email/SMS delivery. The old in-memory simulator is replaced."),
	svc("eventbridge", "EventBridge", "Application Integration", "Event buses and rules — durable, Postgres-persisted, with pattern matching.", "layers", "/services/eventbridge", "available", "PARTIAL",
		[]string{"rule", "event-bus"}, []string{"messaging"},
		"Rules list; Event buses; Put-events flow; matched/delivery counters",
		"Durable Postgres rules; pattern = source / detail-type arrays + nested detail subset matching (not the full AWS event-pattern language); targets are SQS, SNS and Lambda (validated at rule creation). AWS service events are not generated — events arrive through PutEvents. The old in-memory simulator is replaced."),
	svc("ses", "SES", "Application Integration", "Email sending identities.", "layers", "/services/ses", "unavailable", "UNSUPPORTED",
		[]string{"identity"}, nil, "", "Not started."),
	svc("mq", "MQ", "Application Integration", "Managed message brokers.", "layers", "/services/mq", "unavailable", "UNSUPPORTED",
		[]string{"broker"}, nil, "", "Not started."),
	// Containers
	svc("ecr", "ECR", "Containers", "Container image repositories — real local registry (Postgres catalogue + content-addressed blobs).", "layers", "/services/ecr", "available", "PARTIAL",
		[]string{"repository", "image"}, []string{"containers"},
		"Repositories list; image push/pull flow; image detail with layers/size/digest; scan results; lifecycle rules",
		"Real local registry speaking the Docker Registry HTTP API v2 subset (catalog, tags, manifests, monolithic blob upload) on /v2/; blobs are content-addressed files on local disk (or a registry:2 container when ECR_REGISTRY_URL is set). No Docker Hub compatibility. Image scan is an integrity scan (blob digest re-verification), NOT a vulnerability scan."),
	svc("apprunner", "App Runner", "Containers", "Containerised web services.", "layers", "/services/apprunner", "unavailable", "UNSUPPORTED",
		[]string{"service"}, nil, "", "Not started."),
	// Serverless
	svc("apigateway", "API Gateway", "Serverless", "HTTP APIs, resources, stages and deployments — real routing to Lambda and HTTP targets.", "zap", "/services/apigateway", "available", "PARTIAL",
		[]string{"api", "resource", "stage"}, []string{"serverless"},
		"APIs / Resources / Stages / Deploy flow; invoke surface; request logs",
		"Real routing to Lambda functions and SSRF-guarded HTTP targets with real request logging; a deployment snapshots resources into a stage. REST/HTTP only (no WebSocket APIs); invoke requires platform auth (no API keys / usage plans)."),
	svc("stepfunctions", "Step Functions", "Serverless", "State machines and executions.", "zap", "/services/stepfunctions", "unavailable", "UNSUPPORTED",
		[]string{"state-machine", "execution"}, []string{"serverless"}, "", "Not started (Phase 3)."),
	svc("appsync", "AppSync", "Serverless", "GraphQL APIs and schemas.", "zap", "/services/appsync", "unavailable", "UNSUPPORTED",
		[]string{"api", "schema"}, nil, "", "Not started."),
	// Developer Tools
	svc("terraform", "Terraform", "Developer Tools", "Infrastructure as code — real containerised Terraform runs (init/validate/plan/approve/apply/destroy).", "layers", "/services/terraform", "partial", "PARTIAL",
		[]string{"run", "workspace"}, []string{"terraform", "devops"},
		"Runs list; run detail (Plan output / diff, Approvals, Apply log); State view; Variables; Workspace screens",
		"Terraform runs only in containers (never on host); providers point at the local emulator/MinIO only; no Terraform Cloud, no module registry yet (Phase 6); no dedicated UI console yet — runs are API-driven and listed on the service page."),
	svc("terminal", "Cloud9-style Terminal", "Developer Tools", "Lab terminal — allowlisted commands over a short-lived token.", "zap", "/labs", "partial", "PARTIAL",
		[]string{"terminal-session"}, []string{"fundamentals"}, "", "Allowlisted commands only; simulated shell is labelled simulated. Opened from a lab's workstation page."),
	svc("codebuild", "CodeBuild", "Developer Tools", "Build projects — real local builds from a local git repo + buildspec.", "layers", "/services/codebuild", "available", "PARTIAL",
		[]string{"build-project", "build"}, []string{"devops"},
		"Build projects; Build history with real logs; build detail (phases, artifacts)",
		"Real builds: git checkout from a local path under CLOUDFORGE_BUILD_SOURCE_ROOT (no remote URLs), buildspec phases run as real shell commands in a subprocess with timeouts, real logs and artifacts on disk. No Docker build environment; no managed build fleet."),
	svc("codepipeline", "CodePipeline", "Developer Tools", "CI/CD pipelines — source → build → deploy, stages run for real.", "layers", "/services/codepipeline", "available", "PARTIAL",
		[]string{"pipeline"}, []string{"devops"},
		"Pipelines; Pipeline detail with stage visualisation; Executions with per-stage status",
		"Stages execute in order; a failed stage stops the execution. Deploy targets are the ECS service update and the Lambda code update that exist; Terraform deploy honestly fails without Docker."),
	svc("codedeploy", "CodeDeploy", "Developer Tools", "Deployments with real status and rollback to the previous revision.", "layers", "/services/codedeploy", "available", "PARTIAL",
		[]string{"deployment"}, []string{"devops"},
		"Applications; Deployment groups; Deployments with status and rollback action",
		"Deployments apply the revision for real and wait for the target to reach it (ECS desired count RUNNING on the new revision; Lambda code verified). Rollback deploys the previous revision through the same path. No EC2/on-premises instances, no blue/green traffic shifting."),
	svc("xray", "X-Ray", "Developer Tools", "Distributed traces.", "activity", "/services/xray", "unavailable", "UNSUPPORTED",
		[]string{"trace"}, nil, "", "Not started."),
	// Management & Governance
	svc("cloudwatch", "CloudWatch", "Monitoring", "Metrics, alarms and logs over the local control plane.", "activity", "/services/cloudwatch", "partial", "PARTIAL",
		[]string{"metric", "alarm", "dashboard"}, []string{"monitoring"},
		"Metrics / Alarms / Logs / Dashboards left nav; metric graphs; Create-alarm wizard",
		"Metrics are local control-plane metrics (the API's real Prometheus series + live /readyz component health), not AWS CloudWatch; alarms are real threshold rules evaluated live against current values, but alarm definitions are stored in memory and lost on restart; there is no Logs Insights-style log query yet."),
	svc("cloudtrail", "CloudTrail", "Management and Governance", "Audit event history (CloudForge's own audit log).", "activity", "/services/cloudtrail", "partial", "PARTIAL",
		[]string{"event"}, []string{"monitoring"},
		"Event history list (Event / User / Resource / Time); filters; Lookup / export flow",
		"Audit is CloudForge's own log, not AWS CloudTrail; retention 90 days; the event list is admin-only with filters (user/action/resource/date) and CSV/JSON export."),
	svc("config", "Config", "Management and Governance", "Resource configuration rules evaluated against real state.", "layers", "/services/config", "available", "PARTIAL",
		[]string{"rule", "resource"}, []string{"monitoring"},
		"Rules / Resources screens; Evaluate-rule action; configuration history",
		"Three rules (S3 bucket versioning, no open SSH, no open ingress) evaluated against real resources; non-compliant resources are listed with reasons and recorded as Config findings. Configuration history is recorded snapshots of lab resources."),
	svc("organizations", "Organizations", "Management and Governance", "Organizational units over the real user store.", "layers", "/services/organizations", "available", "PARTIAL",
		[]string{"account", "organizational-unit"}, []string{"iam"},
		"Accounts / Organizational units screens; Add-member flow",
		"Units and members are real platform users (learner/instructor/admin), not AWS accounts; no service control policies are enforced."),
	svc("ssm", "Systems Manager", "Management and Governance", "Managed instances and run command via the host agent.", "layers", "/services/ssm", "available", "PARTIAL",
		[]string{"document", "managed-instance"}, []string{"monitoring"},
		"Managed instances list; Run-command flow with real output; command history",
		"Inventory is the real host-agent inventory; Run Command executes allowlisted commands in a lab via the host agent and records the real output and exit code. No host agent is an honest 503 — commands are never faked."),
	svc("costexplorer", "Cost Explorer", "Management and Governance", "Cost and usage — educational estimates from real usage.", "layers", "/services/costexplorer", "available", "PARTIAL",
		[]string{"report"}, []string{"monitoring"},
		"Cost / Usage reports; cost breakdown by service; Budgets link",
		"Costs are computed from real usage (lab hours, S3 bytes, SQS messages, Lambda invocations, RDS/cache hours) at published teaching rates — educational estimates, never an AWS bill."),
	svc("budgets", "Budgets", "Management and Governance", "Budgets evaluated against the real cost model.", "layers", "/services/budgets", "available", "PARTIAL",
		[]string{"budget"}, []string{"monitoring"},
		"Budgets list; Budget detail (limit vs current cost, alert state)",
		"Budgets are evaluated against the real educational cost model on every read. No AWS Budgets integration, no email alerts."),
	svc("trustedadvisor", "Trusted Advisor", "Management and Governance", "Best-practice checks against real resource state.", "shield", "/services/trustedadvisor", "available", "PARTIAL",
		[]string{"check"}, []string{"monitoring"},
		"Checks list; Run-checks action; findings with evidence",
		"Five checks run against real state: idle labs, unattached/open security groups, quota usage, S3 buckets without versioning. A check with no matching resource creates no finding."),
	svc("resourceexplorer", "Resource Explorer", "Management and Governance", "Cross-service search over real local resources.", "search", "/explorer", "available", "REAL",
		[]string{"lab", "vpc", "subnet", "instance", "gateway", "loadbalancer", "route-table", "security-group", "course", "user"}, nil,
		"Cross-service resource search/list (the §4 Resource Explorer area)",
		"Searches local lab resources only; region scoping is display-only in Phase 1 (all resources live in cf-local-1)."),
	// Analytics
	svc("athena", "Athena", "Analytics", "Interactive SQL queries — real PostgreSQL SQL, not Presto/Athena.", "database", "/services/athena", "available", "PARTIAL",
		[]string{"query"}, []string{"databases"},
		"Query editor; Saved queries; Query history / executions; Results table",
		"PARTIAL: queries run read-only (SELECT/WITH only, single statement) against the platform Postgres or an RDS instance database. Saved queries and execution history are durable. There is no Presto/Athena engine."),
	svc("glue", "Glue", "Analytics", "Data catalog crawled from real sources.", "database", "/services/glue", "available", "PARTIAL",
		[]string{"job", "crawler"}, []string{"databases"},
		"Crawlers list; Run-crawler action; Data Catalog tables (table / columns)",
		"Crawlers read real sources: Postgres information_schema, DynamoDB tables, S3 buckets (MinIO). A source that cannot be reached makes the run PARTIAL, never silently skipped. No ETL jobs, no Spark."),
	svc("emr", "EMR", "Analytics", "Big-data clusters.", "database", "/services/emr", "unavailable", "UNSUPPORTED",
		[]string{"cluster", "step"}, nil, "", "Not started (Phase 5)."),
	svc("kinesis", "Kinesis", "Analytics", "Real-time data streams — durable, Postgres-backed.", "activity", "/services/kinesis", "available", "API_EMULATED",
		[]string{"stream"}, []string{"messaging"},
		"Data streams list; PutRecord flow; GetRecords with sequence numbers; shard view",
		"Durable Postgres-backed streams with sequence numbers and shard assignment by partition-key hash. No enhanced fan-out, no Kinesis Data Analytics."),
	svc("quicksight", "QuickSight", "Analytics", "Dashboards — real charts from live Athena results.", "activity", "/services/quicksight", "available", "PARTIAL",
		[]string{"dashboard", "analysis"}, []string{"databases"},
		"Dashboards list; Dashboard detail with chart from live query results",
		"A dashboard is a saved Athena query plus a bar/line chart rendered from the live query results — no sample data, no SPICE, no sharing."),
	svc("firehose", "Kinesis Data Firehose", "Analytics", "Delivery streams to S3 or Postgres, with real delivery records.", "activity", "/services/firehose", "available", "PARTIAL",
		[]string{"delivery-stream"}, []string{"messaging"},
		"Delivery streams list; PutRecord flow; delivery records",
		"Delivery to S3 (MinIO — honest 503 when MinIO is down) or to a Postgres table as JSONB rows, with a real delivery record per put. No buffering, no Lambda transforms."),
	svc("opensearch", "OpenSearch", "Analytics", "Search and analytics domains.", "database", "/services/opensearch", "unavailable", "UNSUPPORTED",
		[]string{"domain"}, nil, "", "Not started (Phase 5)."),
	// ML / AI
	svc("sagemaker", "SageMaker", "Machine Learning and AI", "ML notebooks, real training jobs, model registry and inference.", "flask", "/services/sagemaker", "available", "PARTIAL",
		[]string{"notebook", "model", "endpoint"}, []string{"databases"},
		"Notebook instances; Training jobs; Models; Endpoints; Invoke action",
		"Notebooks run cells in the sandboxed Python/Node subprocess (same as Lambda). Training is a real OLS linear regression in Go on a built-in or user-supplied tiny dataset (no scikit-learn here); metrics are computed from the data. Models are versioned JSON artifacts on disk; endpoints serve real predictions from stored weights. Linear models only; no GPU."),
	svc("bedrock", "Bedrock", "Machine Learning and AI", "Foundation-model playground — SIMULATED concept entries only.", "flask", "/services/bedrock", "available", "SIMULATED",
		[]string{"model"}, nil,
		"Foundation models list; Playground invoke flow",
		"SIMULATED: template responses only — no foundation model runs, nothing is sent to any AI provider, no paid third-party API. For a real model result, train a SageMaker model and invoke its endpoint."),
	svc("rekognition", "Rekognition", "Machine Learning and AI", "Image and video analysis.", "flask", "/services/rekognition", "unavailable", "UNSUPPORTED",
		[]string{"collection"}, nil, "", "Not started (Phase 5)."),
	svc("comprehend", "Comprehend", "Machine Learning and AI", "Natural-language analysis.", "flask", "/services/comprehend", "unavailable", "UNSUPPORTED",
		[]string{"job"}, nil, "", "Not started (Phase 5)."),
	svc("lex", "Lex", "Machine Learning and AI", "Conversational bots.", "flask", "/services/lex", "unavailable", "UNSUPPORTED",
		[]string{"bot"}, nil, "", "Not started (Phase 5)."),
	svc("polly", "Polly", "Machine Learning and AI", "Text-to-speech.", "flask", "/services/polly", "unavailable", "UNSUPPORTED",
		[]string{"voice"}, nil, "", "Not started (Phase 5)."),
	// Migration & Transfer
	svc("dms", "DMS", "Migration and Transfer", "Database migration — real row copies between Postgres databases.", "layers", "/services/dms", "available", "PARTIAL",
		[]string{"task", "endpoint"}, []string{"databases"},
		"Replication tasks list; Task detail (row counts, migration log); Create-task wizard",
		"Full-load row copies between the platform Postgres and RDS instance databases (up to 5000 rows per table) with real row counts and a migration log. No CDC, no ongoing replication."),
	svc("mgn", "Application Migration", "Migration and Transfer", "Lift-and-shift source servers with a replication lifecycle — records plus a teaching state machine.", "layers", "/services/mgn", "available", "API_EMULATED",
		[]string{"source-server"}, []string{"aws-sap"},
		"Source servers list; Start-replication / Test-launch / Cutover actions",
		"No agent is installed and no blocks are copied: replication, test launch and cutover move the server along a teaching lifecycle (SIMULATED) so the cutover lesson can be practised end to end."),
	svc("datasync", "DataSync", "Migration and Transfer", "Data transfer — real copies with SHA-256 verification.", "layers", "/services/datasync", "available", "PARTIAL",
		[]string{"task", "agent"}, []string{"databases"},
		"Tasks list; Task detail (files/bytes copied, verified count); transfer log",
		"Real copies between local workdir paths and S3 buckets with SHA-256 verification per file. Local paths are confined to the workdir root; S3 needs MinIO. No DataSync agents."),
	svc("migrationhub", "Migration Hub", "Migration and Transfer", "Tracking for the real DMS and DataSync tasks.", "layers", "/services/migrationhub", "available", "PARTIAL",
		[]string{"migration-task"}, []string{"databases"},
		"Migration tasks list (DMS + DataSync) with status summary",
		"Migration Hub tracks the real DMS and DataSync tasks — it creates nothing of its own."),
	svc("transfer", "Transfer Family", "Migration and Transfer", "Managed file transfer — UNSUPPORTED (no local SFTP server).", "layers", "/services/transfer", "unavailable", "UNSUPPORTED",
		[]string{"server"}, nil,
		"Servers list — the console states UNSUPPORTED and why",
		"Phase 6 decision: UNSUPPORTED — a managed SFTP endpoint needs a real local SFTP server and this host has no sshd/sftp-server binary. Use DataSync for file transfer in CloudForge."),
	// Additional
	svc("cloudformation", "CloudFormation", "Additional", "Stacks and templates — own template engine with change sets and rollback.", "layers", "/services/cloudformation", "available", "PARTIAL",
		[]string{"stack"}, []string{"terraform"},
		"Stacks list; Stack detail (Resources / Events / Template); Create-stack wizard; change-set preview",
		"A separate template engine from Terraform (JSON/YAML, own stack state, change-set diff, rollback on failure). Provisions SQS, SNS, DynamoDB (subset), Lambda, S3 (MinIO) and RDS only; other resource types are UNSUPPORTED in the change set. Rollback deletes resources created in the failed execution; resources deleted by an update are NOT restored."),
	svc("backup", "Backup", "Additional", "Backup plans, vaults and jobs — plans/vaults are records; a run is a real pg_dump.", "hard-drive", "/services/backup", "available", "REAL",
		[]string{"backup-plan", "vault"}, []string{"aws-saa"},
		"Backup plans / Vaults / Jobs screens; Run-plan action",
		"Plans and vaults are durable records (no schedule runs by itself). A run is a real pg_dump -Fc of the platform Postgres with SHA-256 recorded; without a platform database or pg_dump on the API host the job is FAILED with the reason. No AWS Backup vaults; restore is scripts/backup.sh + restore.sh."),
	svc("servicecatalog", "Service Catalog", "Additional", "Portfolios and products.", "book", "/services/servicecatalog", "unavailable", "UNSUPPORTED",
		[]string{"portfolio", "product"}, nil, "", "Not started."),
	svc("licensemanager", "License Manager", "Additional", "Software licences.", "book", "/services/licensemanager", "unavailable", "UNSUPPORTED",
		[]string{"license"}, nil, "", "Not started."),
	// Certification coverage (SAA-C03 / SAP-C02) — catalogued honestly as
	// unavailable until a console or runtime exists.
	svc("autoscaling", "EC2 Auto Scaling", "Compute", "Auto Scaling groups — durable records with min/desired/max and scaling.", "server", "/services/autoscaling", "available", "API_EMULATED",
		[]string{"auto-scaling-group"}, []string{"aws-saa", "aws-sap"},
		"Auto Scaling groups list; Create-group wizard; Scale action",
		"Groups record min/desired/max and the launch profile; scaling sets the recorded desired capacity. No instances are launched or terminated — use a lab to launch real instances. Group size capped at 10."),
	svc("globalaccelerator", "Global Accelerator", "Networking", "Anycast accelerators with endpoint groups and failover history.", "network", "/services/globalaccelerator", "available", "API_EMULATED",
		[]string{"accelerator"}, []string{"aws-saa", "aws-sap"},
		"Accelerators list; Endpoint groups; Failover action",
		"Static IPs are teaching addresses in 192.0.2.0/24 (TEST-NET-1); no traffic is routed and no health checks run. Failover switches the recorded active endpoint group."),
	svc("directconnect", "Direct Connect", "Networking", "Dedicated connections — durable records with a confirm step.", "network", "/services/directconnect", "available", "API_EMULATED",
		[]string{"connection"}, []string{"aws-sap"},
		"Connections list; Create-connection wizard; Confirm action",
		"No physical cross-connect, LOA-CFA or BGP session exists; confirming a connection is a teaching state change from REQUESTED to AVAILABLE."),
	svc("controltower", "Control Tower", "Management", "Landing zones with guardrails — durable records.", "shield", "/services/controltower", "available", "API_EMULATED",
		[]string{"landing-zone"}, []string{"aws-sap"},
		"Landing zones list; Guardrails enable/disable",
		"No accounts are vended and no SCPs or Config rules are enforced; guardrails are recorded settings. Enforcement is modelled in the Organizations/SCP lessons."),
	svc("identitycenter", "IAM Identity Center", "Security", "Workforce SSO permission sets and account assignments — durable records.", "shield", "/services/identitycenter", "available", "API_EMULATED",
		[]string{"permission-set"}, []string{"aws-saa", "aws-sap"},
		"Permission sets list; Create-permission-set wizard; Assignments",
		"No IdP is federated and assignments grant no access to any account; they are recorded for teaching. The IAM evaluator is the enforcement model."),
	svc("ram", "Resource Access Manager", "Security", "Cross-account resource sharing — durable share records.", "share", "/services/ram", "available", "API_EMULATED",
		[]string{"resource-share"}, []string{"aws-sap"},
		"Resource shares list; Principals add/remove",
		"No second account exists, so sharing changes no access; principals are recorded names, not validated against any account."),
	svc("storagegateway", "Storage Gateway", "Storage", "Hybrid file, volume and tape gateways — durable records with activation.", "hard-drive", "/services/storagegateway", "available", "API_EMULATED",
		[]string{"gateway"}, []string{"aws-sap"},
		"Gateways list; Activate action; File shares",
		"No gateway appliance is deployed, no iSCSI/NFS endpoint exists and no data is cached or uploaded. Activation is a teaching state change."),
	svc("snowball", "Snowball", "Storage", "Petabyte-scale offline transfer jobs — durable records with a shipping lifecycle.", "hard-drive", "/services/snowball", "available", "API_EMULATED",
		[]string{"job"}, []string{"aws-sap"},
		"Jobs list; Advance action along the lifecycle",
		"No device is shipped and no data is copied; the lifecycle (created → preparing → shipped → delivered → completed) is a teaching state machine. DataSync and DMS records are the available migration practice."),
	svc("lakeformation", "Lake Formation", "Analytics", "Fine-grained data-lake permissions — durable grant records.", "database", "/services/lakeformation", "available", "API_EMULATED",
		[]string{"permission"}, []string{"aws-sap"},
		"Permissions list; Grant/Revoke flows",
		"Grants are recorded, not enforced: no data lake exists and grants do not change access to any Glue table or S3 location."),
	svc("macie", "Macie", "Security", "Sensitive-data discovery — classification jobs that scan real local metadata.", "shield", "/services/macie", "available", "API_EMULATED",
		[]string{"finding"}, []string{"aws-saa", "aws-sap"},
		"Classification jobs list; Run action; Findings",
		"A run scans S3 bucket names (when MinIO answers) and DynamoDB table names for sensitive-data name patterns and records findings in the shared findings table. Object/table contents are never read; no finding is invented."),
	svc("computeoptimizer", "Compute Optimizer", "Management", "Rightsizing recommendations computed from real lab state.", "trending-up", "/services/computeoptimizer", "available", "API_EMULATED",
		[]string{"recommendation"}, []string{"aws-saa"},
		"Recommendations list; Refresh action",
		"Recommendations come from lab profile and state only — no CloudWatch utilization exists here, so every recommendation says so and carries LOW confidence (MEDIUM for a stopped lab). Never presented as measured rightsizing."),
	svc("savingsplans", "Savings Plans", "Management", "Commitment plans with utilization computed from real lab hours.", "trending-up", "/services/savingsplans", "available", "API_EMULATED",
		[]string{"savings-plan"}, []string{"aws-saa"},
		"Savings plans list; Utilization view",
		"Plans are records, not real AWS commitments: no money is committed and no discount is applied. Utilization is real arithmetic over lab hours at the teaching rates."),
	svc("auditmanager", "Audit Manager", "Security", "Audit assessments with evidence counted from the real audit log.", "clipboard", "/services/auditmanager", "available", "API_EMULATED",
		[]string{"assessment"}, []string{"aws-sap"},
		"Assessments list; Collect-evidence action",
		"Evidence is a count of real audit-log entries since the assessment started; no AWS evidence is collected and no evidence set is fabricated."),
	svc("artifact", "Artifact", "Security", "Compliance reports catalogue and the organization's own agreements.", "file-text", "/services/artifact", "available", "API_EMULATED",
		[]string{"report"}, []string{"aws-sap"},
		"Reports catalogue; Agreements list",
		"The report list is a catalogue of real AWS Artifact report names; CloudForge does not redistribute AWS compliance documents. Agreements are the organization's own records; accepting here accepts nothing with AWS."),
	// Products (not AWS services): Learning Center + Terraform Learning
	svc("learning-center", "Learning Center", "Learning Center", "Tracks, lessons, assessments, achievements and XP.", "book", "/learn", "available", "PARTIAL",
		[]string{"track", "lesson", "assessment"}, nil,
		"Skill Builder-style home: paths, tracks, lesson player, assessments, achievements",
		"No instructor dashboard or SCORM/LTI export yet; 16 tracks include SAA-C03 and SAP-C02, and 6 lab definitions are launchable."),
	svc("terraform-learning", "Terraform Learning", "Terraform Learning", "Terraform concepts with hands-on runs.", "book", "/learn", "partial", "PARTIAL",
		[]string{"run", "module"}, []string{"terraform"}, "", "No module registry / workspace UI yet (Phase 6)."),
}

// registryServiceID maps a providers-registry service name to a
// serviceCatalog id. Most match 1:1; the registry has no entries for
// most catalog services, which is exactly the point — those services
// report zero operations and UNSUPPORTED fidelity from the registry.
func registryServiceID(catalogID string) string { return catalogID }

// buildServiceInfo merges one metadata row with the live registry.
func (s *Server) buildServiceInfo(m serviceMeta) ServiceInfo {
	ops := []ServiceOperation{}
	if s.Registry != nil {
		for _, c := range s.Registry.ListByService(registryServiceID(m.id)) {
			ops = append(ops, ServiceOperation{
				Operation: c.Operation, Fidelity: c.Fidelity, Supported: c.Supported,
				HTTPRoute: httpRouteFor(m.id, c.Operation), Limitations: c.Limitations,
			})
		}
	}
	// Deterministic order for the wire: registry order is stable, but
	// sort by operation name so the response is stable across runs.
	sort.Slice(ops, func(i, j int) bool { return ops[i].Operation < ops[j].Operation })
	if ops == nil {
		ops = []ServiceOperation{}
	}
	docs := "/learn"
	if len(m.learning) > 0 {
		docs = "/learn"
	}
	return ServiceInfo{
		ID: m.id, Name: m.name, Category: m.category, Description: m.description,
		Icon: m.icon, Route: m.route, Availability: m.availability, Fidelity: m.fidelity,
		Operations: ops, ResourceTypes: m.resourceTypes, DocsURL: docs, Learning: m.learning,
		Mirrors: m.mirrors, Differs: m.differs,
	}
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	out := make([]ServiceInfo, 0, len(serviceCatalog))
	for _, m := range serviceCatalog {
		out = append(out, s.buildServiceInfo(m))
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"services": out, "count": len(out)})
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, m := range serviceCatalog {
		if m.id == id {
			s.writeJSON(w, r, http.StatusOK, s.buildServiceInfo(m))
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "service_not_found", "no service with that id")
}

// --- Resource explorer -----------------------------------------------------

// explorerItem is one row of GET /api/v1/explorer/resources. Every row
// comes from a real backend store; nothing here is synthesised.
type explorerItem struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Region     string            `json:"region"`
	Status     string            `json:"status"`
	CreatedAt  string            `json:"created_at,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Provider   string            `json:"provider"`
	Fidelity   string            `json:"fidelity"`
	LabID      string            `json:"lab_id,omitempty"`
	ServiceID  string            `json:"service_id"`
	DetailURL  string            `json:"detail_url"`
	CanDelete  bool              `json:"can_delete"`
	DeleteHint string            `json:"delete_hint,omitempty"`
}

// explorerRegion is the only region real resources live in. Region
// scoping in Phase 1 is display-only: labs carry no per-region field,
// so every resource reports cf-local-1 and a request for another
// region returns an empty list with a note (never invented rows).
const explorerRegion = "cf-local-1"

func (s *Server) handleExplorerResources(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	isAdmin := u.Role == "admin"
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	region := strings.TrimSpace(r.URL.Query().Get("region"))
	sortBy := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortBy == "" {
		sortBy = "name"
	}
	limit, offset := parsePagination(r)
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	items := []explorerItem{}
	regionNote := ""
	if region != "" && region != explorerRegion {
		// Display-only scoping, stated honestly: no resource lives
		// in another region, so the list is empty rather than faked.
		regionNote = "Region scoping is display-only in Phase 1: all CloudForge resources live in " + explorerRegion + "; nothing is stored in " + region + "."
	} else {
		// Labs (own labs; admins see everyone's).
		var labIDs []string
		if isAdmin {
			all, err := s.Labs.ListAll(r.Context())
			if err == nil {
				for _, lab := range all {
					labIDs = append(labIDs, lab.ID)
					items = append(items, explorerItem{
						ID: lab.ID, Type: "lab", Name: labName(lab.Name, lab.ID), Region: explorerRegion,
						Status: lab.State, CreatedAt: lab.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
						Tags:     map[string]string{"definition": lab.DefinitionID, "owner": lab.OwnerID},
						Provider: "cloudforge", Fidelity: providers.FidelitySimulated, ServiceID: "ec2",
						DetailURL: "/labs", CanDelete: true,
					})
				}
			}
		} else {
			own, err := s.Labs.ListByOwner(r.Context(), u.ID)
			if err == nil {
				for _, lab := range own {
					labIDs = append(labIDs, lab.ID)
					items = append(items, explorerItem{
						ID: lab.ID, Type: "lab", Name: labName(lab.Name, lab.ID), Region: explorerRegion,
						Status: lab.State, CreatedAt: lab.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
						Tags:     map[string]string{"definition": lab.DefinitionID},
						Provider: "cloudforge", Fidelity: providers.FidelitySimulated, ServiceID: "ec2",
						DetailURL: "/labs", CanDelete: true,
					})
				}
			}
		}
		// Lab resources, per lab the caller may see.
		if s.Resources != nil {
			for _, labID := range labIDs {
				res, err := s.Resources.ListResources(r.Context(), labID)
				if err != nil {
					continue
				}
				for _, resItem := range res {
					kind := resItem.Kind
					if kind == "" {
						kind = "unknown"
					}
					items = append(items, explorerItem{
						ID: resItem.ID, Type: kind, Name: resItem.Name, Region: explorerRegion,
						Status: resItem.State, Tags: resItem.Labels,
						Provider: resItem.Provider, Fidelity: providers.FidelitySimulated,
						LabID: labID, ServiceID: explorerServiceForKind(kind),
						DetailURL: "/labs/" + labID + "/topology", CanDelete: true,
					})
					if resItem.Provider == "" {
						items[len(items)-1].Provider = "cloudforge"
					}
				}
			}
		}
		// Courses are public.
		if s.Courses != nil {
			courses, err := s.Courses.List(r.Context())
			if err == nil {
				for _, c := range courses {
					items = append(items, explorerItem{
						ID: c.ID, Type: "course", Name: c.Title, Region: explorerRegion,
						Status: "available", Tags: map[string]string{"track": c.Track},
						Provider: "cloudforge", Fidelity: providers.FidelityReal, ServiceID: "learning-center",
						DetailURL: "/courses", CanDelete: false,
						DeleteHint: "Courses are deleted by an admin from the course API, not from the explorer.",
					})
				}
			}
		}
		// Users are admin-only.
		if isAdmin && s.Users != nil {
			users, err := s.Users.List()
			if err == nil {
				for _, usr := range users {
					items = append(items, explorerItem{
						ID: usr.ID, Type: "user", Name: usr.Name, Region: explorerRegion,
						Status: usr.Role, Tags: map[string]string{"email": usr.Email},
						Provider: "cloudforge", Fidelity: providers.FidelityReal, ServiceID: "iam",
						DetailURL: "/account", CanDelete: false,
						DeleteHint: "Users cannot be deleted from the explorer.",
					})
				}
			}
		}
	}

	// Filters (fresh slice — never filter in place over the source).
	filtered := []explorerItem{}
	for _, it := range items {
		if typeFilter != "" && it.Type != typeFilter {
			continue
		}
		if statusFilter != "" && !strings.EqualFold(it.Status, statusFilter) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(it.ID + " " + it.Name + " " + it.Type + " " + it.ServiceID)
			for k, v := range it.Tags {
				hay += " " + strings.ToLower(k+" "+v)
			}
			if !strings.Contains(hay, q) {
				continue
			}
		}
		filtered = append(filtered, it)
	}
	switch sortBy {
	case "created_at":
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].CreatedAt < filtered[j].CreatedAt })
	case "status":
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Status < filtered[j].Status })
	default:
		sort.Slice(filtered, func(i, j int) bool {
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		})
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]
	if page == nil {
		page = []explorerItem{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"resources": page, "total": total, "limit": limit, "offset": offset,
		"region": regionOrDefault(region), "region_scoped": false, "region_note": regionNote,
	})
}

func regionOrDefault(region string) string {
	if region == "" {
		return explorerRegion
	}
	return region
}

func labName(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// explorerServiceForKind maps a lab-resource kind to its service id.
func explorerServiceForKind(kind string) string {
	switch kind {
	case "vpc":
		return "vpc"
	case "subnet":
		return "subnet"
	case "instance":
		return "ec2"
	case "gateway":
		return "igw"
	case "loadbalancer":
		return "elb"
	case "route-table":
		return "routetable"
	case "security-group":
		return "sg"
	default:
		return "vpc"
	}
}
