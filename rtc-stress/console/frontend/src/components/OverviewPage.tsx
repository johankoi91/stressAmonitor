import { useState } from "react";
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Card,
  Col,
  Drawer,
  Empty,
  Progress,
  Row,
  Select,
  Skeleton,
  Space,
  Statistic,
  Steps,
  Table,
  Tag,
  type TableColumnsType,
} from "antd";
import { Icon, StateTag } from "./Controls";
import ConfigurationSummary from "./ConfigurationSummary";
import { ProcessConnections } from "./AllocationPlan";
import {
  allocationsFor,
  nodeColor,
  requiredUIDs,
  terminal,
  channelPopulation,
  totalPublishers,
  type Config,
  type Host,
  type NodeState,
  type Plan,
  type Task,
} from "../types";
import { bytes, channelOrder, time } from "../format";
interface Props {
  config: Config;
  plan: Plan | null;
  task?: Task;
  taskLoading: boolean;
  taskError: string;
  onRetryTask: () => void;
  nodes: NodeState[];
  hosts: Host[];
  planError: string;
  dirty: boolean;
  busy: string;
  onStop: () => void;
  onEdit: () => void;
  onMachines: () => void;
}
export default function OverviewPage({
  config,
  plan,
  task,
  taskLoading,
  taskError,
  onRetryTask,
  nodes,
  hosts,
  planError,
  dirty,
  busy,
  onStop,
  onEdit,
  onMachines,
}: Props) {
  const [detailOpen, setDetailOpen] = useState(false),
    [group, setGroup] = useState("A");
  const metrics = task?.plan || plan,
    rows = allocationsFor(config),
    targetProcesses =
      metrics?.totalProcesses ?? rows.reduce((s, r) => s + r.processCount, 0),
    targetUIDs =
      metrics?.totalConnections ??
      rows.reduce((s, _, i) => s + requiredUIDs(config, i), 0);
  const selectedGroup = metrics?.hosts.some((h) => h.host === group)
    ? group
    : metrics?.hosts[0]?.host || rows[0]?.role || "A";
  const groupPlan = metrics?.hosts.find((h) => h.host === selectedGroup),
    groupState = task?.machines?.find((h) => h.id === selectedGroup);
  const firstAudio = [0, 1].map(
    (source) =>
      (task?.machines || [])
        .map((machine) => machine.firstAudio[source])
        .filter((at): at is string => !!at)
        .sort()[0],
  );
  const step = !task
    ? -1
    : task.state === "completed"
      ? 4
      : task.state === "running" || task.state === "stopping"
        ? 3
        : task.state === "arming"
          ? 2
          : task.state.startsWith("joining")
            ? 1
            : 0;
  const processData = (groupPlan?.processes || []).map((proc) => {
    const state = groupState?.processes.find((p) => p.id === proc.id),
      names = [...proc.channelNames].sort(channelOrder.compare);
    return {
      process: proc,
      id: proc.id,
      channels: names.length > 1 ? `${names[0]} ～ ${names.at(-1)}` : names[0],
      total: proc.connections.length,
      connected: state?.connected || 0,
      pid: state?.pid,
      services: state?.sdk?.services ?? proc.services,
      rss: state?.sdk?.rssBytes,
      state: state?.state || "pending",
    };
  });
  const processColumns: TableColumnsType<(typeof processData)[number]> = [
    {
      title: "进程",
      dataIndex: "id",
      width: 100,
      render: (v) => <code>{v}</code>,
    },
    { title: "频道范围", dataIndex: "channels", ellipsis: true },
    {
      title: "Connection / UID",
      dataIndex: "total",
      width: 140,
      align: "right",
    },
    {
      title: "当前入频道",
      width: 110,
      render: (_, r) => (task ? `${r.connected} / ${r.total}` : "—"),
    },
    { title: "PID", dataIndex: "pid", width: 85, render: (v) => v || "—" },
    {
      title: "Service",
      dataIndex: "services",
      width: 85,
      align: "right",
      render: (v) => v ?? "—",
    },
    {
      title: "RSS",
      dataIndex: "rss",
      width: 100,
      render: (v) => (v == null ? "—" : bytes(v)),
    },
    {
      title: "状态",
      dataIndex: "state",
      width: 95,
      render: (v) => <StateTag state={v} />,
    },
  ];
  const selectedMachinesOnline =
    rows.length > 0 &&
    rows.every((row) => nodes.find((node) => node.id === row.nodeId)?.online);
  const readinessText = selectedMachinesOnline
    ? "所选执行机器均在线"
    : "部分所选执行机器尚未连接";
  const toolbar = dirty && (
    <div className="overview-toolbar">
      <Tag color="warning">有未保存修改</Tag>
    </div>
  );
  if (!task && (taskLoading || taskError))
    return (
      <div className="page-stack">
        {toolbar}
        <Card>
          {taskError ? (
            <Alert
              type="error"
              showIcon
              title="任务详情加载失败"
              description={taskError}
              action={<Button onClick={onRetryTask}>重试</Button>}
            />
          ) : (
            <Skeleton active={taskLoading} paragraph={{ rows: 7 }} />
          )}
        </Card>
      </div>
    );
  return (
    <div className="page-stack">
      {toolbar}
      {!task && planError && (
        <Alert
          type="warning"
          showIcon
          title={planError}
          action={
            <Button size="small" onClick={onEdit}>
              检查配置
            </Button>
          }
        />
      )}
      <Row gutter={16} className="metric-row">
        {[
          {
            label: "目标频道",
            value: config.channels,
            unit: "个",
          },
          {
            label: "UID 连接",
            value: targetUIDs,
            unit: "个",
            hint: `每频道 ${channelPopulation(config)} 人`,
          },
          {
            label: "执行进程",
            value: targetProcesses,
            unit: "个",
          },
          {
            label: "音频发送者",
            value: metrics?.audioPublishers ?? totalPublishers(config),
            unit: "人",
          },
        ].map((item) => (
          <Col span={6} key={item.label}>
            <Card className="metric-card">
              <Statistic
                title={item.label}
                value={item.value}
                suffix={<span className="stat-unit">{item.unit}</span>}
              />
              {item.hint && <div className="metric-caption">{item.hint}</div>}
            </Card>
          </Col>
        ))}
      </Row>
      <Card
        className="run-card"
        title={
          <Space>
            <span>{task?.name || config.name}</span>
            {task ? (
              <StateTag state={task.state} />
            ) : (
              <Tag color="blue">待启动</Tag>
            )}
          </Space>
        }
        extra={
          <Space size={4} wrap>
            <Button
              type="text"
              icon={<Icon name="tune" />}
              onClick={() => setDetailOpen(true)}
            >
              配置详情
            </Button>
            <Button
              type="link"
              onClick={onMachines}
              icon={
                <Badge
                  status={selectedMachinesOnline ? "success" : "warning"}
                />
              }
            >
              {readinessText}
            </Button>
          </Space>
        }
      >
        <div className="run-card-top">
          <Space wrap>
            <span className="secondary-text">
              计划运行 {config.durationSec} 秒
            </span>
          </Space>
          {task && (
            <Space>
              <Button
                danger
                icon={<Icon name="stop" />}
                disabled={terminal(task.state) || !!busy}
                loading={busy === "stop"}
                onClick={onStop}
              >
                停止任务
              </Button>
            </Space>
          )}
        </div>
        <Steps
          size="small"
          current={step}
          status={
            task?.state === "failed" || task?.state === "interrupted"
              ? "error"
              : undefined
          }
          items={[
            { title: "环境检查" },
            { title: "加入频道" },
            { title: "同步就绪" },
            { title: "发送音频" },
            { title: "任务结束" },
          ]}
        />
        {task?.error && (
          <Alert
            className="inner-alert"
            type="error"
            showIcon
            title={task.error}
          />
        )}
      </Card>
      <div className="section-title">
        <h2>执行分布</h2>
      </div>
      <div className="execution-grid">
        {rows.map((row, index) => {
          const hp = metrics?.hosts.find((h) => h.host === row.role),
            state = task?.machines?.find((m) => m.id === row.role),
            nodeId = hp?.nodeId || row.nodeId,
            node = nodes.find((n) => n.id === nodeId),
            finished = !!task && terminal(task.state),
            joined = finished ? state?.joined || 0 : state?.connected || 0,
            total = hp?.connections ?? requiredUIDs(config, index),
            pubs =
              hp?.audioPublishers ??
              (row.publishingChannels || 0) * (row.publishersPerChannel ?? 2);
          return (
            <Card
              key={row.role}
              className="execution-card"
              title={
                <Space>
                  <Avatar
                    shape="square"
                    size={32}
                    style={{
                      background: nodeColor(nodeId) + "22",
                      color: nodeColor(nodeId),
                    }}
                  >
                    {nodeId}
                  </Avatar>
                  <div>
                    <div className="card-address">
                      {hp?.nodeAddress ||
                        node?.address ||
                        hosts.find((h) => h.id === nodeId)?.address ||
                        "尚未选择机器"}
                    </div>
                  </div>
                </Space>
              }
              extra={
                <Badge
                  status={node?.online ? "success" : "default"}
                  text={node?.online ? "在线" : "离线"}
                />
              }
            >
              <div className="execution-labels">
                <Tag color={pubs > 0 ? "blue" : "default"}>
                  {pubs > 0 ? "发布与接收" : "仅接收"}
                </Tag>
              </div>
              <Statistic
                title={
                  task
                    ? finished
                      ? "本机已入频道 UID"
                      : "本机入频道 UID"
                    : "计划 UID 连接"
                }
                value={task ? joined : total}
                suffix={
                  task ? (
                    <span className="connection-denominator">
                      / {total} UID
                    </span>
                  ) : (
                    <span className="stat-unit">UID</span>
                  )
                }
              />
              {task && (
                <Progress
                  percent={Math.min(
                    100,
                    Math.round((100 * joined) / Math.max(1, total)),
                  )}
                  showInfo={false}
                  strokeColor={nodeColor(nodeId)}
                  size="small"
                />
              )}
              <div className="execution-facts">
                <div>
                  <span>执行进程</span>
                  <strong>{hp?.processes.length ?? row.processCount}</strong>
                </div>
                <div>
                  <span>单进程分配 UID 数</span>
                  <strong>
                    {hp?.maxUidsPerProcess ?? row.maxUidsPerProcess}
                  </strong>
                </div>
                <div>
                  <span>创建频道</span>
                  <strong>
                    {hp?.publishingChannels ?? row.publishingChannels ?? 0}
                  </strong>
                </div>
              </div>
              {task && (
                <div className="execution-traffic">
                  <span>发送 {bytes(state?.audioTx || 0)}</span>
                  <span>接收 {bytes(state?.audioRx || 0)}</span>
                </div>
              )}
            </Card>
          );
        })}
      </div>
      <Card
        title="音频发送时序"
        extra={<Tag>第二路延迟 {config.schedule.offsetMs / 1000} 秒</Tag>}
        className="schedule-card"
      >
        <div className="overview-schedule">
          {[0, 1].map((index) => (
            <div className="overview-source" key={index}>
              <div>
                <span className={`source-marker source-marker-${index}`}>
                  0{index + 1}
                </span>
                <div>
                  <strong>
                    {config.audioLabels?.[index] || config.audioFiles[index]}
                  </strong>
                  <span>
                    {index === 0
                      ? "全部就绪后开始"
                      : `第一路开始后延迟 ${config.schedule.offsetMs / 1000} 秒`}{" "}
                    · {time(firstAudio[index])}
                  </span>
                </div>
              </div>
              <div className="overview-source-track">
                {index === 1 && (
                  <span className="track-delay">
                    +{config.schedule.offsetMs / 1000}s
                  </span>
                )}
                <span
                  className={index === 0 ? "track-fill" : "track-fill second"}
                >
                  持续发送
                </span>
              </div>
            </div>
          ))}
        </div>
      </Card>
      <Card
        className="process-details-card"
        title={
          <Space>
            <Icon name="server" />
            进程承载
          </Space>
        }
      >
        <div className="table-toolbar">
          <span className="secondary-text">
            {groupPlan?.processes.length || 0} 个进程 ·{" "}
            {groupPlan?.connections || 0} 个 UID
          </span>
          <Select
            aria-label="选择执行机器"
            value={selectedGroup}
            style={{ width: 170 }}
            onChange={setGroup}
            options={rows.map((r) => ({
              value: r.role,
              label: `机器 ${r.nodeId}`,
            }))}
          />
        </div>
        <Table
          rowKey="id"
          columns={processColumns}
          dataSource={processData}
          size="small"
          pagination={{
            pageSize: 10,
            showSizeChanger: false,
            hideOnSinglePage: true,
          }}
          scroll={{ x: 850 }}
          expandable={{
            expandedRowRender: (row) => (
              <ProcessConnections process={row.process} config={config} />
            ),
            columnTitle: "展开",
            columnWidth: 65,
          }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="完善场景配置后生成进程计划"
              />
            ),
          }}
        />
      </Card>
      <Drawer
        title={
          <Space>
            <Icon name="tune" />
            配置详情
          </Space>
        }
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
        size={820}
      >
        <ConfigurationSummary
          config={config}
          plan={metrics}
          nodes={nodes}
          hosts={hosts}
          record={!!task}
          dirty={dirty}
        />
      </Drawer>
    </div>
  );
}
