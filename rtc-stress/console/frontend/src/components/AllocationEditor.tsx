import { useEffect, useRef, useState } from "react";
import {
  Avatar,
  Badge,
  Button,
  Col,
  Input,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  type GetRef,
  type TableColumnsType,
} from "antd";
import {
  allocationsFor,
  nodeColor,
  requiredUIDs,
  type Config,
  type Host,
  type NodeState,
  type RoleAllocation,
  type Plan,
} from "../types";
import { Field, Icon, NumericInput } from "./Controls";
import AllocationPlan from "./AllocationPlan";

export default function AllocationEditor({
  config,
  nodes,
  hosts,
  locked,
  onChange,
  onConfigChange,
  plan,
  checking,
  planError,
}: {
  config: Config;
  nodes: NodeState[];
  hosts: Host[];
  locked: boolean;
  onChange: (rows: RoleAllocation[]) => void;
  onConfigChange: (config: Partial<Config>) => void;
  plan: Plan | null;
  checking: boolean;
  planError: string;
}) {
  const rows = allocationsFor(config);
  const [focusRole, setFocusRole] = useState<string | null>(null),
    [feedback, setFeedback] = useState(""),
    [search, setSearch] = useState("");
  const tableRef = useRef<GetRef<typeof Table>>(null);
  useEffect(() => {
    if (!focusRole) return;
    tableRef.current?.scrollTo({ key: focusRole });
    const frame = requestAnimationFrame(() => {
      document.getElementById(`allocation-node-${focusRole}`)?.focus();
      setFocusRole(null);
    });
    return () => cancelAnimationFrame(frame);
  }, [focusRole, rows]);
  const patch = (role: string, value: Partial<RoleAllocation>) =>
    onChange(
      rows.map((row) => (row.role === role ? { ...row, ...value } : row)),
    );
  const processCountFor = (row: RoleAllocation, index: number) => {
    const weight =
      (row.publishersPerChannel || 0) + (row.receiversPerChannel || 0);
    const required = requiredUIDs(config, index);
    if (
      required <= 0 ||
      row.maxUidsPerProcess <= 0 ||
      weight <= 0 ||
      row.maxUidsPerProcess % weight !== 0 ||
      required % row.maxUidsPerProcess !== 0
    )
      return null;
    return required / row.maxUidsPerProcess;
  };
  const add = () => {
    let index = 0,
      role = "A";
    while (rows.some((row) => row.role === role)) {
      index++;
      role = index < 26 ? String.fromCharCode(65 + index) : `G${index + 1}`;
    }
    onChange([
      ...rows,
      {
        role,
        nodeId:
          hosts.find((host) => !rows.some((row) => row.nodeId === host.id))
            ?.id || "",
        publishingChannels: 0,
        publishersPerChannel: 0,
        receiversPerChannel: 1,
        joiningChannels: 0,
        processCount: 1,
        maxUidsPerProcess: 30,
      },
    ]);
    setSearch("");
    setFocusRole(role);
  };
  const remove = (role: string) => {
    const index = rows.findIndex((row) => row.role === role),
      next = rows.filter((row) => row.role !== role);
    onChange(next);
    setFocusRole(next[Math.min(index, next.length - 1)]?.role || null);
  };
  const select = (role: string, nodeId: string) => {
    const old = rows.find((row) => row.role === role)!;
    onChange(
      rows.map((row) =>
        row.role === role
          ? { ...row, nodeId }
          : nodeId && row.nodeId === nodeId
            ? { ...row, nodeId: old.nodeId }
            : row,
      ),
    );
  };
  const created = rows.reduce(
    (sum, row) => sum + (row.publishingChannels || 0),
    0,
  );
  const joined = rows.reduce((sum, row) => sum + (row.joiningChannels || 0), 0);
  const data = rows.map((row, index) => {
    const required = requiredUIDs(config, index),
      own = (row.publishersPerChannel || 0) + (row.receiversPerChannel || 0);
    return {
      ...row,
      index,
      required,
      minimum: processCountFor(row, index),
      node: nodes.find((node) => node.id === row.nodeId),
      fits:
        required > 0 &&
        own > 0 &&
        row.processCount * row.maxUidsPerProcess === required &&
        row.maxUidsPerProcess % own === 0,
    };
  });
  type Item = (typeof data)[number];
  const columns: TableColumnsType<Item> = [
    {
      title: "执行机器",
      key: "machine",
      fixed: "left",
      width: 220,
      render: (_, row) => (
        <div>
          <div className="allocation-machine">
            <Avatar
              shape="square"
              size={26}
              style={{
                background: nodeColor(row.nodeId) + "22",
                color: nodeColor(row.nodeId),
              }}
            >
              {row.role}
            </Avatar>
            <Select
              id={`allocation-node-${row.role}`}
              aria-label={`分组 ${row.role} 执行机器`}
              value={row.nodeId || undefined}
              placeholder="选择机器"
              disabled={locked}
              status={
                !row.nodeId ? "error" : row.node?.online ? undefined : "warning"
              }
              onChange={(nodeId) => select(row.role, nodeId)}
              options={hosts.map((host) => ({
                value: host.id,
                label: `${host.id} · ${nodes.find((node) => node.id === host.id)?.address || host.address}`,
              }))}
            />
          </div>
          <div className="allocation-node-state">
            <Badge
              status={row.node?.online ? "success" : "default"}
              text={
                row.node?.online ? "在线" : row.nodeId ? "离线" : "等待选择机器"
              }
            />
            <span className="secondary-text">
              {" "}
              · {row.required.toLocaleString("zh-CN")} UID
            </span>
          </div>
          {!row.fits && row.required > 0 && (
            <div className="allocation-capacity-issue">
              <Tag color="warning">分配不匹配</Tag>
              {row.minimum !== null && (
                <Button
                  type="link"
                  size="small"
                  disabled={locked}
                  onClick={() => {
                    if (row.minimum !== null)
                      patch(row.role, { processCount: row.minimum });
                  }}
                >
                  改为 {row.minimum} 个进程
                </Button>
              )}
            </div>
          )}
        </div>
      ),
    },
    {
      title: "创建频道个数",
      key: "created",
      width: 95,
      render: (_, row) => (
        <NumericInput
          aria-label={`${row.role} 创建频道个数`}
          min={0}
          max={config.channels}
          disabled={locked}
          value={row.publishingChannels || 0}
          onChange={(event) =>
            patch(row.role, {
              publishingChannels: +event.target.value,
              ...(+event.target.value > 0 &&
              !(row.publishersPerChannel || row.receiversPerChannel)
                ? { publishersPerChannel: 2, receiversPerChannel: 1 }
                : {}),
            })
          }
        />
      ),
    },
    {
      title: "频道内 UID 行为/个数",
      key: "ownerUsers",
      width: 150,
      render: (_, row) => (
        <div className="allocation-creator-users">
          <div>
            <label htmlFor={`owner-publish-${row.role}`}>发流</label>
            <NumericInput
              id={`owner-publish-${row.role}`}
              aria-label={`${row.role} 单频道内发流人数`}
              min={0}
              max={100}
              disabled={locked}
              value={row.publishersPerChannel || 0}
              onChange={(event) =>
                patch(row.role, { publishersPerChannel: +event.target.value })
              }
            />
          </div>
          <div>
            <label htmlFor={`owner-receive-${row.role}`}>收流</label>
            <NumericInput
              id={`owner-receive-${row.role}`}
              aria-label={`${row.role} 单频道内收流人数`}
              min={0}
              max={100}
              disabled={locked}
              value={row.receiversPerChannel || 0}
              onChange={(event) =>
                patch(row.role, { receiversPerChannel: +event.target.value })
              }
            />
          </div>
        </div>
      ),
    },
    {
      title: (
        <Tooltip title="从其它机器创建的频道号数组中加入。同一台机器内不重复，不同机器可以加入同一频道。">
          加入已有频道
        </Tooltip>
      ),
      key: "joined",
      width: 100,
      render: (_, row) => (
        <NumericInput
          aria-label={`${row.role} 加入已有频道个数`}
          min={0}
          max={config.channels - (row.publishingChannels || 0)}
          disabled={locked}
          value={row.joiningChannels || 0}
          onChange={(event) =>
            patch(row.role, { joiningChannels: +event.target.value })
          }
        />
      ),
    },
    {
      title: "进程数",
      key: "processes",
      width: 75,
      render: (_, row) => (
        <NumericInput
          aria-label={`${row.role} 进程数`}
          min={1}
          disabled={locked}
          value={row.processCount}
          onChange={(event) =>
            patch(row.role, { processCount: +event.target.value })
          }
        />
      ),
    },
    {
      title: "单进程分配 UID 数",
      key: "capacity",
      width: 135,
      render: (_, row) => (
        <NumericInput
          aria-label={`${row.role} 单进程分配 UID 数`}
          min={1}
          max={3000}
          disabled={locked}
          value={row.maxUidsPerProcess}
          onChange={(event) =>
            patch(row.role, { maxUidsPerProcess: +event.target.value })
          }
        />
      ),
    },
    {
      title: "",
      key: "remove",
      fixed: "right",
      width: 45,
      render: (_, row) => (
        <Tooltip title={`移除分组 ${row.role}`}>
          <Button
            type="text"
            danger
            aria-label={`移除执行分组 ${row.role}`}
            icon={<Icon name="trash" />}
            disabled={locked || rows.length === 1}
            onClick={() => remove(row.role)}
          />
        </Tooltip>
      ),
    },
  ];
  const visible = data.filter(
    (row) =>
      !search ||
      `${row.role} ${row.nodeId} ${row.node?.address || ""}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  const total = data.reduce((sum, row) => sum + row.required, 0);
  return (
    <section className="allocation-editor">
      <div className="pane-intro with-actions">
        <div>
          <h2>机器与发流配额</h2>
          <Typography.Text type="secondary">
            频道内发流、收流人数统一作用于本机创建和加入的频道。
          </Typography.Text>
        </div>
        <Button
          icon={<Icon name="plus" />}
          disabled={locked || rows.length >= 128}
          onClick={add}
        >
          添加机器
        </Button>
      </div>
      <Row gutter={16} className="allocation-targets">
        <Col span={8}>
          <Field label="总 PCU（UID 连接）">
            <NumericInput
              aria-label="目标总 PCU"
              min={1}
              disabled={locked}
              value={config.totalUids || 0}
              onChange={(event) =>
                onConfigChange({ totalUids: +event.target.value })
              }
            />
          </Field>
        </Col>
        <Col span={8}>
          <Field label="总频道数">
            <NumericInput
              aria-label="目标总频道数"
              min={1}
              max={10000}
              disabled={locked}
              value={config.channels}
              onChange={(event) =>
                onConfigChange({ channels: +event.target.value })
              }
            />
          </Field>
        </Col>
        <Col span={8}>
          <Field label="每频道 UID">
            <NumericInput
              aria-label="目标每频道 UID"
              min={1}
              max={300}
              disabled={locked}
              value={config.usersPerChannel || 0}
              onChange={(event) =>
                onConfigChange({ usersPerChannel: +event.target.value })
              }
            />
          </Field>
        </Col>
      </Row>
      <div className="table-toolbar">
        <Space wrap size={16}>
          <Badge
            status={created === config.channels ? "success" : "warning"}
            text={`创建 ${created} / ${config.channels}`}
          />
          <Badge
            status={
              plan?.coverage?.verified && !checking && !planError
                ? "success"
                : "default"
            }
            text={`加入已有频道 ${joined} 次`}
          />
          <Badge
            status={total === config.totalUids ? "success" : "warning"}
            text={`UID ${total.toLocaleString("zh-CN")} / ${(config.totalUids || 0).toLocaleString("zh-CN")}`}
          />
        </Space>
        <Space>
          {rows.length > 6 && (
            <Input.Search
              aria-label="搜索机器或分组"
              placeholder="搜索机器或分组"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              allowClear
              style={{ width: 180 }}
            />
          )}
          <Button
            disabled={locked}
            onClick={() => {
              let unmatched = 0;
              onChange(
                rows.map((row, index) => {
                  const count = processCountFor(row, index);
                  if (count === null) {
                    unmatched++;
                    return row;
                  }
                  return { ...row, processCount: count };
                }),
              );
              setFeedback(
                unmatched
                  ? `${unmatched} 组无法整除，请调整单进程分配 UID 数；可整除的组已更新。`
                  : "已按固定分配 UID 数更新进程数。",
              );
            }}
          >
            计算进程数
          </Button>
        </Space>
      </div>
      <Table<Item>
        className="allocation-table"
        ref={tableRef}
        rowKey="role"
        size="small"
        pagination={false}
        virtual={rows.length > 40}
        scroll={{ x: 900, y: 430 }}
        dataSource={visible}
        columns={columns}
        rowClassName={(row) =>
          !row.fits || !row.nodeId ? "allocation-needs-attention" : ""
        }
        summary={() => (
          <Table.Summary fixed>
            <Table.Summary.Row>
              <Table.Summary.Cell index={0}>
                <div className="allocation-summary-label">
                  合计 {rows.length} 台 · {total.toLocaleString("zh-CN")} UID
                </div>
              </Table.Summary.Cell>
              {[
                created,
                "—",
                joined,
                rows.reduce((sum, row) => sum + row.processCount, 0),
                "—",
              ].map((value, index) => (
                <Table.Summary.Cell index={index + 1} key={index} align="right">
                  <div className="allocation-summary-value">{value}</div>
                </Table.Summary.Cell>
              ))}
              <Table.Summary.Cell index={6} />
            </Table.Summary.Row>
          </Table.Summary>
        )}
      />
      {feedback && (
        <div className="allocation-feedback" role="status">
          <Icon name="check" size={14} />
          {feedback}
        </div>
      )}
      <AllocationPlan
        plan={plan}
        config={config}
        checking={checking}
        error={planError}
      />
    </section>
  );
}
