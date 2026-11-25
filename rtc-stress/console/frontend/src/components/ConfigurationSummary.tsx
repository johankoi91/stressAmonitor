import { Descriptions, Space, Table, Tag } from "antd";
import {
  allocationsFor,
  requiredUIDs,
  type Config,
  type Host,
  type NodeState,
  type Plan,
} from "../types";
export default function ConfigurationSummary({
  config,
  plan,
  nodes,
  hosts,
  record,
  dirty,
}: {
  config: Config;
  plan?: Plan | null;
  nodes: NodeState[];
  hosts: Host[];
  record: boolean;
  dirty: boolean;
}) {
  const rows = allocationsFor(config);
  return (
    <div className="configuration-detail">
      <Space wrap className="detail-tags">
        <Tag color={dirty ? "warning" : "blue"}>
          {record ? "任务启动快照" : dirty ? "当前编辑配置" : "已保存配置"}
        </Tag>
        <Tag>{config.engine === "go-sdk" ? "Go SDK 2.6.6" : "Native"}</Tag>
        <Tag>
          {config.audioInputMode === "opus"
            ? "Opus 编码帧直发"
            : "PCM 音频输入"}
        </Tag>
      </Space>
      <Descriptions
        column={2}
        size="small"
        bordered
        items={[
          { key: "name", label: "场景名称", children: config.name, span: 2 },
          {
            key: "channel",
            label: "频道范围",
            children: (
              <code>
                {config.channelPrefix}_{config.channelStart} ～{" "}
                {config.channelStart + config.channels - 1}
              </code>
            ),
            span: 2,
          },
          {
            key: "duration",
            label: "运行时长",
            children: `${config.durationSec} 秒`,
          },
          {
            key: "offset",
            label: "第二路延迟",
            children: `${config.schedule.offsetMs / 1000} 秒`,
          },
          {
            key: "format",
            label: "音频格式",
            children: `${config.sampleRate / 1000} kHz / ${config.audioChannels} 声道`,
          },
          {
            key: "mode",
            label: "SDK 音频模式",
            children:
              config.audioScenario === "standard" ? "标准音频" : "超低延迟合唱",
          },
          {
            key: "layout",
            label: "Service 布局",
            children:
              config.serviceLayout === "multi"
                ? "多个 Service"
                : "每进程 1 个 Service",
          },
          { key: "receive", label: "音频接收", children: "仅接收编码数据" },
          {
            key: "source1",
            label: "第一路音频",
            children: config.audioLabels?.[0] || config.audioFiles[0],
            span: 2,
          },
          {
            key: "source2",
            label: "第二路音频",
            children: config.audioLabels?.[1] || config.audioFiles[1],
            span: 2,
          },
          { key: "ap", label: "Local AP", children: config.ap },
          { key: "cert", label: "AP 证书域名", children: config.apCert },
          ...(!record
            ? [
                {
                  key: "token",
                  label: "Token / AppId",
                  children: (
                    <code className="break-anywhere">
                      {config.token || "尚未填写"}
                    </code>
                  ),
                  span: 2,
                },
              ]
            : []),
        ]}
      />
      <h3 className="detail-subheading">执行机器与承载</h3>
      <Table
        rowKey="role"
        size="small"
        pagination={false}
        scroll={{ x: 950 }}
        dataSource={rows.map((row, index) => {
          const host = plan?.hosts.find((h) => h.host === row.role);
          return {
            ...row,
            address:
              host?.nodeAddress ||
              nodes.find((n) => n.id === (host?.nodeId || row.nodeId))
                ?.address ||
              hosts.find((h) => h.id === row.nodeId)?.address ||
              row.nodeId,
            total: host?.connections ?? requiredUIDs(config, index),
          };
        })}
        columns={[
          { title: "分组", dataIndex: "role", width: 65 },
          { title: "执行机器", dataIndex: "address" },
          {
            title: "创建频道个数",
            dataIndex: "publishingChannels",
            width: 100,
          },
          {
            title: "单频道内发流人数",
            dataIndex: "publishersPerChannel",
            width: 110,
          },
          { title: "加入已有频道", dataIndex: "joiningChannels", width: 100 },
          {
            title: "频道内收流 UID",
            dataIndex: "receiversPerChannel",
            width: 110,
          },
          { title: "进程", dataIndex: "processCount", width: 65 },
          {
            title: "单进程分配 UID 数",
            dataIndex: "maxUidsPerProcess",
            width: 130,
          },
          { title: "总 UID", dataIndex: "total", width: 90 },
        ]}
      />
    </div>
  );
}
