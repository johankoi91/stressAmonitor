import { useState } from "react";
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Card,
  Descriptions,
  Drawer,
  Input,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import HostTelemetry, { type HostTelemetryData } from "./HostTelemetry";
import { Icon } from "./Controls";
import { nodeColor, type Host, type NodeState } from "../types";
import { time } from "../format";
export default function MachinesPage({
  hosts,
  nodes,
  telemetry,
  minutes,
  onMinutesChange,
  busy,
  locked,
  dirty,
  output,
  onUpdate,
  onCheck,
  onReconnect,
  onSave,
  onRefresh,
}: {
  hosts: Host[];
  nodes: NodeState[];
  telemetry: HostTelemetryData;
  minutes: number;
  onMinutesChange: (minutes: number) => void;
  busy: string;
  locked: boolean;
  dirty: boolean;
  output: string;
  onUpdate: (index: number, value: Partial<Host>) => void;
  onCheck: (host: Host) => void;
  onReconnect: (host: Host) => void;
  onSave: () => void;
  onRefresh: () => void;
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const rows = hosts.map((host, index) => ({
    ...host,
    index,
    node: nodes.find((node) => node.id === host.id),
  }));
  const detail = rows.find((row) => row.id === selected);
  return (
    <div className="page-stack">
      <div className="cluster-context">
        <Space size={20}>
          <Badge
            status={nodes.every((node) => node.online) ? "success" : "warning"}
            text={`${nodes.filter((node) => node.online).length} / ${nodes.length} 节点在线`}
          />
        </Space>
        <Button
          icon={<Icon name="refresh" />}
          loading={busy === "refresh"}
          onClick={onRefresh}
        >
          刷新节点
        </Button>
      </div>
      <Tabs
        className="machine-page-tabs"
        defaultActiveKey="telemetry"
        items={[
          {
            key: "telemetry",
            label: (
              <Space>
                <Icon name="grid" />
                资源监控
              </Space>
            ),
            children: (
              <HostTelemetry
                data={telemetry}
                nodes={nodes}
                hosts={hosts}
                minutes={minutes}
                onMinutesChange={onMinutesChange}
              />
            ),
          },
          {
            key: "registry",
            label: (
              <Space>
                <Icon name="server" />
                节点管理<Tag>{hosts.length}</Tag>
              </Space>
            ),
            children: (
              <Card
                title="已发现的执行节点"
                extra={
                  <Button
                    type="primary"
                    icon={<Icon name="check" />}
                    loading={busy === "save"}
                    disabled={locked || !dirty}
                    onClick={onSave}
                  >
                    保存节点配置
                  </Button>
                }
              >
                <Table
                  rowKey="id"
                  dataSource={rows}
                  pagination={{
                    pageSize: 10,
                    hideOnSinglePage: true,
                    showSizeChanger: false,
                  }}
                  scroll={{ x: 1000 }}
                  columns={[
                    {
                      title: "节点",
                      key: "node",
                      width: 180,
                      render: (_, row) => (
                        <Space>
                          <Avatar
                            shape="square"
                            size={32}
                            style={{
                              color: nodeColor(row.id),
                              background: nodeColor(row.id) + "22",
                            }}
                          >
                            {row.id}
                          </Avatar>
                          <div>
                            <strong>节点 {row.id}</strong>
                            <div className="secondary-text small-text">
                              {row.id === "A"
                                ? "中心与执行"
                                : "WebSocket 执行端"}
                            </div>
                          </div>
                        </Space>
                      ),
                    },
                    {
                      title: "节点地址",
                      key: "address",
                      width: 225,
                      render: (_, row) => (
                        <Input
                          aria-label={`节点 ${row.id} 地址`}
                          value={row.address}
                          disabled={locked}
                          onChange={(event) =>
                            onUpdate(row.index, { address: event.target.value })
                          }
                        />
                      ),
                    },
                    {
                      title: "连接状态",
                      key: "status",
                      width: 110,
                      render: (_, row) => (
                        <Badge
                          status={row.node?.online ? "success" : "default"}
                          text={row.node?.online ? "在线" : "离线"}
                        />
                      ),
                    },
                    {
                      title: "版本",
                      key: "version",
                      width: 95,
                      render: (_, row) => row.node?.version || "—",
                    },
                    {
                      title: "最近心跳",
                      key: "heartbeat",
                      width: 110,
                      render: (_, row) =>
                        row.id === "A"
                          ? "中心本机"
                          : time(row.node?.lastSeenAt),
                    },
                    {
                      title: "执行状态",
                      key: "task",
                      width: 100,
                      render: (_, row) => (
                        <Tag
                          color={
                            row.node?.activeTaskId ? "processing" : "default"
                          }
                        >
                          {row.node?.activeTaskId ? "执行中" : "空闲"}
                        </Tag>
                      ),
                    },
                    {
                      title: "操作",
                      key: "actions",
                      width: 230,
                      render: (_, row) => (
                        <Space size={0}>
                          <Button
                            type="link"
                            size="small"
                            onClick={() => setSelected(row.id)}
                          >
                            详情
                          </Button>
                          <Button
                            type="link"
                            size="small"
                            disabled={locked || !row.node?.online}
                            loading={busy === row.id + "check"}
                            onClick={() => onCheck(row)}
                          >
                            环境检查
                          </Button>
                          {row.id !== "A" && (
                            <Button
                              type="link"
                              size="small"
                              disabled={!!busy || !row.node?.online}
                              loading={busy === row.id + "reconnect"}
                              onClick={() => onReconnect(row)}
                            >
                              重新连接
                            </Button>
                          )}
                        </Space>
                      ),
                    },
                  ]}
                />
                <Typography.Paragraph className="table-note" type="secondary">
                  节点地址用于识别机器；执行端的中心连接地址由该机器的 Agent
                  配置决定。新节点连接后会自动发现。
                </Typography.Paragraph>
              </Card>
            ),
          },
        ]}
      />
      {output && (
        <Card title="最近一次执行结果" size="small">
          <pre className="command-output">{output}</pre>
        </Card>
      )}
      <Drawer
        title={`节点 ${selected || ""} · 连接详情`}
        open={!!selected}
        size={600}
        onClose={() => setSelected(null)}
      >
        {detail && (
          <>
            <Space className="detail-tags">
              <Badge
                status={detail.node?.online ? "success" : "default"}
                text={detail.node?.online ? "在线" : "离线"}
              />
              <Tag>{detail.node?.version || "尚未连接"}</Tag>
            </Space>
            <Descriptions
              column={1}
              bordered
              size="small"
              items={[
                { key: "address", label: "节点地址", children: detail.address },
                {
                  key: "work",
                  label: "SDK 工作目录",
                  children: <code>{detail.workDir}</code>,
                },
                {
                  key: "mode",
                  label: "连接方式",
                  children:
                    detail.id === "A"
                      ? "中心本机直接执行"
                      : `ws://${hosts.find((host) => host.id === "A")?.address || "中心地址"}:8092/ws/agent`,
                },
                {
                  key: "seen",
                  label: "最近心跳",
                  children: time(detail.node?.lastSeenAt),
                },
                {
                  key: "reconnect",
                  label: "重连次数",
                  children: detail.node?.reconnects || 0,
                },
                {
                  key: "active",
                  label: "正在执行",
                  children: detail.node?.activeTaskId || "空闲",
                },
                {
                  key: "boot",
                  label: "Agent 实例",
                  children: detail.node?.bootId || "—",
                },
              ]}
            />
            {!detail.node?.online && (
              <Alert
                className="inner-alert"
                type="warning"
                showIcon
                title="节点尚未连接"
                description={
                  detail.node?.lastError ||
                  "请在该机器启动 Agent，连接后状态会自动更新。"
                }
              />
            )}
          </>
        )}
      </Drawer>
    </div>
  );
}
