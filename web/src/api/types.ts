// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
//
// JSON contract of the Brokerium REST API (/api/v1). Mirrors the Go types in internal/.

export type Role = 'admin' | 'operator' | 'viewer';

export interface User {
  id: string;
  username: string;
  role: Role;
  disabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export type SecurityProtocol = 'PLAINTEXT' | 'SSL' | 'SASL_PLAINTEXT' | 'SASL_SSL';
export type SaslMechanism = 'PLAIN' | 'SCRAM-SHA-256' | 'SCRAM-SHA-512' | 'AWS_MSK_IAM' | 'OAUTHBEARER';

export interface Security {
  protocol: SecurityProtocol;
  saslMechanism?: SaslMechanism | '';
  username?: string;
  tlsServerName?: string;
  tlsInsecureSkipVerify?: boolean;
  tlsCaCertPem?: string;
  awsRegion?: string;
}

/** Write-only: never returned by the API. */
export interface Secrets {
  password?: string;
  tlsClientCertPem?: string;
  tlsClientKeyPem?: string;
  awsAccessKeyId?: string;
  awsSecretAccessKey?: string;
  awsSessionToken?: string;
  oauthToken?: string;
  kubeconfig?: string;
}

export type Provider = 'external' | 'agent' | 'docker' | 'strimzi';
export type Flavor = 'apache' | 'msk' | 'confluent-cloud' | 'aiven';

export interface Cluster {
  id: string;
  name: string;
  provider: Provider;
  flavor: Flavor;
  bootstrapServers: string[];
  security: Security;
  labels: Record<string, string>;
  settings: Record<string, unknown>;
  kafkaClusterId: string;
  createdAt: string;
  updatedAt: string;
}

/** Body of POST /clusters, POST /clusters/test and PATCH /clusters/{id}. */
export interface ClusterRequest {
  name?: string;
  provider?: Provider;
  flavor?: Flavor;
  bootstrapServers?: string[];
  security?: Security;
  secrets?: Secrets;
  labels?: Record<string, string>;
  settings?: Record<string, unknown>;
  skipTest?: boolean;
}

export interface TestResult {
  ok: boolean;
  clusterId?: string;
  brokers: number;
  error?: string;
}

export type Severity = 'critical' | 'warning' | 'info';

export interface Issue {
  severity: Severity;
  code: string;
  message: string;
}

export type HealthStatus = 'ok' | 'warning' | 'critical' | 'unreachable' | 'unknown';

export interface Health {
  status: HealthStatus;
  issues: Issue[];
  brokers: number;
  brokersAlive: number;
  brokersFenced: number;
  brokersCordoned: number;
  topics: number;
  partitions: number;
  offlinePartitions: number;
  underReplicated: number;
  underMinIsr: number;
  atMinIsr: number;
  reassigning: number;
  quorumLeader: number;
  voters: number;
  votersLagging: number;
  diskUsedPercentMax: number;
  metadataVersion: string;
  kraftVersion: number;
}

export interface Capabilities {
  manageTopics: boolean;
  manageConfigs: boolean;
  manageAcls: boolean;
  manageScram: boolean;
  manageQuotas: boolean;
  manageGroups: boolean;
  browseMessages: boolean;
  reassign: boolean;
  electLeaders: boolean;
  cordon: boolean;
  unregisterBrokers: boolean;
  describeQuorum: boolean;
  removeVoters: boolean;
  addControllers: boolean;
  provisionNodes: boolean;
  restartNodes: boolean;
  upgradeVersion: boolean;
  finalizeFeatures: boolean;
  migrateQuorum: boolean;
  scaleNodePools: boolean;
  notes: string[];
}

export interface ClusterSummary {
  cluster: Cluster;
  health: Health;
  capabilities: Capabilities;
  error?: string;
  updatedAt: string;
}

export interface Endpoint {
  name: string;
  host: string;
  port: number;
}

export interface QuorumReplica {
  nodeId: number;
  directoryId: string;
  role: 'leader' | 'follower' | 'observer';
  logEndOffset: number;
  lag: number;
  lastFetchTimestamp: number;
  lastCaughtUpTimestamp: number;
  endpoints?: Endpoint[];
}

export interface Quorum {
  leaderId: number;
  leaderEpoch: number;
  highWatermark: number;
  voters: QuorumReplica[];
  observers: QuorumReplica[];
}

export interface LogDir {
  path: string;
  totalBytes: number;
  usableBytes: number;
  dataBytes: number;
  cordoned: boolean;
  error?: string;
}

export interface Broker {
  id: number;
  host: string;
  port: number;
  rack: string;
  fenced: boolean;
  alive: boolean;
  cordoned: boolean;
  logDirs: LogDir[] | null;
  diskTotalBytes: number;
  diskUsableBytes: number;
  dataBytes: number;
  replicas: number;
  leaders: number;
  preferredLeaders: number;
}

export interface Partition {
  id: number;
  leader: number;
  leaderEpoch: number;
  replicas: number[];
  isr: number[];
  offline?: number[] | null;
  sizeBytes: number;
  /** Replica size per broker id. */
  replicaBytes?: Record<string, number> | null;
  error?: string;
}

export interface Topic {
  name: string;
  id: string;
  internal: boolean;
  replicationFactor: number;
  minInsyncReplicas: number;
  configs: Record<string, string>;
  sizeBytes: number;
  partitions: Partition[];
  error?: string;
}

export interface Reassignment {
  topic: string;
  partition: number;
  replicas: number[];
  adding: number[] | null;
  removing: number[] | null;
}

/** Consumer groups (classic and KIP-848) or share groups (KIP-932). */
export type GroupType = 'consumer' | 'share';

export interface Group {
  id: string;
  type: GroupType;
  state: string;
  protocolType: string;
  members: number;
  lag: number;
  topics: string[] | null;
  error?: string;
}

export interface Feature {
  name: string;
  finalized: number;
  supportedMin: number;
  supportedMax: number;
}

export interface Snapshot {
  collectedAt: string;
  durationMs: number;
  clusterId: string;
  brokers: Broker[];
  quorum?: Quorum;
  features: Feature[];
  topics: Topic[];
  reassignments: Reassignment[];
  groups: Group[];
  errors?: string[];
}

/** GET /clusters/{id} */
export interface ClusterView {
  cluster: Cluster;
  snapshot?: Snapshot;
  health: Health;
  capabilities: Capabilities;
  error?: string;
  updatedAt: string;
}

export interface ConfigEntry {
  name: string;
  value: string;
  source: string;
  sensitive: boolean;
}

/** GET /clusters/{id}/topics/{topic} */
export interface TopicDetail {
  topic: Topic;
  configs: ConfigEntry[];
  offsets: Record<string, { start?: number; end?: number }>;
}

export interface GroupPartitionLag {
  topic: string;
  partition: number;
  committed: number;
  end: number;
  lag: number;
  member: string;
  clientId: string;
  host: string;
}

/** GET /clusters/{id}/groups/{group} */
export interface GroupDetail {
  group: string;
  type: GroupType;
  state: string;
  protocolType: string;
  members: number;
  totalLag: number;
  partitions: GroupPartitionLag[];
  error: string;
}

export interface RateEntry {
  bytesInPerSec: number;
  messagesInPerSec: number;
}

/** GET /clusters/{id}/rates — agentless estimates (ADR 0010). */
export interface Rates {
  sampledAt: string;
  windowSeconds: number;
  brokers: Record<string, RateEntry>;
  topics: Record<string, RateEntry>;
}

// ---------------------------------------------------------------- operations

export type OperationStatus = 'planned' | 'pending' | 'running' | 'paused' | 'succeeded' | 'failed' | 'cancelled';
export type StepStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped';

export interface Step {
  index: number;
  name: string;
  kind: string;
  args: unknown;
  alwaysRun: boolean;
  status: StepStatus;
  detail: unknown;
  error: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface PartitionMove {
  topic: string;
  partition: number;
  from: number[];
  to: number[];
  sizeBytes: number;
  replicaMoves: number;
  leaderChange: boolean;
}

export interface BrokerLoad {
  id: number;
  rack: string;
  replicas: number;
  leaders: number;
  dataBytes: number;
  bytesInPerSec: number;
  excluded: boolean;
}

export interface DistributionStats {
  brokers: BrokerLoad[];
  replicaSpread: number;
  leaderSpread: number;
  dataSpreadBytes: number;
  score: number;
}

export interface OperationPlan {
  summary: string;
  warnings: string[];
  irreversible: boolean;
  moves?: PartitionMove[];
  before?: DistributionStats;
  after?: DistributionStats;
  dataToMoveBytes?: number;
  details?: Record<string, unknown>;
}

export interface Operation {
  id: string;
  clusterId: string;
  type: string;
  status: OperationStatus;
  summary: string;
  params: Record<string, unknown>;
  plan: OperationPlan;
  irreversible: boolean;
  currentStep: number;
  requestedAction: string;
  error: string;
  createdBy: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  updatedAt: string;
  steps?: Step[];
}

/** GET /operation-types */
export interface OperationType {
  type: string;
  title: string;
  description: string;
  scope: 'cluster' | 'broker' | 'controller' | 'node';
  /** Capabilities that must all be true for the action to be offered. */
  requires: (keyof Capabilities)[];
  irreversible: boolean;
  /** Enterprise feature the license must cover; missing for Community operations. */
  feature?: string;
}

/**
 * Params per operation type (body of POST /clusters/{id}/operations: {type, params}).
 * The server always returns a 'planned' operation; POST /operations/{id}/confirm starts it.
 */
export interface OperationParams {
  rebalance: {
    mode?: 'even' | 'replicas' | 'leaders';
    excludeBrokers?: number[];
    topics?: string[];
    maxMoves?: number;
    throttleBytesPerSec?: number;
    batchSize?: number;
    rackAware?: boolean;
  };
  'move-partitions': {
    moves: { topic: string; partition: number; replicas: number[] }[];
    throttleBytesPerSec?: number;
    batchSize?: number;
  };
  'elect-leaders': { topics?: string[] };
  'cordon-broker': { brokerId: number; cordon: boolean };
  'drain-broker': { brokerId: number; decommission?: boolean; throttleBytesPerSec?: number };
  'unregister-broker': { brokerId: number };
  'add-broker': { count?: number; rack?: string; rebalance?: boolean };
  'replace-broker': { brokerId: number; mode?: 'new-id' | 'same-id' };
  'add-controller': Record<string, never>;
  'remove-controller': { nodeId: number };
  'migrate-quorum': Record<string, never>;
  'rolling-restart': { nodeIds?: number[] };
  'rolling-upgrade': { version: string };
  'finalize-upgrade': Record<string, never>;
  'bootstrap-cluster': {
    controllers?: number;
    brokers?: number;
    version?: string;
    racks?: string[];
    quorum?: 'dynamic' | 'static';
    portBase?: number;
  };
  'destroy-cluster': Record<string, never>;
  'scale-node-pool': { pool: string; replicas: number };
}

// ---------------------------------------------------------------- data plane

export type ByteEncoding = 'utf8' | 'base64';

export interface MessageHeader {
  key: string;
  value: string;
  /** Omitted for text. */
  encoding?: ByteEncoding;
}

export interface Message {
  partition: number;
  offset: number;
  timestamp: string;
  key: string | null;
  value: string | null;
  headers: MessageHeader[];
  keyEncoding: ByteEncoding;
  valueEncoding: ByteEncoding;
}

export interface Acl {
  resourceType: 'TOPIC' | 'GROUP' | 'CLUSTER' | 'TRANSACTIONAL_ID' | 'DELEGATION_TOKEN' | 'USER';
  resourceName: string;
  patternType: 'LITERAL' | 'PREFIXED';
  principal: string;
  host: string;
  operation: string;
  permission: 'ALLOW' | 'DENY';
}

export interface ScramUser {
  user: string;
  mechanisms: { mechanism: 'SCRAM-SHA-256' | 'SCRAM-SHA-512'; iterations: number }[];
}

export interface QuotaEntity {
  type: 'user' | 'client-id' | 'ip';
  /** null means the default entity. */
  name: string | null;
}

export interface QuotaEntry {
  entity: QuotaEntity[];
  values: Record<string, number>;
}

// ---------------------------------------------------------------- nodes and agents

export interface NodeStatus {
  running?: boolean;
  activeVersion?: string;
  installedVersions?: string[];
  meta?: { clusterId: string; nodeId: number; directoryId: string; formatted: boolean };
  dataBytes?: number;
  diskFreeBytes?: number;
  hostname?: string;
  pid?: number;
  lastExit?: string;
  processRoles?: string;
}

export interface AgentNode {
  id: string;
  name: string;
  clusterId: string | null;
  role: '' | 'broker' | 'controller';
  kafkaNodeId: number | null;
  hostname: string;
  agentVersion: string;
  /** insecure: signed messages behind a TLS-terminating proxy instead of a client certificate. */
  transport: 'mtls' | 'insecure';
  status: NodeStatus;
  labels: Record<string, string> | null;
  providerRef: string;
  lastSeenAt: string | null;
  createdAt: string;
  connected: boolean;
  /** Expiry of the current agent certificate; unknown until the agent connects. */
  certNotAfter: string | null;
  certificateState: CertificateState;
}

/** expiring: close to its expiry and not renewed; empty when the expiry is unknown. */
export type CertificateState = '' | 'ok' | 'expiring' | 'expired';

/** POST /join-tokens */
export interface JoinTokenResponse {
  token: string;
  expiresAt: string;
  command: string;
}

export interface AuditEntry {
  id: number;
  at: string;
  actor: string;
  action: string;
  clusterId: string;
  target: string;
  result: string;
  detail: unknown;
}

// ---------------------------------------------------------------- editions and licenses

export type Edition = 'community' | 'enterprise';

/** none: no license key. Enterprise features work in valid and grace. */
export type LicenseState = 'none' | 'invalid' | 'valid' | 'grace' | 'expired';

/** environment: BROKERIUM_LICENSE; file: BROKERIUM_LICENSE_FILE; database: added through the API. */
export type LicenseSource = 'environment' | 'file' | 'database';

/** A feature of Brokerium Enterprise. */
export interface EditionFeature {
  id: string;
  title: string;
  description: string;
  /** The license covers the feature and is valid or in its grace period. */
  licensed: boolean;
}

/** The license key in use. For a key that does not verify only state, source and error are set. */
export interface LicenseInfo {
  id: string;
  customer: string;
  email: string;
  /** Feature ids, or '*' for all features. */
  features: string[];
  issuedAt?: string;
  notBefore?: string;
  expiresAt?: string;
  graceEndsAt?: string;
  state: Exclude<LicenseState, 'none'>;
  source: LicenseSource;
  error?: string;
}

/** GET /license */
export interface LicenseView {
  edition: Edition;
  features: EditionFeature[];
  license: LicenseInfo | null;
}

export interface ApiErrorBody {
  error: { code: string; message: string };
}
