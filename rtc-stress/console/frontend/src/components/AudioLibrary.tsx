import {
  Avatar,
  Button,
  Card,
  Col,
  Progress,
  Row,
  Select,
  Space,
  Tag,
  Typography,
  Upload,
} from "antd";
import {
  allocationsFor,
  type AudioFile,
  type Config,
  type Host,
  type NodeState,
} from "../types";
import { Field, Icon, NumericInput } from "./Controls";
export default function AudioLibrary({
  config,
  files,
  nodes,
  hosts,
  locked,
  onChange,
  onUpload,
  uploading,
  progress,
}: {
  config: Config;
  files: AudioFile[];
  nodes: NodeState[];
  hosts: Host[];
  locked: boolean;
  onChange: (value: Partial<Config>) => void;
  onUpload: (index: number, file: File) => void;
  uploading: number | null;
  progress: number;
}) {
  const choices = [
    { value: "builtin:440", label: "内置 440 Hz 测试音" },
    { value: "builtin:660", label: "内置 660 Hz 测试音" },
    ...files.map((file) => ({ value: "asset:" + file.id, label: file.name })),
  ];
  const select = (index: number, ref: string) => {
    const audioFiles = [...config.audioFiles] as [string, string];
    audioFiles[index] = ref;
    const file = files.find((file) => "asset:" + file.id === ref);
    const audioLabels = audioFiles.map(
      (value) =>
        files.find((file) => "asset:" + file.id === value)?.name ||
        choices.find((choice) => choice.value === value)?.label ||
        value,
    ) as [string, string];
    onChange({
      audioFiles,
      audioLabels,
      audioInputMode: file?.codec === "opus" ? "opus" : "pcm",
      ...(file?.codec === "opus" ? { engine: "go-sdk" as const } : {}),
      ...(file
        ? { sampleRate: file.sampleRate, audioChannels: file.channels }
        : {}),
    });
  };
  const senders = allocationsFor(config).filter(
    (row) =>
      (row.publishingChannels || 0) +
        (config.allocationPolicy === "membership"
          ? row.joiningChannels || 0
          : 0) >
        0 && (row.publishersPerChannel ?? 2) > 0,
  );
  return (
    <section className="scene-audio">
      <div className="pane-intro with-actions">
        <div>
          <h2>音源与发送时序</h2>
          <Typography.Text type="secondary">
            两路素材须不同，上传后自动选中并同步到发流端。
          </Typography.Text>
        </div>
        <Tag color="cyan">
          {config.audioInputMode === "opus" ? "Opus · 20 ms/帧" : "PCM 音频"}
        </Tag>
      </div>
      <Row gutter={20}>
        {[0, 1].map((index) => {
          const ref = config.audioFiles[index],
            file = files.find((file) => "asset:" + file.id === ref),
            color = index === 0 ? "#69d7ad" : "#81b7ff";
          return (
            <Col span={12} key={index}>
              <Card
                className="source-card"
                title={
                  <Space>
                    <Avatar
                      shape="square"
                      size={32}
                      style={{ background: color + "22", color }}
                    >
                      0{index + 1}
                    </Avatar>
                    {index === 0 ? "第一路音频" : "第二路音频"}
                  </Space>
                }
                extra={
                  <Tag color={file?.codec === "opus" ? "cyan" : "default"}>
                    {file?.codec === "opus" ? "OPUS" : "PCM"}
                  </Tag>
                }
              >
                <Typography.Paragraph type="secondary" className="small-text">
                  {index === 0
                    ? "全部就绪后立即发送"
                    : `延迟 ${config.schedule.offsetMs / 1000} 秒后发送`}
                </Typography.Paragraph>
                <Field label={`音频库 · ${index === 0 ? "第一路" : "第二路"}`}>
                  <Select
                    disabled={locked}
                    value={ref}
                    onChange={(value) => select(index, value)}
                    showSearch={{ optionFilterProp: "label" }}
                    options={
                      !choices.some((choice) => choice.value === ref)
                        ? [
                            {
                              value: ref,
                              label: config.audioLabels?.[index] || ref,
                            },
                            ...choices,
                          ]
                        : choices
                    }
                  />
                </Field>
                <Space wrap size={[6, 8]} className="source-metadata">
                  {file ? (
                    <>
                      <Tag>{file.durationSec.toFixed(1)} 秒</Tag>
                      <Tag>{file.sampleRate / 1000} kHz</Tag>
                      <Tag>{file.channels === 1 ? "单声道" : "双声道"}</Tag>
                      {file.codec === "opus" && (
                        <Tag>
                          {((file.bitrate || 0) / 1000).toFixed(0)} kbps
                        </Tag>
                      )}
                      <Tag>{(file.size / 1048576).toFixed(2)} MiB</Tag>
                    </>
                  ) : (
                    <Tag>内置或已有音频 · 循环发送</Tag>
                  )}
                </Space>
                <div className="source-upload">
                  <Upload
                    accept=".wav,.pcm,.opus,.ogg,audio/wav,audio/ogg"
                    disabled={locked}
                    showUploadList={false}
                    beforeUpload={(file) => {
                      onUpload(index, file);
                      return Upload.LIST_IGNORE;
                    }}
                  >
                    <Button
                      aria-label={`上传音频 ${index + 1}`}
                      icon={<Icon name="upload" />}
                      loading={uploading === index}
                      disabled={locked}
                    >
                      {uploading === index ? "上传中…" : "上传新音频"}
                    </Button>
                  </Upload>
                  <Typography.Text type="secondary" className="small-text">
                    WAV / PCM / Opus · ≤128 MiB
                  </Typography.Text>
                </div>
                {uploading === index && (
                  <Progress percent={progress} size="small" status="active" />
                )}
              </Card>
            </Col>
          );
        })}
      </Row>
      <div className="source-delivery">
        <Icon name="server" size={16} />
        <Typography.Text type="secondary">
          素材自动同步到 {senders.length} 台发流机器
        </Typography.Text>
        <span className="secondary-text small-text">
          {senders
            .map(
              (row) =>
                nodes.find((node) => node.id === row.nodeId)?.address ||
                hosts.find((host) => host.id === row.nodeId)?.address ||
                row.nodeId ||
                "待选择",
            )
            .join(" · ")}
        </span>
      </div>
      <div className="form-group">
        <div className="form-group-heading">
          <h3>发送时序</h3>
          <Typography.Text type="secondary">
            全部用户先入频道，随后两路持续发流
          </Typography.Text>
        </div>
        <div className="audio-timing-layout">
          <Field label="第二路延迟（毫秒）">
            <NumericInput
              min={0}
              max={3600000}
              disabled={locked}
              value={config.schedule.offsetMs}
              onChange={(event) =>
                onChange({
                  schedule: {
                    ...config.schedule,
                    mode: "staggered",
                    offsetMs: +event.target.value,
                  },
                })
              }
            />
          </Field>
          <div
            className="schedule-preview"
            aria-label={`第一路立即发送，第二路延迟 ${config.schedule.offsetMs / 1000} 秒后持续发送`}
          >
            {[0, 1].map((index) => (
              <div key={index}>
                <span>音频 0{index + 1}</span>
                <div className="schedule-track">
                  {index === 1 && (
                    <span className="schedule-delay">
                      +{config.schedule.offsetMs / 1000}s
                    </span>
                  )}
                  <span
                    className={
                      index === 0 ? "schedule-bar" : "schedule-bar second"
                    }
                  >
                    持续发送
                  </span>
                </div>
              </div>
            ))}
            <p>
              全部 UID 就绪<span>运行 {config.durationSec} 秒</span>
            </p>
          </div>
        </div>
      </div>
      <div className="form-group">
        <div className="form-group-heading">
          <h3>音频格式</h3>
          <Typography.Text type="secondary">
            {config.audioInputMode === "opus"
              ? "Opus 格式由所选文件确定"
              : "两路素材须使用相同的采样格式"}
          </Typography.Text>
        </div>
        <Row gutter={20}>
          <Col span={12}>
            <Field label="采样率">
              <Select
                disabled={locked || config.audioInputMode === "opus"}
                value={config.sampleRate}
                onChange={(sampleRate) => onChange({ sampleRate })}
                options={[8000, 16000, 32000, 44100, 48000].map((rate) => ({
                  value: rate,
                  label: `${rate / 1000} kHz`,
                }))}
              />
            </Field>
          </Col>
          <Col span={12}>
            <Field label="声道">
              <Select
                disabled={locked || config.audioInputMode === "opus"}
                value={config.audioChannels}
                onChange={(audioChannels) => onChange({ audioChannels })}
                options={[
                  { value: 1, label: "单声道" },
                  { value: 2, label: "双声道" },
                ]}
              />
            </Field>
          </Col>
        </Row>
      </div>
    </section>
  );
}
