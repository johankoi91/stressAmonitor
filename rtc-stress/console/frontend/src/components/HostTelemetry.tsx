import {
  useEffect,
  useMemo,
  useRef,
  useId,
  type Dispatch,
  type SetStateAction,
  useState,
  type CSSProperties,
} from "react";
import {
  Alert,
  Badge,
  Button,
  Card,
  Collapse,
  Progress,
  Select,
  Space,
  Statistic,
  Tag,
  Typography,
} from "antd";
import { Icon } from "./Controls";
import {
  nodeColor,
  nodeDash,
  type Host,
  type HostID,
  type NodeState,
} from "../types";

import type { Sample, Series, HostTelemetryData } from "./useHostTelemetry";
export type { HostTelemetryData } from "./useHostTelemetry";
type Metric = "cpu" | "memoryPercent" | "receiveBps" | "transmitBps";
const clock = (at: number) =>
  new Date(at).toLocaleTimeString("zh-CN", {
    hour12: false,
    timeZone: "Asia/Shanghai",
  });
const value = (n: number | null | undefined, unit = "%") =>
  n == null ? "—" : `${(unit === "Mb/s" ? n / 1e6 : n).toFixed(1)} ${unit}`;
const gib = (n: number | null | undefined) =>
  n == null ? "—" : (n / 1024 ** 3).toFixed(1);

const colorStyle = (id: string) =>
  ({ "--node-color": nodeColor(id) }) as CSSProperties;
const rangeLabel = (minutes: number) =>
  minutes >= 60 ? `${minutes / 60} 小时` : `${minutes} 分钟`;
const plotTime = (at: number, date = false, seconds = false) =>
  new Date(at).toLocaleString("zh-CN", {
    timeZone: "Asia/Shanghai",
    ...(date ? ({ month: "2-digit", day: "2-digit" } as const) : {}),
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" as const } : {}),
    hour12: false,
  });

type Inspection = { fraction: number; metric: Metric };
const inspectValue = (n: number | null | undefined, unit: string) => {
  if (n == null || !Number.isFinite(n)) return "无采样";
  if (unit === "Mb/s")
    return Math.abs(n) > 0 && Math.abs(n) < 1e6
      ? `${(n / 1000).toFixed(2)} Kb/s`
      : `${(n / 1e6).toFixed(2)} Mb/s`;
  return `${n.toFixed(2)} %`;
};
function Timeline({
  nodeLabel,
  nodeLabels,
  title,
  metric,
  series,
  end,
  minutes,
  intervalSeconds,
  percent = false,
  inspection,
  onInspect,
}: {
  nodeLabel: string;
  nodeLabels: Record<string, string>;
  title: string;
  metric: Metric;
  series: Series[];
  end: number;
  minutes: number;
  intervalSeconds: number;
  percent?: boolean;
  inspection: Inspection | null;
  onInspect: Dispatch<SetStateAction<Inspection | null>>;
}) {
  const bucketMs = intervalSeconds * 1000,
    intervalLabel = intervalSeconds === 5 ? "5 秒" : "5 分钟";
  const svgRef = useRef<SVGSVGElement>(null),
    tooltipId = useId();
  const [plotWidth, setPlotWidth] = useState(640);
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const observer = new ResizeObserver(([entry]) =>
      setPlotWidth(Math.max(240, Math.round(entry.contentRect.width))),
    );
    observer.observe(svg);
    return () => observer.disconnect();
  }, []);
  const start = end - minutes * 60000;
  const visible = useMemo(
    () =>
      series.map((item) => ({
        ...item,
        samples: item.samples.filter(
          (point) => point.at >= start && point.at <= end,
        ),
      })),
    [series, start, end],
  );
  const max = useMemo(
    () =>
      percent
        ? 100
        : Math.max(
            1e6,
            ...visible.flatMap((item) =>
              item.samples.map((point) => point[metric] || 0),
            ),
          ) * 1.1,
    [visible, metric, percent],
  );
  const width = plotWidth,
    height = 210,
    left = 48,
    right = 12,
    top = 12,
    bottom = 28;
  const x = (at: number) =>
    left + ((at - start) / (end - start)) * (width - left - right);
  const y = (n: number) => top + (1 - n / max) * (height - top - bottom);
  const slots = Math.max(1, Math.round((end - start) / bucketMs));
  const cursorAt =
    inspection == null
      ? null
      : start + Math.round(inspection.fraction * slots) * bucketMs;
  const active = inspection?.metric === metric;
  const unit = percent ? "%" : "Mb/s";
  const plotted = useMemo(
    () =>
      visible.map((item) => {
        let previous: Sample | undefined;
        let path = "";
        let last: Sample | undefined;
        const points = new Map<number, Sample>();
        for (const point of item.samples) {
          points.set(point.at, point);
          const n = point[metric];
          if (n == null) {
            previous = undefined;
            continue;
          }
          const command =
            !previous || point.at - previous.at > bucketMs * 1.2 ? "M" : "L";
          path += `${command}${x(point.at).toFixed(1)},${y(n).toFixed(1)} `;
          previous = point;
          last = point;
        }
        return { node: item.node, path, last, points };
      }),
    [visible, metric, bucketMs, start, end, width, max],
  );
  const observations = plotted.map((item) => ({
    node: item.node,
    point: cursorAt == null ? undefined : item.points.get(cursorAt),
  }));
  const tooltipWidth = Math.min(242, width - 16);
  const tooltipLeft =
    cursorAt == null
      ? 8
      : Math.max(8, Math.min(width - tooltipWidth - 8, x(cursorAt) + 14));
  const ticks =
    width < 400 && minutes >= 360
      ? [start, end]
      : [start, (start + end) / 2, end];
  return (
    <Card className="telemetry-chart" size="small">
      <div className="telemetry-chart-title">
        <h3>{title}</h3>
        <span>{unit}</span>
      </div>
      <div className="telemetry-plot">
        <svg
          ref={svgRef}
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          tabIndex={0}
          data-telemetry-timeline={metric}
          aria-describedby={active && cursorAt != null ? tooltipId : undefined}
          aria-label={`${title}时间轴，机器 ${nodeLabel}，最近 ${rangeLabel(minutes)}，每点为 ${intervalLabel}均值。移动指针显示时间与各节点数值；方向键按 ${intervalLabel}查看，Escape 退出。`}
          onPointerMove={(event) => {
            const box = event.currentTarget.getBoundingClientRect();
            const raw = Math.max(
              0,
              Math.min(
                1,
                (((event.clientX - box.left) / box.width) * width - left) /
                  (width - left - right),
              ),
            );
            const fraction = Math.round(raw * slots) / slots;
            onInspect((old) =>
              old?.fraction === fraction && old.metric === metric
                ? old
                : { fraction, metric },
            );
          }}
          onPointerLeave={() =>
            onInspect((old) => (old?.metric === metric ? null : old))
          }
          onFocus={(event) => {
            if (event.currentTarget.matches(":focus-visible"))
              onInspect((old) => ({ fraction: old?.fraction ?? 1, metric }));
          }}
          onBlur={(event) => {
            if (
              !(event.relatedTarget instanceof Element) ||
              !event.relatedTarget.hasAttribute("data-telemetry-timeline")
            )
              onInspect(null);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") onInspect(null);
            if (
              ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
            ) {
              event.preventDefault();
              onInspect((old) => {
                const slot =
                  event.key === "Home"
                    ? 0
                    : event.key === "End"
                      ? slots
                      : Math.max(
                          0,
                          Math.min(
                            slots,
                            Math.round((old?.fraction ?? 1) * slots) +
                              (event.key === "ArrowLeft" ? -1 : 1),
                          ),
                        );
                return { fraction: slot / slots, metric };
              });
            }
          }}
        >
          {[0, 0.5, 1].map((fraction) => (
            <g key={fraction}>
              <line
                x1={left}
                x2={width - right}
                y1={y(max * fraction)}
                y2={y(max * fraction)}
                className="chart-grid"
              />
              <text x={left - 8} y={y(max * fraction) + 4} textAnchor="end">
                {((max * fraction) / (percent ? 1 : 1e6)).toFixed(
                  percent ? 0 : 1,
                )}
              </text>
            </g>
          ))}
          {plotted.map((item) => (
            <g key={item.node}>
              <path
                d={item.path}
                fill="none"
                className={`chart-line node-${item.node}`}
                style={colorStyle(item.node)}
                strokeWidth="2"
                strokeDasharray={nodeDash(item.node)}
              />
              {item.last && (
                <circle
                  className={`chart-point node-${item.node}`}
                  style={colorStyle(item.node)}
                  cx={x(item.last.at)}
                  cy={y(item.last[metric]!)}
                  r="2.5"
                />
              )}
            </g>
          ))}
          {ticks.map((at, index) => (
            <text
              key={index}
              x={x(at)}
              y={height - 5}
              textAnchor={
                index === 0
                  ? "start"
                  : index === ticks.length - 1
                    ? "end"
                    : "middle"
              }
            >
              {plotTime(at, minutes >= 360, intervalSeconds === 5)}
            </text>
          ))}
          {cursorAt != null && (
            <>
              <line
                x1={x(cursorAt)}
                x2={x(cursorAt)}
                y1={top}
                y2={height - bottom}
                className="chart-cursor"
              />
              {observations.map(
                ({ node, point }) =>
                  point?.[metric] != null && (
                    <circle
                      key={node}
                      className="chart-inspect-point"
                      cx={x(cursorAt)}
                      cy={y(point[metric]!)}
                      r="4"
                      style={{ stroke: nodeColor(node) }}
                    />
                  ),
              )}
            </>
          )}
        </svg>
        {active && cursorAt != null && (
          <div
            id={tooltipId}
            role="tooltip"
            className="chart-tooltip"
            style={{ left: tooltipLeft, width: tooltipWidth, top: 8 }}
          >
            <time>{plotTime(cursorAt, true, intervalSeconds === 5)}</time>
            <div className="chart-tooltip-metric">
              {title} · {intervalLabel}均值
            </div>
            <div className="chart-tooltip-values">
              {observations.slice(0, 8).map(({ node, point }) => (
                <div key={node}>
                  <span className="chart-tooltip-node">
                    <i style={{ background: nodeColor(node) }} />
                    {nodeLabels[node] || node}
                  </span>
                  <strong className={point?.[metric] == null ? "missing" : ""}>
                    {inspectValue(point?.[metric], unit)}
                  </strong>
                </div>
              ))}
            </div>
            {observations.length > 8 && (
              <div className="chart-tooltip-more">
                其余 {observations.length - 8} 台机器可在上方筛选查看
              </div>
            )}
          </div>
        )}
      </div>
      <div className="chart-readout" aria-live={active ? "polite" : "off"}>
        {cursorAt != null && (
          <>
            <time>{plotTime(cursorAt, true, intervalSeconds === 5)}</time>
            {observations.map(({ node, point }) => (
              <span key={node} style={{ color: nodeColor(node) }}>
                {nodeLabels[node] || node} {value(point?.[metric], unit)}
              </span>
            ))}
          </>
        )}
      </div>
    </Card>
  );
}
function TelemetryCharts({
  nodeLabel,
  nodeLabels,
  series,
  end,
  minutes,
  intervalSeconds,
}: {
  nodeLabel: string;
  nodeLabels: Record<string, string>;
  series: Series[];
  end: number;
  minutes: number;
  intervalSeconds: number;
}) {
  const [inspection, setInspection] = useState<Inspection | null>(null);
  useEffect(() => setInspection(null), [nodeLabel, minutes, intervalSeconds]);
  return (
    <div className="telemetry-charts">
      {(
        [
          { title: "CPU 使用率", metric: "cpu", percent: true },
          { title: "内存使用率", metric: "memoryPercent", percent: true },
          { title: "网络下行", metric: "receiveBps" },
          { title: "网络上行", metric: "transmitBps" },
        ] as const
      ).map((item) => (
        <Timeline
          key={item.metric}
          {...item}
          nodeLabel={nodeLabel}
          nodeLabels={nodeLabels}
          series={series}
          end={end}
          minutes={minutes}
          intervalSeconds={intervalSeconds}
          inspection={inspection}
          onInspect={setInspection}
        />
      ))}
    </div>
  );
}
export default function HostTelemetry({
  data,
  nodes,
  hosts,
  minutes,
  onMinutesChange,
}: {
  data: HostTelemetryData;
  nodes: NodeState[];
  hosts: Host[];
  minutes: number;
  onMinutesChange: (minutes: number) => void;
}) {
  const ids = [
    ...new Set([
      ...hosts.map((host) => host.id),
      ...nodes.map((node) => node.id),
    ]),
  ].sort();
  const nodeLabels = useMemo(() => {
    const labels: Record<string, string> = {};
    for (const host of hosts) labels[host.id] = host.address || host.id;
    for (const node of nodes)
      labels[node.id] = node.address || labels[node.id] || node.id;
    return labels;
  }, [hosts, nodes]);
  const [node, setNode] = useState<HostID | "all">("all");
  const [frozen, setFrozen] = useState<HostTelemetryData | null>(null);
  const view = frozen || data;
  const { series, now, updated, error, intervalSeconds, loading } = view;
  const bucketMs = intervalSeconds * 1000;
  const intervalLabel = intervalSeconds === 5 ? "5 秒" : "5 分钟";
  const paused = frozen !== null;
  const end = Math.floor(now / bucketMs) * bucketMs;
  const selectedIds = ids.filter((id) => node === "all" || node === id);
  const visible = useMemo(
    () => series.filter((s) => node === "all" || s.node === node),
    [series, node],
  );
  return (
    <div className="host-telemetry page-stack">
      <div className="telemetry-toolbar">
        <div>
          <h2>CPU、内存与网络时间轴</h2>
        </div>
        <Space>
          <Select
            aria-label="监控机器"
            value={node}
            onChange={setNode}
            style={{ width: 190 }}
            options={[
              { value: "all", label: "全部机器" },
              ...ids.map((id) => ({ value: id, label: nodeLabels[id] || id })),
            ]}
          />
          <Select
            aria-label="监控时间范围"
            value={minutes}
            onChange={(value) => {
              setFrozen(null);
              onMinutesChange(value);
            }}
            style={{ width: 155 }}
            options={[15, 60, 180, 360, 720].map((n) => ({
              value: n,
              label: `最近 ${rangeLabel(n)}`,
            }))}
          />
          <Button
            aria-pressed={paused}
            icon={<Icon name={paused ? "play" : "stop"} />}
            onClick={() => setFrozen(paused ? null : data)}
          >
            {paused ? "继续刷新" : "暂停刷新"}
          </Button>
        </Space>
      </div>
      <div className="telemetry-meta">
        <Space wrap size={18}>
          {(paused || loading) && (
            <Badge
              status={paused ? "warning" : "processing"}
              text={paused ? "视图已暂停" : `正在读取${intervalLabel}数据`}
            />
          )}
          <Typography.Text type="secondary">
            {updated ? `最近更新 ${clock(updated)}` : "等待采样"}
          </Typography.Text>
        </Space>
      </div>
      {error && (
        <Alert type="error" showIcon title="监控更新失败" description={error} />
      )}
      <div className="telemetry-node-grid">
        {selectedIds.map((id) => {
          const latest = series.find((s) => s.node === id)?.latest,
            online = !!nodes.find((n) => n.id === id)?.online,
            stale = !latest || now - latest.at > 8000,
            healthy = online && !stale && !error && latest?.status === "ok";
          const status = !online
            ? "节点离线"
            : error
              ? "更新失败"
              : stale
                ? "采样未更新"
                : latest?.status === "unavailable"
                  ? "采集不可用"
                  : latest?.status === "partial"
                    ? "部分指标可用"
                    : "采集正常";
          return (
            <Card
              key={id}
              className="telemetry-node"
              style={{ borderTopColor: nodeColor(id) }}
              title={
                <code className="telemetry-node-address">
                  {nodeLabels[id] || id}
                </code>
              }
              extra={
                <Tag color={healthy ? "success" : "warning"}>{status}</Tag>
              }
              aria-label={`${nodeLabels[id] || id} 实时指标`}
            >
              <div className="telemetry-primary">
                <div>
                  <Statistic
                    title="CPU 使用率"
                    value={latest?.cpu ?? "—"}
                    precision={1}
                    suffix={latest?.cpu == null ? undefined : "%"}
                  />
                  {latest?.cpu != null ? (
                    <Progress
                      percent={latest.cpu}
                      showInfo={false}
                      strokeColor={nodeColor(id)}
                      size="small"
                    />
                  ) : (
                    <div className="metric-progress-placeholder" aria-hidden />
                  )}
                </div>
                <div>
                  <Statistic
                    title="内存使用率"
                    value={latest?.memoryPercent ?? "—"}
                    precision={1}
                    suffix={latest?.memoryPercent == null ? undefined : "%"}
                  />
                  <div className="secondary-text small-text">
                    {gib(latest?.memoryUsed)} / {gib(latest?.memoryTotal)} GiB
                  </div>
                </div>
              </div>
              <div className="telemetry-network">
                <div>
                  <span>下行 ↓</span>
                  <strong>{value(latest?.receiveBps, "Mb/s")}</strong>
                </div>
                <div>
                  <span>上行 ↑</span>
                  <strong>{value(latest?.transmitBps, "Mb/s")}</strong>
                </div>
              </div>
              <div className="telemetry-sample">
                {latest ? `采样 ${clock(latest.at)}` : "等待采样"} · Netdata{" "}
                {latest?.netdataVersion || "等待启动"}
              </div>
              {latest?.error && (
                <Collapse
                  ghost
                  size="small"
                  items={[
                    {
                      key: "error",
                      label: "采集详情",
                      children: (
                        <span className="secondary-text break-anywhere">
                          {latest.error}
                        </span>
                      ),
                    },
                  ]}
                />
              )}
              {stale && latest && (
                <div className="secondary-text small-text">显示最后记录值</div>
              )}
            </Card>
          );
        })}
      </div>
      <div className="telemetry-legend" aria-label="曲线图例">
        {selectedIds.map((id) => (
          <span key={id}>
            <i
              style={{
                borderTopColor: nodeColor(id),
                borderTopStyle:
                  nodeDash(id) === "2 3"
                    ? "dotted"
                    : nodeDash(id)
                      ? "dashed"
                      : "solid",
              }}
            />
            <span style={{ color: nodeColor(id) }}>{nodeLabels[id] || id}</span>
          </span>
        ))}
        <Typography.Text type="secondary">
          移动鼠标查看各节点数值；方向键逐点查看
        </Typography.Text>
      </div>
      <TelemetryCharts
        nodeLabel={selectedIds.map((id) => nodeLabels[id] || id).join("/")}
        nodeLabels={nodeLabels}
        series={visible}
        end={end}
        minutes={minutes}
        intervalSeconds={intervalSeconds}
      />
    </div>
  );
}
