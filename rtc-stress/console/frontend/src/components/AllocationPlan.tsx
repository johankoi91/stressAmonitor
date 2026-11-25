import { useState } from "react";
import {
  Alert,
  Card,
  Checkbox,
  Empty,
  Select,
  Skeleton,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import type { Config, Connection, Plan, Process } from "../types";
import { channelOrder } from "../format";
import { allocationsFor } from "../types";

export function ProcessConnections({
  process,
  config,
}: {
  process: Process;
  config: Config;
}) {
  return (
    <div className="connection-mapping">
      <Typography.Paragraph type="secondary">
        一个 Connection 对应一个 UID
        加入一个频道。发流用户同时订阅，收流用户只接收编码音频。
      </Typography.Paragraph>
      <Table<Connection & { index: number }>
        rowKey="index"
        size="small"
        dataSource={process.connections.map((connection, index) => ({
          ...connection,
          index: index + 1,
        }))}
        pagination={{
          pageSize: 10,
          hideOnSinglePage: true,
          showSizeChanger: false,
        }}
        scroll={{ x: 720 }}
        columns={[
          {
            title: "Connection",
            dataIndex: "index",
            width: 110,
            render: (n) => `#${n}`,
          },
          {
            title: "Service 实例",
            width: 115,
            render: () => (config.engine === "go-sdk" ? "#1" : "—"),
          },
          {
            title: "频道",
            dataIndex: "channel",
            render: (name) => <code className="break-anywhere">{name}</code>,
          },
          { title: "UID", dataIndex: "uid", width: 125 },
          {
            title: "角色",
            width: 120,
            render: (_, c) => (
              <Tag color={c.audioSource >= 0 ? "cyan" : "default"}>
                {c.audioSource >= 0 ? "发流并订阅" : "仅收流"}
              </Tag>
            ),
          },
          {
            title: "音源",
            width: 180,
            render: (_, c) =>
              c.audioSource >= 0 ? (
                <span
                  title={
                    config.audioLabels?.[c.audioSource] ||
                    config.audioFiles[c.audioSource]
                  }
                >
                  音源 {c.audioSource + 1} ·{" "}
                  {config.audioLabels?.[c.audioSource] ||
                    config.audioFiles[c.audioSource]}
                </span>
              ) : (
                "—"
              ),
          },
        ]}
      />
    </div>
  );
}

export default function AllocationPlan({
  plan,
  config,
  checking,
  error,
}: {
  plan: Plan | null;
  config: Config;
  checking: boolean;
  error: string;
}) {
  const [selected, setSelected] = useState("");
  const [view, setView] = useState("processes");
  const [allChannels, setAllChannels] = useState(false);
  const rows = allocationsFor(config);
  const selectedRole = rows.some((row) => row.role === selected)
    ? selected
    : rows[0]?.role;
  const current = plan?.hosts.find((host) => host.host === selectedRole);
  return (
    <section className="allocation-plan" aria-label="进程明细">
      <Card size="small" className="allocation-processes">
        <div className="table-toolbar">
          <Space size={12}>
            <h2>进程明细</h2>
            <Tag
              color={checking ? "processing" : error ? "warning" : "success"}
            >
              {checking
                ? "正在计算"
                : error
                  ? "待调整配置"
                  : plan?.coverage?.verified
                    ? "覆盖已校验"
                    : "当前配置"}
            </Tag>
          </Space>
          <Space>
            <span className="secondary-text">机器 / 分组</span>
            <Select
              aria-label="进程明细机器与分组"
              value={selectedRole}
              onChange={setSelected}
              style={{ width: 230 }}
              showSearch
              optionFilterProp="label"
              options={rows.map((row) => ({
                value: row.role,
                label: `机器 ${row.nodeId || "待选择"} · 分组 ${row.role}`,
              }))}
            />
          </Space>
        </div>
        {checking ? (
          <Skeleton active paragraph={{ rows: 4 }} />
        ) : error ? (
          <Alert
            type="warning"
            showIcon
            title="调整配置后生成进程明细"
            description={error}
          />
        ) : !current ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="填写机器配额后生成进程计划"
          />
        ) : (
          <>
            <Tabs
              activeKey={view}
              onChange={setView}
              items={[
                {
                  key: "processes",
                  label: "进程与 UID",
                  children: (
                    <Table<Process>
                      key={current.host}
                      rowKey="id"
                      size="small"
                      dataSource={current.processes}
                      scroll={{ x: 800 }}
                      pagination={{
                        pageSize: 8,
                        hideOnSinglePage: true,
                        showSizeChanger: false,
                      }}
                      expandable={{
                        expandedRowRender: (process) => (
                          <ProcessConnections
                            process={process}
                            config={config}
                          />
                        ),
                        columnTitle: "展开",
                        columnWidth: 65,
                      }}
                      columns={[
                        {
                          title: "进程",
                          dataIndex: "id",
                          width: 100,
                          render: (id) => <code>{id}</code>,
                        },
                        {
                          title: "Service 实例",
                          dataIndex: "services",
                          width: 115,
                          render: (n) =>
                            config.engine === "go-sdk" ? n || 1 : "—",
                        },
                        {
                          title: "单个 Service 的 Connection 数",
                          width: 210,
                          render: (_, p) =>
                            config.engine === "go-sdk"
                              ? p.connections.length
                              : "—",
                        },
                        {
                          title: "总 UID 数",
                          width: 110,
                          render: (_, p) => p.connections.length,
                        },
                        {
                          title: "频道对应",
                          render: (_, p) => {
                            const names = [...p.channelNames].sort(
                              channelOrder.compare,
                            );
                            return (
                              <div>
                                <strong>{names.length} 个频道</strong>
                                <div className="secondary-text small-text break-anywhere">
                                  {names.length <= 2
                                    ? names.join("、")
                                    : `${names[0]} … ${names.at(-1)}（展开查看全部）`}
                                </div>
                              </div>
                            );
                          },
                        },
                      ]}
                    />
                  ),
                },
                ...(plan?.channelAssignments
                  ? [
                      {
                        key: "channels",
                        label: "频道分配清单",
                        children: (
                          <>
                            <div className="table-toolbar">
                              <Typography.Text type="secondary">
                                已校验 {plan.coverage?.channels}{" "}
                                个频道及每机加入名单，每频道{" "}
                                {config.usersPerChannel} UID
                              </Typography.Text>
                              <Checkbox
                                checked={allChannels}
                                onChange={(event) =>
                                  setAllChannels(event.target.checked)
                                }
                              >
                                显示全部频道
                              </Checkbox>
                            </div>
                            <Table
                              rowKey="channel"
                              size="small"
                              scroll={{ x: 850 }}
                              pagination={{
                                pageSize: 10,
                                showSizeChanger: false,
                                hideOnSinglePage: true,
                              }}
                              dataSource={plan.channelAssignments.filter(
                                (entry) =>
                                  allChannels ||
                                  entry.members?.some(
                                    (member) => member.role === current.host,
                                  ) ||
                                  entry.creatorRole === current.host ||
                                  entry.receiverRole === current.host,
                              )}
                              columns={[
                                {
                                  title: "频道名",
                                  dataIndex: "channel",
                                  render: (name) => (
                                    <code className="break-anywhere">
                                      {name}
                                    </code>
                                  ),
                                },
                                {
                                  title: "创建机器",
                                  dataIndex: "creatorNode",
                                  width: 100,
                                },
                                {
                                  title: "加入已有频道的机器",
                                  width: 180,
                                  render: (_, entry) =>
                                    entry.members
                                      ?.filter((member) => !member.ownsChannel)
                                      .map((member) => member.node)
                                      .join("、") ||
                                    entry.receiverNode ||
                                    "—",
                                },
                                {
                                  title: "频道内 UID 行为/个数",
                                  width: 210,
                                  render: (_, entry) =>
                                    entry.members
                                      ? entry.members.map((member) => (
                                          <div key={member.role}>
                                            {member.node} · 发流{" "}
                                            {member.publishers} / 收流{" "}
                                            {member.receivers}
                                          </div>
                                        ))
                                      : `${entry.publishers} 发 / ${entry.localReceivers + entry.joinedReceivers} 收`,
                                },
                                {
                                  title: "合计 UID",
                                  dataIndex: "totalUids",
                                  width: 95,
                                },
                              ]}
                            />
                          </>
                        ),
                      },
                    ]
                  : []),
              ]}
            />
            <Typography.Paragraph
              type="secondary"
              className="allocation-plan-note"
            >
              每进程 1 个 Service；每个 Connection 对应一个 UID
              加入一个频道。展开进程可查看完整对应关系。
            </Typography.Paragraph>
          </>
        )}
      </Card>
    </section>
  );
}
