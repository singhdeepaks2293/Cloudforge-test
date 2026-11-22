import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout.tsx'
import { Account } from './pages/Account.tsx'
import { Admin } from './pages/Admin.tsx'
import { Console } from './pages/Console.tsx'
import { Courses } from './pages/Courses.tsx'
import { Dashboard } from './pages/Dashboard.tsx'
import { CloudTrailConsole } from './pages/CloudTrailConsole.tsx'
import { CloudWatchConsole } from './pages/CloudWatchConsole.tsx'
import { DynamoDbConsole } from './pages/DynamoDbConsole.tsx'
import { Ec2Console } from './pages/Ec2Console.tsx'
import { ElastiCacheConsole } from './pages/ElastiCacheConsole.tsx'
import { ElbConsole } from './pages/ElbConsole.tsx'
import { EventBridgeConsole } from './pages/EventBridgeConsole.tsx'
import { Explorer } from './pages/Explorer.tsx'
import { IamConsole } from './pages/IamConsole.tsx'
import { LambdaConsole } from './pages/LambdaConsole.tsx'
import { Labs } from './pages/Labs.tsx'
import { LabWorkstation } from './pages/LabWorkstation.tsx'
import { Learn } from './pages/Learn.tsx'
import { Login, Register } from './pages/Auth.tsx'
import { ApiGatewayConsole } from './pages/ApiGatewayConsole.tsx'
import { CloudFormationConsole } from './pages/CloudFormationConsole.tsx'
import { AthenaConsole, FirehoseConsole, GlueConsole, KinesisConsole, QuickSightConsole } from './pages/AnalyticsConsoles.tsx'
import { BedrockConsole, SageMakerConsole } from './pages/MlConsoles.tsx'
import { DataSyncConsole, DmsConsole, MigrationHubConsole, TransferConsole } from './pages/MigrationConsoles.tsx'
import { ConfigConsole, GuardDutyConsole, InspectorConsole, KmsConsole, SecretsConsole, SecurityHubConsole, WafConsole } from './pages/SecurityConsoles.tsx'
import { FlowLogsConsole, NetworkFirewallConsole, TransitGatewayConsole, VpcEndpointsConsole, VpcPeeringConsole } from './pages/NetworkingConsoles.tsx'
import { CostExplorerConsole, OrganizationsConsole, SsmConsole, TrustedAdvisorConsole } from './pages/ManagementConsoles.tsx'
import { CodeBuildConsole } from './pages/CodeBuildConsole.tsx'
import { CodeDeployConsole } from './pages/CodeDeployConsole.tsx'
import { CodePipelineConsole } from './pages/CodePipelineConsole.tsx'
import { EcrConsole } from './pages/EcrConsole.tsx'
import { EcsConsole } from './pages/EcsConsole.tsx'
import { EksConsole } from './pages/EksConsole.tsx'
import { RdsConsole } from './pages/RdsConsole.tsx'
import { Route53Console } from './pages/Route53Console.tsx'
import { S3Console } from './pages/S3Console.tsx'
import { SnsConsole } from './pages/SnsConsole.tsx'
import { SqsConsole } from './pages/SqsConsole.tsx'
import { TerraformLearning } from './pages/TerraformLearning.tsx'
import { ServiceDetail } from './pages/ServiceDetail.tsx'
import { Services } from './pages/Services.tsx'
import { Topology } from './pages/Topology.tsx'
import { VpcConsole } from './pages/VpcConsole.tsx'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="learn" element={<Learn />} />
            <Route path="courses" element={<Courses />} />
            <Route path="labs" element={<Labs />} />
            <Route path="labs/:id/topology" element={<Topology />} />
            <Route path="labs/:id/workstation" element={<LabWorkstation />} />
            <Route path="console" element={<Console />} />
            {/* Phase 1 — console foundation: registry-driven services,
                resource explorer, per-service consoles, account/admin. */}
            <Route path="services" element={<Services />} />
            <Route path="services/ec2" element={<Ec2Console />} />
            <Route path="services/s3" element={<S3Console />} />
            <Route path="services/vpc" element={<VpcConsole />} />
            <Route path="services/iam" element={<IamConsole />} />
            <Route path="services/cloudwatch" element={<CloudWatchConsole />} />
            <Route path="services/cloudtrail" element={<CloudTrailConsole />} />
            {/* Phase 3 — data and application services. */}
            <Route path="services/dynamodb" element={<DynamoDbConsole />} />
            <Route path="services/rds" element={<RdsConsole />} />
            <Route path="services/elasticache" element={<ElastiCacheConsole />} />
            <Route path="services/elb" element={<ElbConsole />} />
            <Route path="services/route53" element={<Route53Console />} />
            <Route path="services/sqs" element={<SqsConsole />} />
            <Route path="services/sns" element={<SnsConsole />} />
            <Route path="services/eventbridge" element={<EventBridgeConsole />} />
            <Route path="services/lambda" element={<LambdaConsole />} />
            <Route path="services/apigateway" element={<ApiGatewayConsole />} />
            {/* Phase 4 — containers and developer tools. */}
            <Route path="services/ecr" element={<EcrConsole />} />
            <Route path="services/ecs" element={<EcsConsole />} />
            <Route path="services/eks" element={<EksConsole />} />
            <Route path="services/codebuild" element={<CodeBuildConsole />} />
            <Route path="services/codepipeline" element={<CodePipelineConsole />} />
            <Route path="services/codedeploy" element={<CodeDeployConsole />} />
            <Route path="services/cloudformation" element={<CloudFormationConsole />} />
            {/* Phase 6 — advanced services. */}
            <Route path="services/athena" element={<AthenaConsole />} />
            <Route path="services/glue" element={<GlueConsole />} />
            <Route path="services/kinesis" element={<KinesisConsole />} />
            <Route path="services/firehose" element={<FirehoseConsole />} />
            <Route path="services/quicksight" element={<QuickSightConsole />} />
            <Route path="services/sagemaker" element={<SageMakerConsole />} />
            <Route path="services/bedrock" element={<BedrockConsole />} />
            <Route path="services/dms" element={<DmsConsole />} />
            <Route path="services/datasync" element={<DataSyncConsole />} />
            <Route path="services/transfer" element={<TransferConsole />} />
            <Route path="services/migrationhub" element={<MigrationHubConsole />} />
            <Route path="services/guardduty" element={<GuardDutyConsole />} />
            <Route path="services/securityhub" element={<SecurityHubConsole />} />
            <Route path="services/inspector" element={<InspectorConsole />} />
            <Route path="services/config" element={<ConfigConsole />} />
            <Route path="services/waf" element={<WafConsole />} />
            <Route path="services/kms" element={<KmsConsole />} />
            <Route path="services/secretsmanager" element={<SecretsConsole />} />
            <Route path="services/vpcpeering" element={<VpcPeeringConsole />} />
            <Route path="services/transitgateway" element={<TransitGatewayConsole />} />
            <Route path="services/vpcendpoints" element={<VpcEndpointsConsole />} />
            <Route path="services/networkfirewall" element={<NetworkFirewallConsole />} />
            <Route path="services/flowlogs" element={<FlowLogsConsole />} />
            <Route path="services/ssm" element={<SsmConsole />} />
            <Route path="services/trustedadvisor" element={<TrustedAdvisorConsole />} />
            <Route path="services/costexplorer" element={<CostExplorerConsole />} />
            <Route path="services/organizations" element={<OrganizationsConsole />} />
            <Route path="terraform-learning" element={<TerraformLearning />} />
            <Route path="services/terraform-learning" element={<TerraformLearning />} />
            <Route path="services/:id" element={<ServiceDetail />} />
            <Route path="explorer" element={<Explorer />} />
            <Route path="account" element={<Account />} />
            <Route path="admin" element={<Admin />} />
            <Route path="login" element={<Login />} />
            <Route path="register" element={<Register />} />
            <Route
              path="*"
              element={
                <div>
                  <h1 className="text-xl font-semibold">Page not found</h1>
                  <p className="mt-1 text-sm text-aws-muted">That route does not exist yet.</p>
                </div>
              }
            />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
