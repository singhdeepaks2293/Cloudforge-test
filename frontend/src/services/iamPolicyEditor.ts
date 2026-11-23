/**
 * IAM policy editor logic — the pure (DOM-free) core of the tabbed
 * Visual / JSON policy editor in IamConsole.tsx.
 *
 * The backend (backend/internal/providers/iam.go, ValidatePolicyDocument)
 * only accepts a real subset of IAM policy grammar:
 *   - Version 2012-10-17 (or 2008-10-17)
 *   - Effect Allow / Deny only
 *   - Action / Resource as a string or a list of strings, wildcards OK
 *   - Condition only StringEquals on aws:username
 *   - NO policy variables (${...}), NO NotAction / NotResource,
 *     NO Principal in identity policies
 * Anything outside that is rejected by the backend, so the visual
 * editor only offers what the backend accepts, and JSON that uses an
 * unsupported feature is never silently rewritten into the visual
 * form — it stays as JSON with a warning naming the feature.
 */

export type PolicyEffect = 'Allow' | 'Deny'

export interface VisualStatement {
  effect: PolicyEffect
  /** Service prefix, e.g. "s3". "*" means the "*" action. */
  service: string
  /** Selected actions for this service, full "prefix:Action" strings. */
  actions: string[]
  /** Extra action typed by hand, e.g. "s3:*" — merged into Action. */
  customAction: string
  /** Resource input as typed (comma-separated for several resources). */
  resources: string
  /** aws:username for a StringEquals condition, "" for no condition. */
  username: string
}

export interface ServiceActions {
  id: string
  name: string
  /** Common actions for the service, full "prefix:Action" strings. */
  actions: string[]
  /** Example resource patterns offered in the resource input. */
  resourcePatterns: string[]
}

/**
 * Services that exist in the CloudForge registry, with the common
 * actions a learner reaches for. Action names are the real AWS IAM
 * action names for the service; VPC actions live under the ec2:
 * prefix in AWS, so the VPC entry offers ec2:CreateVpc etc.
 */
export const POLICY_SERVICES: ServiceActions[] = [
  {
    id: 's3', name: 'S3', actions: ['s3:ListBuckets', 's3:GetObject', 's3:PutObject', 's3:DeleteObject', 's3:CreateBucket', 's3:DeleteBucket', 's3:ListBucket', 's3:GetBucketPolicy'],
    resourcePatterns: ['*', 'arn:cloudforge:s3:::my-bucket', 'arn:cloudforge:s3:::my-bucket/*'],
  },
  {
    id: 'ec2', name: 'EC2', actions: ['ec2:DescribeInstances', 'ec2:RunInstances', 'ec2:StartInstances', 'ec2:StopInstances', 'ec2:TerminateInstances', 'ec2:DescribeVolumes', 'ec2:CreateTags'],
    resourcePatterns: ['*', 'arn:cloudforge:ec2:::instance/*'],
  },
  {
    id: 'iam', name: 'IAM', actions: ['iam:GetUser', 'iam:CreateUser', 'iam:DeleteUser', 'iam:ListUsers', 'iam:GetPolicy', 'iam:CreatePolicy', 'iam:AttachUserPolicy', 'iam:PassRole'],
    resourcePatterns: ['*', 'arn:cloudforge:iam::local:user/*', 'arn:cloudforge:iam::local:policy/*'],
  },
  {
    id: 'dynamodb', name: 'DynamoDB', actions: ['dynamodb:GetItem', 'dynamodb:PutItem', 'dynamodb:DeleteItem', 'dynamodb:Query', 'dynamodb:Scan', 'dynamodb:CreateTable', 'dynamodb:DescribeTable'],
    resourcePatterns: ['*', 'arn:cloudforge:dynamodb:::table/my-table'],
  },
  {
    id: 'lambda', name: 'Lambda', actions: ['lambda:InvokeFunction', 'lambda:CreateFunction', 'lambda:DeleteFunction', 'lambda:GetFunction', 'lambda:ListFunctions', 'lambda:UpdateFunctionCode'],
    resourcePatterns: ['*', 'arn:cloudforge:lambda:::function:my-function'],
  },
  {
    id: 'rds', name: 'RDS', actions: ['rds:DescribeDBInstances', 'rds:CreateDBInstance', 'rds:DeleteDBInstance', 'rds:StartDBInstance', 'rds:StopDBInstance'],
    resourcePatterns: ['*', 'arn:cloudforge:rds:::db:my-db'],
  },
  {
    id: 'vpc', name: 'VPC', actions: ['ec2:CreateVpc', 'ec2:DeleteVpc', 'ec2:DescribeVpcs', 'ec2:CreateSubnet', 'ec2:DeleteSubnet', 'ec2:DescribeSubnets', 'ec2:CreateSecurityGroup'],
    resourcePatterns: ['*', 'arn:cloudforge:ec2:::vpc/*'],
  },
  {
    id: 'sqs', name: 'SQS', actions: ['sqs:SendMessage', 'sqs:ReceiveMessage', 'sqs:DeleteMessage', 'sqs:GetQueueUrl', 'sqs:CreateQueue', 'sqs:ListQueues'],
    resourcePatterns: ['*', 'arn:cloudforge:sqs:::my-queue'],
  },
  {
    id: 'sns', name: 'SNS', actions: ['sns:Publish', 'sns:Subscribe', 'sns:CreateTopic', 'sns:DeleteTopic', 'sns:ListTopics'],
    resourcePatterns: ['*', 'arn:cloudforge:sns:::my-topic'],
  },
  {
    id: 'route53', name: 'Route 53', actions: ['route53:ListHostedZones', 'route53:CreateHostedZone', 'route53:ChangeResourceRecordSets', 'route53:GetHostedZone'],
    resourcePatterns: ['*', 'arn:cloudforge:route53:::hostedzone/*'],
  },
  {
    id: 'cloudwatch', name: 'CloudWatch', actions: ['cloudwatch:GetMetricData', 'cloudwatch:PutMetricData', 'cloudwatch:DescribeAlarms', 'cloudwatch:PutMetricAlarm', 'logs:CreateLogGroup', 'logs:PutLogEvents'],
    resourcePatterns: ['*'],
  },
  {
    id: 'cloudtrail', name: 'CloudTrail', actions: ['cloudtrail:DescribeTrails', 'cloudtrail:CreateTrail', 'cloudtrail:StartLogging', 'cloudtrail:LookupEvents'],
    resourcePatterns: ['*'],
  },
  {
    id: 'kms', name: 'KMS', actions: ['kms:Encrypt', 'kms:Decrypt', 'kms:GenerateDataKey', 'kms:CreateKey', 'kms:DescribeKey'],
    resourcePatterns: ['*', 'arn:cloudforge:kms:::key/*'],
  },
  {
    id: 'secretsmanager', name: 'Secrets Manager', actions: ['secretsmanager:GetSecretValue', 'secretsmanager:CreateSecret', 'secretsmanager:DeleteSecret', 'secretsmanager:ListSecrets'],
    resourcePatterns: ['*', 'arn:cloudforge:secretsmanager:::secret:my-secret'],
  },
  {
    id: 'elasticache', name: 'ElastiCache', actions: ['elasticache:DescribeCacheClusters', 'elasticache:CreateCacheCluster', 'elasticache:DeleteCacheCluster'],
    resourcePatterns: ['*'],
  },
  {
    id: 'elasticloadbalancing', name: 'Elastic Load Balancing', actions: ['elasticloadbalancing:DescribeLoadBalancers', 'elasticloadbalancing:CreateLoadBalancer', 'elasticloadbalancing:DeleteLoadBalancer', 'elasticloadbalancing:RegisterTargets'],
    resourcePatterns: ['*'],
  },
  {
    id: 'ecs', name: 'ECS', actions: ['ecs:RunTask', 'ecs:DescribeTasks', 'ecs:ListTasks', 'ecs:CreateService', 'ecs:DescribeServices'],
    resourcePatterns: ['*'],
  },
  {
    id: 'apigateway', name: 'API Gateway', actions: ['apigateway:GET', 'apigateway:POST', 'apigateway:DELETE', 'apigateway:PATCH'],
    resourcePatterns: ['*', 'arn:cloudforge:apigateway:::apis/my-api'],
  },
  {
    id: 'events', name: 'EventBridge', actions: ['events:PutEvents', 'events:PutRule', 'events:DeleteRule', 'events:ListRules'],
    resourcePatterns: ['*', 'arn:cloudforge:events:::rule/my-rule'],
  },
  {
    id: 'cloudfront', name: 'CloudFront', actions: ['cloudfront:GetDistribution', 'cloudfront:CreateDistribution', 'cloudfront:ListDistributions', 'cloudfront:CreateInvalidation'],
    resourcePatterns: ['*'],
  },
  {
    id: 'sts', name: 'STS', actions: ['sts:AssumeRole', 'sts:GetCallerIdentity'],
    resourcePatterns: ['*'],
  },
  {
    id: 'autoscaling', name: 'Auto Scaling', actions: ['autoscaling:DescribeAutoScalingGroups', 'autoscaling:CreateAutoScalingGroup', 'autoscaling:UpdateAutoScalingGroup', 'autoscaling:DeleteAutoScalingGroup'],
    resourcePatterns: ['*'],
  },
  {
    id: 'backup', name: 'Backup', actions: ['backup:CreateBackupPlan', 'backup:StartBackupJob', 'backup:ListBackupJobs', 'backup:DescribeBackupJob'],
    resourcePatterns: ['*'],
  },
  {
    id: 'ce', name: 'Cost Explorer', actions: ['ce:GetCostAndUsage', 'ce:GetCostForecast'],
    resourcePatterns: ['*'],
  },
]

export function serviceById(id: string): ServiceActions | undefined {
  return POLICY_SERVICES.find((s) => s.id === id)
}

export const SAMPLE_VISUAL_STATEMENTS: VisualStatement[] = [
  { effect: 'Allow', service: 's3', actions: ['s3:ListBuckets'], customAction: '', resources: '*', username: '' },
  { effect: 'Deny', service: 's3', actions: ['s3:DeleteBucket'], customAction: '', resources: '*', username: '' },
]

export function blankStatement(): VisualStatement {
  return { effect: 'Allow', service: 's3', actions: [], customAction: '', resources: '*', username: '' }
}

function splitResources(raw: string): string[] {
  const parts = raw.split(',').map((p) => p.trim()).filter((p) => p !== '')
  return parts.length > 0 ? parts : ['*']
}

function singleOrList(values: string[]): string | string[] {
  return values.length === 1 ? values[0] : values
}

/** Build a policy document from visual statements. */
export function visualToDocument(statements: VisualStatement[]): Record<string, unknown> {
  return {
    Version: '2012-10-17',
    Statement: statements.map((st) => {
      const actions = [...new Set([...st.actions, st.customAction.trim()].filter((a) => a !== ''))]
      const stmt: Record<string, unknown> = {
        Effect: st.effect,
        Action: singleOrList(actions),
        Resource: singleOrList(splitResources(st.resources)),
      }
      if (st.username.trim() !== '') {
        stmt.Condition = { StringEquals: { 'aws:username': st.username.trim() } }
      }
      return stmt
    }),
  }
}

export function visualToJson(statements: VisualStatement[]): string {
  return JSON.stringify(visualToDocument(statements), null, 2)
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function asStringList(v: unknown): string[] | null {
  if (typeof v === 'string') return [v]
  if (Array.isArray(v) && v.every((x) => typeof x === 'string')) return [...(v as string[])]
  return null
}

export interface VisualParseResult {
  /** Populated visual statements, or null when the JSON is not representable. */
  statements: VisualStatement[] | null
  /** Human-readable reasons the JSON cannot be shown visually (empty when representable). */
  unsupported: string[]
  /** JSON syntax error, when the text is not valid JSON at all. */
  jsonError: string | null
}

/**
 * Parse a policy document (already-parsed JSON, or a JSON string) back
 * into visual statements. Returns unsupported reasons — never drops
 * content silently: when unsupported is non-empty, statements is null
 * and the caller's JSON text must be kept as-is.
 */
export function documentToVisual(doc: unknown): VisualParseResult {
  let parsed: unknown = doc
  if (typeof doc === 'string') {
    try {
      parsed = JSON.parse(doc)
    } catch (e) {
      return { statements: null, unsupported: [], jsonError: e instanceof Error ? e.message : 'Policy document is not valid JSON.' }
    }
  }
  const unsupported: string[] = []
  if (!isRecord(parsed)) {
    return { statements: null, unsupported: ['Policy document must be a JSON object.'], jsonError: null }
  }
  if (JSON.stringify(parsed).includes('${')) {
    unsupported.push('Policy variables (${...}) are not supported by the backend and cannot be shown in the visual editor.')
  }
  if (parsed.Version !== '2012-10-17' && parsed.Version !== '2008-10-17') {
    unsupported.push(`Version ${typeof parsed.Version === 'string' ? `"${parsed.Version}"` : 'is missing'} is not supported by the backend (must be 2012-10-17 or 2008-10-17).`)
  }
  const rawStatements = parsed.Statement
  if (!Array.isArray(rawStatements) || rawStatements.length === 0) {
    unsupported.push('Policy must contain at least one Statement.')
    return { statements: null, unsupported, jsonError: null }
  }
  const statements: VisualStatement[] = []
  rawStatements.forEach((raw, i) => {
    const n = i + 1
    if (!isRecord(raw)) {
      unsupported.push(`Statement ${n} is not a JSON object.`)
      return
    }
    for (const key of Object.keys(raw)) {
      if (key === 'Sid') {
        unsupported.push(`Statement ${n}: Sid is not shown in the visual editor.`)
      } else if (key === 'NotAction') {
        unsupported.push(`Statement ${n}: NotAction is not supported by the backend.`)
      } else if (key === 'NotResource') {
        unsupported.push(`Statement ${n}: NotResource is not supported by the backend.`)
      } else if (key === 'Principal') {
        unsupported.push(`Statement ${n}: Principal is not supported in identity policies (use a trust policy on a role).`)
      } else if (!['Effect', 'Action', 'Resource', 'Condition'].includes(key)) {
        unsupported.push(`Statement ${n}: field "${key}" is not supported by the visual editor.`)
      }
    }
    const effectRaw = typeof raw.Effect === 'string' ? raw.Effect.toLowerCase() : ''
    const effect: PolicyEffect | null = effectRaw === 'allow' ? 'Allow' : effectRaw === 'deny' ? 'Deny' : null
    if (effect === null) {
      unsupported.push(`Statement ${n}: Effect must be Allow or Deny.`)
    }
    const actions = asStringList(raw.Action)
    if (actions === null || actions.length === 0) {
      unsupported.push(`Statement ${n}: Action is required and must be a string or a list of strings.`)
    }
    const resources = asStringList(raw.Resource)
    if (resources === null || resources.length === 0) {
      unsupported.push(`Statement ${n}: Resource is required and must be a string or a list of strings.`)
    }
    let username = ''
    if (raw.Condition !== undefined) {
      if (!isRecord(raw.Condition)) {
        unsupported.push(`Statement ${n}: Condition must be an object.`)
      } else {
        for (const [op, conds] of Object.entries(raw.Condition)) {
          if (op.toLowerCase() !== 'stringequals') {
            unsupported.push(`Statement ${n}: condition operator "${op}" is not supported (only StringEquals).`)
            continue
          }
          if (!isRecord(conds)) {
            unsupported.push(`Statement ${n}: StringEquals condition must be an object.`)
            continue
          }
          for (const [key, value] of Object.entries(conds)) {
            if (key.toLowerCase() !== 'aws:username') {
              unsupported.push(`Statement ${n}: condition key "${key}" is not supported (only aws:username).`)
              continue
            }
            const values = asStringList(value)
            if (values === null || values.length === 0) {
              unsupported.push(`Statement ${n}: aws:username condition value must be a string.`)
            } else if (values.length > 1) {
              unsupported.push(`Statement ${n}: the visual editor supports a single aws:username value, not ${values.length}.`)
            } else {
              username = values[0]
            }
          }
        }
      }
    }
    if (effect === null || actions === null || actions.length === 0 || resources === null || resources.length === 0) {
      return
    }
    // Split actions by service prefix. A statement mixing services is
    // representable only when every action shares one prefix (or "*");
    // otherwise the visual editor cannot place it under one service.
    const prefixes = new Set(actions.map((a) => (a === '*' ? '*' : a.includes(':') ? a.split(':')[0].toLowerCase() : '')))
    if (prefixes.has('')) {
      unsupported.push(`Statement ${n}: action "${actions.find((a) => !a.includes(':') && a !== '*')}" has no service prefix.`)
      return
    }
    if (prefixes.size > 1) {
      unsupported.push(`Statement ${n}: actions span more than one service (${[...prefixes].join(', ')}) — split it into one statement per service to edit it visually.`)
      return
    }
    const prefix = [...prefixes][0]
    const picked: string[] = []
    let customAction = ''
    for (const a of actions) {
      if (a === '*' || a.toLowerCase() === `${prefix}:*`) {
        customAction = a === '*' ? '*' : `${prefix}:*`
      } else {
        // Known picker actions and real actions the picker does not
        // list (e.g. s3:GetBucketAcl) alike: kept in the action list,
        // shown as a chip in the picker, and preserved on save.
        picked.push(a)
      }
    }
    statements.push({
      effect,
      service: prefix,
      actions: picked,
      customAction,
      resources: resources.join(', '),
      username,
    })
  })
  if (unsupported.length > 0) return { statements: null, unsupported, jsonError: null }
  return { statements, unsupported: [], jsonError: null }
}

/** Client-side checks before the backend validates on create. */
export function visualValidationErrors(statements: VisualStatement[]): string[] {
  const errors: string[] = []
  if (statements.length === 0) errors.push('Add at least one statement.')
  statements.forEach((st, i) => {
    const n = i + 1
    const actions = [...st.actions, st.customAction.trim()].filter((a) => a !== '')
    if (actions.length === 0) errors.push(`Statement ${n}: pick at least one action or type a custom action.`)
    if (st.resources.trim() === '') errors.push(`Statement ${n}: Resource is required.`)
  })
  return errors
}

// --- Access-level action groups (survey 2026-11-22) -------------------------
//
// AWS groups a service's actions into List / Read / Write / Permissions
// management / Tagging with counts. CloudForge groups its own picker
// actions the same way, client-side, from the action verb — this is a
// display grouping only; the backend evaluates the actions themselves,
// not the group they sit in.

export type AccessLevel = 'List' | 'Read' | 'Write' | 'Permissions management' | 'Tagging'

/** Access level for one action, from its verb (CloudForge's grouping, not AWS's). */
export function accessLevelForAction(action: string): AccessLevel {
  const verb = action.includes(':') ? action.split(':')[1] : action
  if (/^(Tag|Untag)/.test(verb)) return 'Tagging'
  if (/Policy|Permission|Attach|Detach|PassRole|Acl$|Grant/.test(verb)) return 'Permissions management'
  if (/^(List|Describe)/.test(verb)) return 'List'
  if (/^(Get|Head|BatchGet|Check|Lookup|Select|Query|Scan)/.test(verb)) return 'Read'
  return 'Write'
}

export interface ActionGroup {
  level: AccessLevel
  actions: string[]
}

/** A service's picker actions grouped by access level, with counts. Empty groups are omitted. */
export function groupServiceActions(service: ServiceActions): ActionGroup[] {
  const levels: AccessLevel[] = ['List', 'Read', 'Write', 'Permissions management', 'Tagging']
  return levels
    .map((level) => ({ level, actions: service.actions.filter((a) => accessLevelForAction(a) === level) }))
    .filter((g) => g.actions.length > 0)
}

// --- Client-side policy linter (survey 2026-11-22) --------------------------
//
// AWS shows a Policy Linter panel (Security / Errors / Warnings /
// Suggestions). This is a basic client-side linter over the stored
// document grammar only: it cannot see AWS's managed-policy rules or
// the account, so it says what it checked and never claims an AWS
// lint result. The backend still validates on create.

export interface LintFinding {
  category: 'Security' | 'Errors' | 'Warnings' | 'Suggestions'
  message: string
}

export function lintPolicyDocument(doc: unknown): LintFinding[] {
  const findings: LintFinding[] = []
  if (!isRecord(doc)) {
    findings.push({ category: 'Errors', message: 'Policy document must be a JSON object.' })
    return findings
  }
  const rawStatements = doc.Statement
  if (!Array.isArray(rawStatements) || rawStatements.length === 0) {
    findings.push({ category: 'Errors', message: 'Policy must contain at least one Statement.' })
    return findings
  }
  rawStatements.forEach((raw, i) => {
    const n = i + 1
    if (!isRecord(raw)) {
      findings.push({ category: 'Errors', message: `Statement ${n} is not a JSON object.` })
      return
    }
    const actions = asStringList(raw.Action)
    const resources = asStringList(raw.Resource)
    if (actions === null || actions.length === 0) {
      findings.push({ category: 'Errors', message: `Statement ${n}: Action is required and must be a string or a list of strings.` })
    }
    if (resources === null || resources.length === 0) {
      findings.push({ category: 'Errors', message: `Statement ${n}: Resource is required and must be a string or a list of strings.` })
    }
    if (raw.Effect !== 'Allow' && raw.Effect !== 'Deny') {
      findings.push({ category: 'Errors', message: `Statement ${n}: Effect must be Allow or Deny.` })
    }
    if (actions === null || resources === null) return
    const allActions = actions.some((a) => a === '*' || a.endsWith(':*'))
    const allResources = resources.includes('*')
    const writeActions = actions.filter((a) => accessLevelForAction(a) === 'Write' || accessLevelForAction(a) === 'Permissions management')
    if (raw.Effect === 'Allow' && allActions && allResources) {
      findings.push({ category: 'Security', message: `Statement ${n}: allows all actions on all resources — an overly broad grant. Narrow the actions or the resources.` })
    } else if (raw.Effect === 'Allow' && allResources && writeActions.length > 0) {
      findings.push({ category: 'Warnings', message: `Statement ${n}: write actions (${writeActions.join(', ')}) apply to all resources (*). Consider specific ARNs.` })
    }
    if (raw.Effect === 'Allow' && allResources && actions.some((a) => /^(iam|kms|sts):/.test(a))) {
      findings.push({ category: 'Security', message: `Statement ${n}: identity/security actions (${actions.filter((a) => /^(iam|kms|sts):/.test(a)).join(', ')}) apply to all resources — scope them to specific ARNs.` })
    }
    if (allResources && actions.length > 0 && !allActions) {
      findings.push({ category: 'Suggestions', message: `Statement ${n}: replace Resource "*" with the specific ARN(s) this statement needs.` })
    }
  })
  return findings
}

// --- Permissions summary (review step + policy detail) -----------------------

export interface PolicySummaryRow {
  service: string
  accessLevel: string
  resource: string
  condition: string
}

/**
 * One summary row per statement, as the AWS Permissions summary shows
 * (Service / Access level / Resource / Request condition). Derived
 * from the stored document via the visual parse; unrepresentable
 * documents return representable: false instead of a guessed summary.
 */
export function summarizePolicyDocument(doc: unknown): { rows: PolicySummaryRow[]; representable: boolean } {
  const parsed = documentToVisual(doc)
  if (!parsed.statements) return { rows: [], representable: false }
  const rows = parsed.statements.map((st) => {
    const actions = [...st.actions, st.customAction.trim()].filter((a) => a !== '')
    const levels = [...new Set(actions.map(accessLevelForAction))]
    const all = actions.some((a) => a === '*' || a.endsWith(':*'))
    const accessLevel = all ? 'All' : levels.length > 1 ? `Limited: ${levels.join(', ')}` : (levels[0] ?? '—')
    const service = serviceById(st.service)?.name ?? st.service
    return {
      service,
      accessLevel,
      resource: st.resources.trim() === '' ? '—' : st.resources,
      condition: st.username.trim() !== '' ? `StringEquals aws:username = ${st.username.trim()}` : 'None',
    }
  })
  return { rows, representable: true }
}

// --- CloudForge managed policies (import source) -----------------------------
//
// AWS's Import policy dialog lists 1,622 AWS managed policies.
// CloudForge has no AWS managed-policy catalogue, so the import dialog
// offers this built-in list of CloudForge sample policies instead —
// real documents in the grammar the backend accepts, labelled as
// CloudForge samples, never as AWS managed policies.

export interface ManagedPolicy {
  name: string
  description: string
  document: Record<string, unknown>
}

export const CLOUDFORGE_MANAGED_POLICIES: ManagedPolicy[] = [
  {
    name: 'CloudForgeReadOnly',
    description: 'Read-only list/describe/get across S3, EC2, DynamoDB, Lambda and RDS.',
    document: {
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: ['s3:ListBuckets', 's3:ListBucket', 's3:GetObject'], Resource: '*' },
        { Effect: 'Allow', Action: ['ec2:DescribeInstances', 'ec2:DescribeVolumes'], Resource: '*' },
        { Effect: 'Allow', Action: ['dynamodb:DescribeTable', 'dynamodb:GetItem', 'dynamodb:Query'], Resource: '*' },
        { Effect: 'Allow', Action: ['lambda:ListFunctions', 'lambda:GetFunction'], Resource: '*' },
        { Effect: 'Allow', Action: 'rds:DescribeDBInstances', Resource: '*' },
      ],
    },
  },
  {
    name: 'CloudForgeS3ReadOnly',
    description: 'List buckets and read objects; no writes.',
    document: {
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: ['s3:ListBuckets', 's3:ListBucket', 's3:GetObject', 's3:GetBucketPolicy'], Resource: '*' },
      ],
    },
  },
  {
    name: 'CloudForgeLambdaInvoke',
    description: 'Invoke and inspect Lambda functions.',
    document: {
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: ['lambda:InvokeFunction', 'lambda:ListFunctions', 'lambda:GetFunction'], Resource: '*' },
      ],
    },
  },
  {
    name: 'CloudForgeMessagingSend',
    description: 'Send/receive SQS messages and publish SNS topics.',
    document: {
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: ['sqs:SendMessage', 'sqs:ReceiveMessage', 'sqs:ListQueues'], Resource: '*' },
        { Effect: 'Allow', Action: ['sns:Publish', 'sns:ListTopics'], Resource: '*' },
      ],
    },
  },
  {
    name: 'CloudForgeBillingReadOnly',
    description: 'Read cost and usage data only.',
    document: {
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: ['ce:GetCostAndUsage', 'ce:GetCostForecast'], Resource: '*' },
      ],
    },
  },
]
