/**
 * Local service catalog — the list the AWS-console-style header search
 * and left nav are built from. These are CloudForge's own learning
 * services: each entry maps to a real local route (labs, courses,
 * console, …), never to an AWS service. The search box filters this
 * list; nothing here is fetched from AWS.
 */
import {
  Activity,
  BookOpen,
  Database,
  FlaskConical,
  HardDrive,
  Home,
  KeyRound,
  Layers,
  Lock,
  Network,
  Route,
  Scale,
  Server,
  Shield,
  Table2,
  Zap,
  type LucideIcon,
} from 'lucide-react'

/** Resolve a catalog icon key to a lucide component. */
export const SERVICE_ICONS: Record<string, LucideIcon> = {
  server: Server,
  zap: Zap,
  network: Network,
  layers: Layers,
  route: Route,
  shield: Shield,
  scale: Scale,
  database: Database,
  'hard-drive': HardDrive,
  table: Table2,
  key: KeyRound,
  lock: Lock,
  activity: Activity,
  book: BookOpen,
  flask: FlaskConical,
  home: Home,
}

export interface ConsoleService {
  /** Short name shown in nav tiles and search results, e.g. "EC2". */
  name: string
  /** Longer label, e.g. "Instances (Labs)". */
  label: string
  /** One-line description shown in search results. */
  description: string
  /** Local route to navigate to. */
  to: string
  /** Group shown in the left nav. */
  group: string
  /** lucide icon key (resolved by AwsNav / AwsHeader). */
  icon: string
  /** Keywords the header search matches against (lowercase). */
  keywords: string[]
}

export const NAV_GROUPS: { group: string; services: ConsoleService[] }[] = [
  {
    group: 'Compute',
    services: [
      {
        name: 'EC2',
        label: 'Instances (Labs)',
        description: 'Lab sessions as instances — launch, view state, open the workstation.',
        to: '/labs',
        group: 'Compute',
        icon: 'server',
        keywords: ['ec2', 'instance', 'instances', 'lab', 'labs', 'vm', 'virtual machine', 'compute'],
      },
      {
        name: 'Lambda',
        label: 'Lambda (Learn)',
        description: 'Serverless concepts — learn how functions run without servers.',
        to: '/learn',
        group: 'Compute',
        icon: 'zap',
        keywords: ['lambda', 'serverless', 'function', 'functions'],
      },
    ],
  },
  {
    group: 'Networking',
    services: [
      {
        name: 'VPC',
        label: 'VPC & Topology',
        description: 'Virtual networks and the topology editor for a lab.',
        to: '/console',
        group: 'Networking',
        icon: 'network',
        keywords: ['vpc', 'network', 'topology', 'subnet', 'subnets', 'route table', 'security group', 'load balancer'],
      },
      {
        name: 'Subnets',
        label: 'Subnets',
        description: 'Subnet layout inside a lab VPC — see the topology view.',
        to: '/console',
        group: 'Networking',
        icon: 'layers',
        keywords: ['subnet', 'subnets'],
      },
      {
        name: 'Route Tables',
        label: 'Route Tables',
        description: 'Routing inside a lab network — see the topology view.',
        to: '/console',
        group: 'Networking',
        icon: 'route',
        keywords: ['route', 'routes', 'route table', 'route tables', 'routing'],
      },
      {
        name: 'Security Groups',
        label: 'Security Groups',
        description: 'Firewall rules for lab resources — see the topology view.',
        to: '/console',
        group: 'Networking',
        icon: 'shield',
        keywords: ['security group', 'security groups', 'firewall', 'sg'],
      },
      {
        name: 'Load Balancers',
        label: 'Load Balancers',
        description: 'Load balancers inside a lab — see the topology view.',
        to: '/console',
        group: 'Networking',
        icon: 'scale',
        keywords: ['load balancer', 'load balancers', 'elb', 'alb'],
      },
    ],
  },
  {
    group: 'Storage',
    services: [
      {
        name: 'S3',
        label: 'S3 (Courses)',
        description: 'Object storage concepts — course material lives in the curriculum.',
        to: '/courses',
        group: 'Storage',
        icon: 'database',
        keywords: ['s3', 'storage', 'bucket', 'buckets', 'object storage'],
      },
      {
        name: 'EBS',
        label: 'EBS (Labs)',
        description: 'Block storage attached to lab instances.',
        to: '/labs',
        group: 'Storage',
        icon: 'hard-drive',
        keywords: ['ebs', 'volume', 'volumes', 'block storage', 'disk'],
      },
    ],
  },
  {
    group: 'Databases',
    services: [
      {
        name: 'RDS',
        label: 'RDS (Learn)',
        description: 'Relational databases — learn managed database concepts.',
        to: '/learn',
        group: 'Databases',
        icon: 'database',
        keywords: ['rds', 'database', 'databases', 'postgres', 'mysql', 'relational', 'sql'],
      },
      {
        name: 'DynamoDB',
        label: 'DynamoDB (Learn)',
        description: 'Key-value / NoSQL database concepts.',
        to: '/learn',
        group: 'Databases',
        icon: 'table',
        keywords: ['dynamodb', 'dynamo', 'nosql', 'key-value', 'database'],
      },
    ],
  },
  {
    group: 'Security',
    services: [
      {
        name: 'IAM',
        label: 'IAM (Learn)',
        description: 'Identity and access management — users, roles and policies.',
        to: '/learn',
        group: 'Security',
        icon: 'key',
        keywords: ['iam', 'identity', 'access', 'user', 'users', 'role', 'roles', 'policy', 'policies', 'permission'],
      },
      {
        name: 'KMS',
        label: 'KMS (Learn)',
        description: 'Key management and encryption concepts.',
        to: '/learn',
        group: 'Security',
        icon: 'lock',
        keywords: ['kms', 'key', 'keys', 'encryption', 'encrypt'],
      },
    ],
  },
  {
    group: 'Monitoring',
    services: [
      {
        name: 'CloudWatch',
        label: 'CloudWatch (Console Home)',
        description: 'Health and metrics for the local control plane.',
        to: '/',
        group: 'Monitoring',
        icon: 'activity',
        keywords: ['cloudwatch', 'monitor', 'monitoring', 'metrics', 'health', 'logs', 'alarm'],
      },
    ],
  },
  {
    group: 'Learn',
    services: [
      {
        name: 'Courses',
        label: 'Courses',
        description: 'The CloudForge curriculum — tracks, lessons and labs.',
        to: '/courses',
        group: 'Learn',
        icon: 'book',
        keywords: ['course', 'courses', 'curriculum', 'learn', 'lesson', 'lessons', 'training'],
      },
      {
        name: 'Labs',
        label: 'Labs',
        description: 'Hands-on lab sessions.',
        to: '/labs',
        group: 'Learn',
        icon: 'flask',
        keywords: ['lab', 'labs', 'hands-on', 'practice', 'challenge', 'challenges'],
      },
      {
        name: 'Console Home',
        label: 'Console Home',
        description: 'CloudForge home — health, recently visited services, getting started.',
        to: '/',
        group: 'Learn',
        icon: 'home',
        keywords: ['home', 'dashboard', 'console home', 'welcome'],
      },
    ],
  },
]

export const ALL_SERVICES: ConsoleService[] = NAV_GROUPS.flatMap((g) => g.services)

/** Regions available in this local deployment — no real AWS regions. */
export const LOCAL_REGIONS = [
  { id: 'cf-local-1', label: 'cf-local-1 (Local)' },
  { id: 'cf-local-2', label: 'cf-local-2 (Local standby)' },
] as const

export const DEFAULT_REGION = LOCAL_REGIONS[0].id

/** Case-insensitive substring search over name, label, description and keywords. */
export function searchServices(query: string): ConsoleService[] {
  const q = query.trim().toLowerCase()
  if (q === '') return []
  // Dedupe by (name, to): several catalog entries share a route.
  const seen = new Set<string>()
  return ALL_SERVICES.filter((s) => {
    const hay = [s.name, s.label, s.description, ...s.keywords].join(' ').toLowerCase()
    if (!hay.includes(q)) return false
    const key = `${s.name}|${s.to}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  }).slice(0, 8)
}

/** Recently visited services, stored in localStorage (max 5, most recent first). */
const RECENT_KEY = 'cloudforge_recent_services'

export function getRecentServices(): ConsoleService[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY)
    if (!raw) return []
    const names = JSON.parse(raw) as string[]
    if (!Array.isArray(names)) return []
    return names
      .map((n) => ALL_SERVICES.find((s) => s.name === n))
      .filter((s): s is ConsoleService => s !== undefined)
  } catch {
    return []
  }
}

export function recordRecentService(name: string): void {
  try {
    const current = getRecentServices().map((s) => s.name).filter((n) => n !== name)
    localStorage.setItem(RECENT_KEY, JSON.stringify([name, ...current].slice(0, 5)))
  } catch {
    // Recent-services history is best-effort; never break navigation.
  }
}
