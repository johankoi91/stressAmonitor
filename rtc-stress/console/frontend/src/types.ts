export type HostID = string;
export interface Host {
  id: HostID;
  address: string;
  workDir: string;
}
export interface NodeState {
  id: HostID;
  online: boolean;
  mode: "local" | "websocket";
  version: string;
  bootId?: string;
  connectedAt?: string;
  lastSeenAt?: string;
  activeTaskId?: string;
  address?: string;
  workDir?: string;
  reconnects: number;
  lastError?: string;
}
export interface RoleAllocation {
  joiningChannels?: number;
  receiversPerJoinedChannel?: number;
  publishingChannels?: number;
  publishersPerChannel?: number;
  receiversPerChannel?: number;
  role: HostID;
  nodeId: HostID;
  processCount: number;
  maxUidsPerProcess: number;
}
export interface AudioFile {
  codec?: "pcm" | "opus";
  frameDurationMs?: number;
  bitrate?: number;
  id: string;
  name: string;
  size: number;
  sha256: string;
  sampleRate: number;
  channels: number;
  durationSec: number;
  createdAt: string;
  extension: string;
}
export interface Config {
  allocationPolicy?: "coverage" | "membership";
  totalUids?: number;
  usersPerChannel?: number;
  audioInputMode?: "pcm" | "opus";
  audioScenario?: "chorus" | "standard";
  distribution?: "centralized" | "distributed";
  publisherChannels?: [number, number, number];
  engine?: "native" | "go-sdk";
  serviceLayout?: "single" | "multi";
  connectionsPerService?: number;
  allocations?: RoleAllocation[];
  audioLabels?: [string, string];
  name: string;
  channels: number;
  channelPrefix: string;
  channelStart: number;
  channelsPerProcessA: number;
  usersPerProcessBC: number;
  uids: [number, number, number, number, number];
  token: string;
  ap: string;
  apCert: string;
  vosIp: string;
  vosPort: number;
  useAut: boolean;
  audioFiles: [string, string];
  sampleRate: number;
  audioChannels: number;
  durationSec: number;
  readyTimeoutSec: number;
  launchIntervalMs: number;
  schedule: {
    mode: "staggered" | "alternating";
    offsetMs: number;
    windowMs: number;
    gapMs: number;
  };
}
export interface Settings {
  config: Config;
  hosts: Host[];
}
export interface Connection {
  channel: string;
  uid: number;
  role: number;
  audioSource: number;
}
export interface Process {
  services?: number;
  id: string;
  host: HostID;
  channelNames: string[];
  connections: Connection[];
}
export interface HostPlan {
  joiningChannels?: number;
  receiversPerJoinedChannel?: number;
  publishersPerChannel?: number;
  receiversPerChannel?: number;
  publishingChannels?: number;
  audioPublishers?: number;
  nodeId?: HostID;
  nodeAddress?: string;
  maxUidsPerProcess?: number;
  host: HostID;
  channels: number;
  connections: number;
  processes: Process[];
}
export interface ChannelMember {
  role: string;
  node: string;
  publishers: number;
  receivers: number;
  ownsChannel: boolean;
}
export interface ChannelAssignment {
  members?: ChannelMember[];
  channel: string;
  creatorRole: string;
  creatorNode: string;
  receiverRole?: string;
  receiverNode?: string;
  publishers: number;
  localReceivers: number;
  joinedReceivers: number;
  totalUids: number;
}
export interface Plan {
  channelAssignments?: ChannelAssignment[];
  coverage?: {
    channels: number;
    extraChannels: number;
    joinMemberships?: number;
    totalUids: number;
    verified: boolean;
  };
  hosts: HostPlan[];
  totalProcesses: number;
  totalConnections: number;
  audioPublishers: number;
}
export interface Machine {
  nodeId?: HostID;
  nodeAddress?: string;
  id: HostID;
  expected: number;
  connected: number;
  joined: number;
  processes: {
    id: string;
    sdk?: {
      services: number;
      connections: number;
      rssBytes: number;
      goHeapBytes: number;
      threads: number;
      activeStreams: number;
      receivedStreams: number;
      expectedStreams: number;
      encodedReceived: number;
      packets: number;
      sendErrors: number;
      decodeEnabled: boolean;
    };
    pid: number;
    state: string;
    expected: number;
    connected: number;
  }[];
  firstAudio: [string | null, string | null];
  audioBytes: [number, number];
  audioTx: number;
  audioRx: number;
}
export interface Task {
  id: string;
  name: string;
  state: string;
  error?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  config: Config;
  plan: Plan;
  machines: Machine[];
}
export const terminal = (state: string) =>
  ["completed", "failed", "stopped", "interrupted"].includes(state);
export const states: Record<string, string> = {
  preflight: "环境检查",
  joining_creators: "创建端入频道",
  joining_existing: "加入已有频道",
  joining_all: "全部分组入频道",
  joining_a: "A 入频道",
  joining_bc: "B/C 入频道",
  arming: "准备音频",
  running: "运行中",
  stopping: "停止中",
  completed: "已完成",
  stopped: "已停止",
  failed: "失败",
  interrupted: "已中断",
  pending: "待启动",
  exited: "已退出",
};

function legacyPublisherCounts(config: Config): number[] {
  if (config.distribution !== "distributed") return [config.channels, 0, 0];
  if (config.publisherChannels?.some((n) => n !== 0))
    return config.publisherChannels;
  const a = Math.floor(config.channels * 0.3),
    b = Math.ceil((config.channels - a) / 2);
  return [a, b, config.channels - a - b];
}
export function allocationsFor(config: Config): RoleAllocation[] {
  const a = config.channelsPerProcessA || 15,
    b = config.usersPerProcessBC || 15;
  const rows = config.allocations?.length
    ? config.allocations
    : [
        {
          role: "A",
          nodeId: "A",
          processCount: Math.ceil(config.channels / a),
          maxUidsPerProcess: a * 3,
        },
        {
          role: "B",
          nodeId: "B",
          processCount: Math.ceil(config.channels / b),
          maxUidsPerProcess: b,
        },
        {
          role: "C",
          nodeId: "C",
          processCount: Math.ceil(config.channels / b),
          maxUidsPerProcess: b,
        },
      ];
  if (
    (config.allocationPolicy === "coverage" ||
      config.allocationPolicy === "membership") &&
    !config.allocations?.length
  )
    return [];
  const counts = legacyPublisherCounts(config);
  return rows.map((row) => ({
    ...row,
    publishingChannels:
      row.publishingChannels ?? counts[["A", "B", "C"].indexOf(row.role)] ?? 0,
    publishersPerChannel:
      row.publishersPerChannel ??
      ((row.publishingChannels ??
        counts[["A", "B", "C"].indexOf(row.role)] ??
        0) > 0
        ? 2
        : 0),
    receiversPerChannel: row.receiversPerChannel ?? 1,
    ...(config.allocationPolicy === "coverage" ||
    config.allocationPolicy === "membership"
      ? {
          joiningChannels: row.joiningChannels ?? 0,
          receiversPerJoinedChannel: row.receiversPerJoinedChannel ?? 0,
        }
      : {}),
  }));
}
// Legacy settings are converted to a visible draft. Saving is an explicit user
// action; no channel topology is changed silently on the server.
export function prepareMembership(config: Config): Config {
  const old = allocationsFor(config);
  if (config.allocationPolicy === "membership")
    return {
      ...config,
      allocations: old.map((row) => ({
        ...row,
        receiversPerJoinedChannel: undefined,
      })),
    };
  const wasCoverage = config.allocationPolicy === "coverage";
  const receivers = old.reduce(
    (sum, row) => sum + (row.receiversPerChannel || 0),
    0,
  );
  const users = wasCoverage
    ? config.usersPerChannel || 5
    : Math.max(
        1,
        ...old
          .filter((row) => (row.publishingChannels || 0) > 0)
          .map((row) => (row.publishersPerChannel || 0) + receivers),
      );
  return {
    ...config,
    allocationPolicy: "membership",
    totalUids: wasCoverage
      ? config.totalUids || config.channels * users
      : config.channels * users,
    usersPerChannel: users,
    distribution: undefined,
    publisherChannels: undefined,
    allocations: old.map((row) => {
      const owned = row.publishingChannels || 0;
      return {
        ...row,
        publishersPerChannel: owned > 0 ? row.publishersPerChannel || 0 : 0,
        receiversPerChannel:
          wasCoverage && owned === 0
            ? row.receiversPerJoinedChannel || 0
            : row.receiversPerChannel || 0,
        joiningChannels: wasCoverage
          ? row.joiningChannels || 0
          : (row.receiversPerChannel || 0) > 0
            ? config.channels - owned
            : 0,
        receiversPerJoinedChannel: undefined,
      };
    }),
  };
}
export function publisherCountsFor(config: Config): number[] {
  return allocationsFor(config).map((row) => row.publishingChannels || 0);
}
export function requiredUIDs(config: Config, index: number): number {
  const row = allocationsFor(config)[index];
  if (config.allocationPolicy === "membership")
    return row
      ? ((row.publishingChannels || 0) + (row.joiningChannels || 0)) *
          ((row.publishersPerChannel || 0) + (row.receiversPerChannel || 0))
      : 0;
  if (config.allocationPolicy === "coverage")
    return row
      ? (row.publishingChannels || 0) *
          ((row.publishersPerChannel || 0) + (row.receiversPerChannel || 0)) +
          (row.joiningChannels || 0) * (row.receiversPerJoinedChannel || 0)
      : 0;
  return row
    ? config.channels * (row.receiversPerChannel || 0) +
        (row.publishersPerChannel ?? 2) * (row.publishingChannels || 0)
    : 0;
}
export function usersPerChannel(config: Config): number {
  return (
    Math.max(
      0,
      ...allocationsFor(config)
        .filter((row) => (row.publishingChannels || 0) > 0)
        .map((row) => row.publishersPerChannel ?? 2),
    ) +
    allocationsFor(config).reduce(
      (total, row) => total + (row.receiversPerChannel || 0),
      0,
    )
  );
}
export function channelPopulation(config: Config): string {
  if (
    config.allocationPolicy === "coverage" ||
    config.allocationPolicy === "membership"
  )
    return String(config.usersPerChannel || 0);
  const rows = allocationsFor(config),
    receivers = rows.reduce((n, r) => n + (r.receiversPerChannel || 0), 0);
  const counts = rows
    .filter((r) => (r.publishingChannels || 0) > 0)
    .map((r) => (r.publishersPerChannel ?? 2) + receivers);
  if (!counts.length) return String(receivers);
  const min = Math.min(...counts),
    max = Math.max(...counts);
  return min === max ? String(min) : `${min}～${max}`;
}
export function totalPublishers(config: Config): number {
  return allocationsFor(config).reduce(
    (n, r) =>
      n +
      ((r.publishingChannels || 0) +
        (config.allocationPolicy === "membership"
          ? r.joiningChannels || 0
          : 0)) *
        (r.publishersPerChannel ?? 2),
    0,
  );
}
export function flexible(config: Config): boolean {
  return !!config.allocations?.some(
    (row) => row.receiversPerChannel != null || row.publishingChannels != null,
  );
}
export function nodeColor(id: string): string {
  const palette = [
    "#5ee6ba",
    "#79b8ff",
    "#d2a8ff",
    "#f6cc85",
    "#ff9eac",
    "#6ddbe0",
    "#b5d685",
    "#e8ad7f",
  ];
  const known = ["A", "B", "C"].indexOf(id);
  const hash = [...id].reduce(
    (n, char) => (n * 31 + char.charCodeAt(0)) >>> 0,
    0,
  );
  return palette[known >= 0 ? known : hash % palette.length];
}
export function nodeDash(id: string): string | undefined {
  const hash = [...id].reduce((n, char) => n + char.charCodeAt(0), 0);
  const pattern = ["A", "B", "C"].includes(id)
    ? id.charCodeAt(0) - 65
    : hash % 3;
  return [undefined, "7 3", "2 3"][pattern];
}
