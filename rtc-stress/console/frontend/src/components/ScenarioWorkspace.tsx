import { useState } from "react";
import {
  Alert,
  Badge,
  Button,
  Card,
  Col,
  Form,
  Input,
  Row,
  Select,
  Space,
  Tabs,
  Tag,
  Typography,
} from "antd";
import AllocationEditor from "./AllocationEditor";
import AudioLibrary from "./AudioLibrary";
import { Field, Icon, NumericInput } from "./Controls";
import {
  allocationsFor,
  requiredUIDs,
  type AudioFile,
  type Config,
  type Host,
  type NodeState,
  type Plan,
} from "../types";
type Section = "basic" | "machines" | "audio" | "advanced";
const outOfRange = (value: number, min: number, max: number) =>
  value < min || value > max ? `请输入 ${min}～${max}` : undefined;
export default function ScenarioWorkspace({
  config,
  hosts,
  nodes,
  audioFiles,
  plan,
  planError,
  checking,
  locked,
  dirty,
  saving,
  onChange,
  onSave,
  onUpload,
  uploading,
  progress,
}: {
  config: Config;
  hosts: Host[];
  nodes: NodeState[];
  audioFiles: AudioFile[];
  plan: Plan | null;
  planError: string;
  checking: boolean;
  locked: boolean;
  dirty: boolean;
  saving: boolean;
  onChange: (value: Partial<Config>) => void;
  onSave: () => void;
  onUpload: (index: number, file: File) => void;
  uploading: number | null;
  progress: number;
}) {
  const [section, setSection] = useState<Section>("basic");
  const rows = allocationsFor(config),
    processes = rows.reduce((s, row) => s + row.processCount, 0),
    connections = rows.reduce((s, _, i) => s + requiredUIDs(config, i), 0),
    publishing = rows.reduce((s, row) => s + (row.publishingChannels || 0), 0),
    ready = !checking && !planError && !!plan;
  const locateIssue = () => {
    const target: Section =
      /VOS|AUT|Service|SDK 音频模式|Service 布局|UID.*(冲突|基值)|接收者 UID.*范围|启动间隔|入频道超时/.test(
        planError,
      )
        ? "advanced"
        : /第二路|音频|Opus|采样|声道|音源/.test(planError)
          ? "audio"
          : /分组|容量|发流频道|创建频道|额外加入|加入已有|UID 分配|单进程|PCU|配额|先决条件|进程|节点|机器/.test(
                planError,
              )
            ? "machines"
            : "basic";
    setSection(target);
    window.scrollTo({ top: 0 });
  };
  return (
    <Form layout="vertical" component="div" className="scenario-workspace">
      <div className="scenario-name-row">
        <label htmlFor="scenario-name" className="secondary-text">
          场景名称
        </label>
        <Input
          id="scenario-name"
          aria-label="任务名称"
          size="small"
          value={config.name}
          disabled={locked}
          onChange={(event) => onChange({ name: event.target.value })}
        />
      </div>
      {locked && (
        <Alert
          type="info"
          showIcon
          title="任务执行期间配置已锁定，结束后可继续编辑。"
        />
      )}
      {planError && !checking && (
        <Alert
          type="warning"
          showIcon
          title={planError}
          action={
            <Button size="small" onClick={locateIssue}>
              定位问题
            </Button>
          }
        />
      )}
      <Card className="scenario-tabs-card">
        <Tabs
          activeKey={section}
          onChange={(key) => setSection(key as Section)}
          destroyOnHidden
          items={[
            {
              key: "basic",
              label: (
                <Space>
                  <Icon name="grid" />
                  基础配置
                </Space>
              ),
              children: (
                <div className="scenario-pane">
                  <div className="pane-intro">
                    <h2>任务规模与目标环境</h2>
                  </div>
                  <div className="form-group">
                    <div className="form-group-heading">
                      <h3>任务规模</h3>
                    </div>
                    <Row gutter={20}>
                      <Col span={5}>
                        <Field
                          label="运行时长（秒）"
                          required
                          error={outOfRange(config.durationSec, 1, 86400)}
                        >
                          <NumericInput
                            value={config.durationSec}
                            min={1}
                            max={86400}
                            disabled={locked}
                            onChange={(event) =>
                              onChange({ durationSec: +event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                      <Col span={14}>
                        <Field
                          label="频道前缀"
                          required
                          error={
                            !/^[a-zA-Z0-9_-]{1,40}$/.test(config.channelPrefix)
                              ? "限 1～40 位字母、数字、下划线和连字符"
                              : undefined
                          }
                        >
                          <Input
                            value={config.channelPrefix}
                            disabled={locked}
                            onChange={(event) =>
                              onChange({ channelPrefix: event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                      <Col span={5}>
                        <Field label="起始编号">
                          <NumericInput
                            aria-label="频道起始编号"
                            min={0}
                            value={config.channelStart}
                            disabled={locked}
                            onChange={(event) =>
                              onChange({ channelStart: +event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                    </Row>
                    <div className="form-range">
                      <Icon name="grid" size={14} />
                      <code>
                        {config.channelPrefix}_{config.channelStart} ～{" "}
                        {config.channelPrefix}_
                        {config.channelStart + Math.max(0, config.channels - 1)}
                      </code>
                      <span>· 时长从音频开始发送计时</span>
                    </div>
                  </div>
                  <div className="form-group">
                    <div className="form-group-heading">
                      <h3>目标 RTC 环境</h3>
                    </div>
                    <Field label="Token / AppId" hint="留空保存时保留原值。">
                      <Input
                        autoComplete="off"
                        spellCheck={false}
                        value={config.token}
                        disabled={locked}
                        onChange={(event) =>
                          onChange({ token: event.target.value })
                        }
                      />
                    </Field>
                    <Row gutter={20}>
                      <Col span={12}>
                        <Field label="Local AP">
                          <Input
                            spellCheck={false}
                            value={config.ap}
                            disabled={locked}
                            onChange={(event) =>
                              onChange({ ap: event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                      <Col span={12}>
                        <Field label="AP 证书域名">
                          <Input
                            spellCheck={false}
                            value={config.apCert}
                            disabled={locked}
                            onChange={(event) =>
                              onChange({ apCert: event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                    </Row>
                  </div>
                </div>
              ),
            },
            {
              key: "machines",
              label: (
                <Space>
                  <Icon name="server" />
                  机器与配额<Tag>{rows.length}</Tag>
                </Space>
              ),
              children: (
                <div className="scenario-pane">
                  <AllocationEditor
                    config={config}
                    nodes={nodes}
                    hosts={hosts}
                    locked={locked}
                    onChange={(allocations) => onChange({ allocations })}
                    onConfigChange={onChange}
                    plan={plan}
                    checking={checking}
                    planError={planError}
                  />
                </div>
              ),
            },
            {
              key: "audio",
              label: (
                <Space>
                  <Icon name="audio" />
                  音源与时序
                </Space>
              ),
              children: (
                <div className="scenario-pane">
                  <AudioLibrary
                    config={config}
                    files={audioFiles}
                    nodes={nodes}
                    hosts={hosts}
                    locked={locked}
                    onChange={onChange}
                    onUpload={onUpload}
                    uploading={uploading}
                    progress={progress}
                  />
                </div>
              ),
            },
            {
              key: "advanced",
              label: (
                <Space>
                  <Icon name="tune" />
                  高级参数
                </Space>
              ),
              children: (
                <div className="scenario-pane">
                  <div className="pane-intro">
                    <h2>SDK 与连接参数</h2>
                    <Typography.Text type="secondary">
                      调整运行模式、连接节奏及用户身份。
                    </Typography.Text>
                  </div>
                  <div className="form-group">
                    <div className="form-group-heading">
                      <h3>运行参数</h3>
                      <Tag>Go SDK 2.6.6</Tag>
                    </div>
                    <Row gutter={20}>
                      <Col span={12}>
                        <Field label="SDK 音频模式">
                          <Select
                            value={config.audioScenario || "chorus"}
                            disabled={locked}
                            onChange={(audioScenario) =>
                              onChange({ engine: "go-sdk", audioScenario })
                            }
                            options={[
                              { value: "standard", label: "标准音频" },
                              { value: "chorus", label: "超低延迟合唱" },
                            ]}
                          />
                        </Field>
                      </Col>
                      <Col span={12}>
                        <Field label="每进程 Service 布局">
                          <Select
                            value={config.serviceLayout || "single"}
                            disabled={locked}
                            onChange={(serviceLayout) =>
                              onChange({ engine: "go-sdk", serviceLayout })
                            }
                            options={[
                              {
                                value: "single",
                                label: "1 个 Service · 多个 Connection",
                              },
                              {
                                value: "multi",
                                label: "多个 Service（当前 SDK 不支持）",
                                disabled: true,
                              },
                            ]}
                          />
                        </Field>
                      </Col>
                      <Col span={12}>
                        <Field
                          label="入频道超时（秒）"
                          error={outOfRange(config.readyTimeoutSec, 10, 3600)}
                        >
                          <NumericInput
                            min={10}
                            max={3600}
                            disabled={locked}
                            value={config.readyTimeoutSec}
                            onChange={(event) =>
                              onChange({ readyTimeoutSec: +event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                      <Col span={12}>
                        <Field
                          label="进程启动间隔（毫秒）"
                          error={outOfRange(config.launchIntervalMs, 0, 10000)}
                        >
                          <NumericInput
                            min={0}
                            max={10000}
                            disabled={locked}
                            value={config.launchIntervalMs}
                            onChange={(event) =>
                              onChange({
                                launchIntervalMs: +event.target.value,
                              })
                            }
                          />
                        </Field>
                      </Col>
                    </Row>
                    <Typography.Text type="secondary" className="small-text">
                      接收用户仅统计编码音频，不解码或播放；合唱模式会增加发包与调度频率。
                    </Typography.Text>
                  </div>
                  <div className="form-group">
                    <div className="form-group-heading">
                      <h3>UID 身份</h3>
                      <Typography.Text type="secondary">
                        按频道与执行分组分配唯一 UID
                      </Typography.Text>
                    </div>
                    <Row gutter={20}>
                      {[
                        "音源 1 发送者起始 UID",
                        "音源 2 发送者起始 UID",
                        "接收者起始 UID",
                      ].map((label, index) => (
                        <Col span={8} key={label}>
                          <Field label={label}>
                            <NumericInput
                              min={1}
                              disabled={locked}
                              value={config.uids[index]}
                              onChange={(event) => {
                                const uids = [...config.uids] as Config["uids"];
                                uids[index] = +event.target.value;
                                onChange({ uids });
                              }}
                            />
                          </Field>
                        </Col>
                      ))}
                    </Row>
                    <Typography.Text type="secondary" className="small-text">
                      同频道发送者依次使用音源 1、2；同一音源的后续 UID
                      每次增加总频道数。完整对应关系见机器与配额中的进程明细。
                    </Typography.Text>
                  </div>
                  <div className="form-group">
                    <div className="form-group-heading">
                      <h3>传输选项</h3>
                      <Typography.Text type="secondary">
                        按目标环境需要填写
                      </Typography.Text>
                    </div>
                    <Row gutter={20}>
                      <Col span={6}>
                        <Field label="AUT">
                          <Select
                            disabled={locked}
                            value={config.useAut ? "1" : "0"}
                            onChange={(value) =>
                              onChange({ useAut: value === "1" })
                            }
                            options={[
                              { value: "0", label: "关闭" },
                              { value: "1", label: "开启" },
                            ]}
                          />
                        </Field>
                      </Col>
                      <Col span={10}>
                        <Field label="VOS IP（可选）">
                          <Input
                            disabled={locked}
                            value={config.vosIp}
                            onChange={(event) =>
                              onChange({ vosIp: event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                      <Col span={8}>
                        <Field label="VOS 端口（可选）">
                          <NumericInput
                            min={1}
                            max={65535}
                            disabled={locked}
                            value={config.vosPort || ""}
                            onChange={(event) =>
                              onChange({ vosPort: +event.target.value })
                            }
                          />
                        </Field>
                      </Col>
                    </Row>
                  </div>
                </div>
              ),
            },
          ]}
        />
      </Card>
      <Card className="scenario-savebar" size="small">
        <div className="scenario-savebar-content">
          <div className="scenario-validation-state" role="status">
            <Badge
              status={checking ? "processing" : ready ? "success" : "warning"}
            />
            <div>
              <strong>
                {checking
                  ? "正在校验配置"
                  : planError
                    ? "配置需要调整"
                    : ready
                      ? dirty
                        ? "配置已通过校验，尚未保存"
                        : "配置已保存"
                      : "正在生成任务计划"}
              </strong>
              <div className="secondary-text small-text">
                {rows.length} 台机器 · {processes} 个进程 ·{" "}
                {connections.toLocaleString("zh-CN")} 个 UID
              </div>
            </div>
            {planError && !checking && (
              <Button type="link" onClick={locateIssue}>
                定位配置问题
              </Button>
            )}
          </div>
          <Space size={20}>
            <Typography.Text
              type={publishing === config.channels ? "secondary" : "warning"}
            >
              {config.channels} 频道 · 创建配额 {publishing}/{config.channels}
            </Typography.Text>
            <Button
              type="primary"
              icon={<Icon name="check" />}
              loading={saving}
              disabled={locked || !dirty || !ready}
              onClick={onSave}
            >
              保存配置
            </Button>
          </Space>
        </div>
      </Card>
    </Form>
  );
}
