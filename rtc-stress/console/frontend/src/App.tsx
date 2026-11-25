import {
  useCallback,
  useEffect,
  useRef,
  useState,
  lazy,
  Suspense,
} from "react";
import {
  App as AntApp,
  Alert,
  Badge,
  Breadcrumb,
  Button,
  Card,
  Divider,
  Layout,
  Menu,
  Space,
  Spin,
  Tag,
  Typography,
} from "antd";
import { api, APIError, uploadAudio } from "./api";
import { Icon } from "./components/Icons";
const ScenarioWorkspace = lazy(() => import("./components/ScenarioWorkspace"));
const OverviewPage = lazy(() => import("./components/OverviewPage"));
const MachinesPage = lazy(() => import("./components/MachinesPage"));
import { useHostTelemetry } from "./components/useHostTelemetry";
import { time } from "./format";
import {
  terminal,
  allocationsFor,
  prepareMembership,
  type AudioFile,
  type Config,
  type Host,
  type NodeState,
  type Plan,
  type Settings,
  type Task,
} from "./types";
const pageFromHash = () => {
  const value = window.location.hash.slice(1);
  return ["overview", "scenario", "machines"].includes(value)
    ? value
    : "overview";
};
export default function App() {
  const { message } = AntApp.useApp();
  const [loaded, setLoaded] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [page, setPage] = useState(pageFromHash),
    [selected, setSelected] = useState(""),
    [plan, setPlan] = useState<Plan | null>(null),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(""),
    [lastUpdated, setLastUpdated] = useState(""),
    [output, setOutput] = useState("");
  const navigate = (next: string) => {
    setPage(next);
    window.location.hash = next;
    window.scrollTo({ top: 0 });
  };
  useEffect(() => {
    document.getElementById("main-content")?.focus();
  }, [page]);
  useEffect(() => {
    const onHashChange = () => setPage(pageFromHash());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);
  const [nodes, setNodes] = useState<NodeState[]>([]);
  const [audioFiles, setAudioFiles] = useState<AudioFile[]>([]);
  const [uploading, setUploading] = useState<number | null>(null),
    [uploadProgress, setUploadProgress] = useState(0);
  const [overviewMode, setOverviewMode] = useState<"configuration" | "task">(
    "configuration",
  );
  const [taskDetails, setTaskDetails] = useState<Record<string, Task>>({});
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [detailRetry, setDetailRetry] = useState(0);
  const taskRequests = useRef(new Map<string, Promise<Task>>());
  const fetchTask = useCallback((id: string) => {
    const existing = taskRequests.current.get(id);
    if (existing) return existing;
    const request = api<Task>(`/tasks/${encodeURIComponent(id)}`)
      .then((task) => {
        setTaskDetails((current) => {
          const latest = Object.values(current)[0];
          return latest && latest.createdAt > task.createdAt
            ? current
            : { [task.id]: task };
        });
        return task;
      })
      .finally(() => taskRequests.current.delete(id));
    taskRequests.current.set(id, request);
    return request;
  }, []);
  const taskDetailsRef = useRef(taskDetails);
  useEffect(() => {
    taskDetailsRef.current = taskDetails;
  }, [taskDetails]);
  const [planPending, setPlanPending] = useState(true);
  const [dirty, setDirty] = useState(false),
    [planError, setPlanError] = useState("");
  const load = useCallback(async () => {
    try {
      const value = await api<{
        settings: Settings;
        nodes: NodeState[];
        audio: AudioFile[];
        currentTaskId: string;
      }>("/bootstrap");
      const nodeList = value.nodes,
        audioList = value.audio;
      setSettings({
        ...value.settings,
        config: prepareMembership(value.settings.config),
      });
      setDirty(value.settings.config.allocationPolicy !== "membership");
      setAudioFiles(audioList);
      setNodes(nodeList);
      if (value.currentTaskId) {
        setSelected(value.currentTaskId);
        setOverviewMode("task");
      }
      setLoaded(true);
      setLastUpdated(new Date().toISOString());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);
  useEffect(() => {
    const loading =
      page === "scenario"
        ? import("./components/ScenarioWorkspace")
        : page === "machines"
          ? import("./components/MachinesPage")
          : import("./components/OverviewPage");
    void loading.catch(() => {});
  }, [page]);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    if (!loaded) return;
    let active = true;
    let inFlight = false;
    const poll = async () => {
      if (document.hidden || inFlight) return;
      inFlight = true;
      try {
        const [nodeList, remoteSettings] = await Promise.all([
          api<NodeState[]>("/nodes"),
          dirty
            ? Promise.resolve(null)
            : api<{ settings: Settings }>("/settings"),
        ]);
        if (active) {
          setNodes(nodeList);
          if (remoteSettings) {
            const next = {
              ...remoteSettings.settings,
              config: prepareMembership(remoteSettings.settings.config),
            };
            setSettings((current) =>
              JSON.stringify(current) === JSON.stringify(next) ? current : next,
            );
            if (
              remoteSettings.settings.config.allocationPolicy !== "membership"
            )
              setDirty(true);
          }
          setLastUpdated(new Date().toISOString());
        }
        const centerTaskId = nodeList.find(
          (node) => node.mode === "local",
        )?.activeTaskId;
        const taskIds = centerTaskId
          ? [centerTaskId]
          : Object.values(taskDetailsRef.current)
              .filter((task) => !terminal(task.state))
              .map((task) => task.id);
        const unseen = taskIds.some((id) => !taskDetailsRef.current[id]);
        const updates = (
          await Promise.all(
            taskIds.map((id) =>
              fetchTask(id).catch((error) => {
                if (error instanceof APIError && error.status === 404) {
                  setTaskDetails((current) => (current[id] ? {} : current));
                  setSelected((current) => (current === id ? "" : current));
                  return null;
                }
                throw error;
              }),
            ),
          )
        ).filter((value): value is Task => value !== null);
        const latest = Object.values(taskDetailsRef.current)[0];
        if (
          active &&
          unseen &&
          updates.length &&
          (!latest || updates[0].createdAt >= latest.createdAt)
        ) {
          setSelected(updates[0].id);
          setOverviewMode("task");
        }
      } catch (e) {
        if (active) {
          setError(e instanceof Error ? e.message : String(e));
        }
      } finally {
        inFlight = false;
      }
    };
    // Bootstrap already provided nodes/settings; begin refreshing on the next interval.
    const interval = setInterval(() => void poll(), 2000);
    document.addEventListener("visibilitychange", poll);
    return () => {
      active = false;
      clearInterval(interval);
      document.removeEventListener("visibilitychange", poll);
    };
  }, [loaded, dirty, fetchTask]);
  useEffect(() => {
    if (!settings || !loaded) return;
    setPlanPending(true);
    let active = true;
    const timer = setTimeout(() => {
      api<Plan>("/plan", "POST", settings.config)
        .then((p) => {
          if (active) {
            setPlan(p);
            setPlanError("");
            setPlanPending(false);
          }
        })
        .catch((e) => {
          if (active) {
            setPlan(null);
            setPlanError(e instanceof Error ? e.message : String(e));
            setPlanPending(false);
          }
        });
    }, 250);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [settings, loaded]);
  const selectedTaskId = selected || undefined;
  const task =
    overviewMode === "task" && selectedTaskId
      ? taskDetails[selectedTaskId]
      : undefined;
  useEffect(() => {
    if (
      overviewMode !== "task" ||
      !selectedTaskId ||
      taskDetails[selectedTaskId]
    ) {
      setDetailLoading(false);
      return;
    }
    let active = true;
    setDetailLoading(true);
    setDetailError("");
    fetchTask(selectedTaskId)
      .catch((error) => {
        if (!active) return;
        if (error instanceof APIError && error.status === 404) {
          setTaskDetails({});
          setSelected("");
          setDetailError("");
        } else
          setDetailError(
            error instanceof Error ? error.message : String(error),
          );
      })
      .finally(() => {
        if (active) setDetailLoading(false);
      });
    return () => {
      active = false;
    };
  }, [overviewMode, selectedTaskId, taskDetails, fetchTask, detailRetry]);
  const activeTask = Object.values(taskDetails).find((t) => !terminal(t.state)),
    locked =
      !!activeTask || nodes.some((node) => !!node.activeTaskId) || !!busy;
  const config = settings?.config;
  const patch = (value: Partial<Config>) => {
    setPlanPending(true);
    setDirty(true);
    setOverviewMode("configuration");
    setSelected("");
    setTaskDetails({});
    setSettings((s) =>
      s
        ? {
            ...s,
            config: {
              ...s.config,
              ...value,
              allocations: value.allocations ?? allocationsFor(s.config),
            },
          }
        : s,
    );
  };
  const updateHost = (index: number, value: Partial<Host>) => {
    setDirty(true);
    setSettings((s) =>
      s
        ? {
            ...s,
            hosts: s.hosts.map((h, i) =>
              i === index ? { ...h, ...value } : h,
            ),
          }
        : s,
    );
  };
  const perform = async (name: string, action: () => Promise<void>) => {
    setBusy(name);
    setError("");
    setNotice("");
    try {
      await action();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };
  const save = async () => {
    if (!settings) return;
    await api("/settings", "PUT", settings);
    const saved = await api<{ settings: Settings }>("/settings");
    setSettings(saved.settings);
    setDirty(false);
    setOverviewMode("configuration");
    setSelected("");
    setTaskDetails({});
  };
  const start = () =>
    perform("start", async () => {
      await save();
      const value = await api<Task>("/tasks", "POST", settings!.config);
      setTaskDetails({ [value.id]: value });
      setSelected(value.id);
      setOverviewMode("task");
      navigate("overview");
      setNotice("任务已创建，正在检查所选执行机器。");
    });
  const hostAction = (host: Host, action: "check" | "reconnect") =>
    perform(host.id + action, async () => {
      if (action === "check") await save();
      const r = await api<{ message: string }>(
        action === "check"
          ? "/hosts/" + host.id + "/check"
          : "/nodes/" + host.id + "/reconnect",
        "POST",
        {},
      );
      setOutput(r.message);
      setNotice(
        `${host.id} ${action === "reconnect" ? "已请求重新连接并同步任务" : "检查通过"}`,
      );
    });
  const refreshNodes = () =>
    void perform("refresh", async () => {
      const [nodeList, files] = await Promise.all([
        api<NodeState[]>("/nodes"),
        api<AudioFile[]>("/audio"),
      ]);
      setNodes(nodeList);
      setAudioFiles(files);
      setNotice("机器与音频列表已更新。");
    });
  const handleUpload = (index: number, file: File) =>
    void perform("upload", async () => {
      if (!settings) return;
      setUploading(index);
      setUploadProgress(0);
      try {
        const saved = await uploadAudio(
          file,
          settings.config.sampleRate,
          settings.config.audioChannels,
          setUploadProgress,
        );
        setAudioFiles((list) => [
          saved,
          ...list.filter((f) => f.id !== saved.id),
        ]);
        const values = [...settings.config.audioFiles] as [string, string];
        values[index] = "asset:" + saved.id;
        const labels = [
          ...(settings.config.audioLabels || settings.config.audioFiles),
        ] as [string, string];
        labels[index] = saved.name;
        patch({
          audioFiles: values,
          audioLabels: labels,
          audioInputMode: saved.codec === "opus" ? "opus" : "pcm",
          ...(saved.codec === "opus" ? { engine: "go-sdk" as const } : {}),
          sampleRate: saved.sampleRate,
          audioChannels: saved.channels,
        });
        setNotice(`已上传 ${saved.name}，选为第 ${index + 1} 路音频。`);
      } finally {
        setUploading(null);
      }
    });
  const [telemetryMinutes, setTelemetryMinutes] = useState(720);
  const telemetry = useHostTelemetry(
    page === "machines" && loaded,
    telemetryMinutes <= 60 ? 5 : 300,
  );
  useEffect(() => {
    if (notice)
      void message.success({
        content: notice,
        key: "action-feedback",
        duration: 4,
      });
  }, [notice, message]);
  const saveSettings = () =>
    void perform("save", async () => {
      await save();
      setNotice("配置已保存。");
    });
  if (!loaded || !settings || !config)
    return (
      <div className="loading-screen">
        <Card>
          <Space orientation="vertical" align="center" size={20}>
            <Spin size="large" />
            <Typography.Title level={4}>
              正在连接 RTC 压测控制台
            </Typography.Title>
            <Typography.Text type="secondary">
              读取场景、执行节点与音频资源
            </Typography.Text>
            {error && (
              <Alert
                type="error"
                title={error}
                action={<Button onClick={() => void load()}>重新连接</Button>}
              />
            )}
          </Space>
        </Card>
      </div>
    );
  const online = nodes.filter((node) => node.online).length;
  const pageTitle =
    page === "overview"
      ? "任务总览"
      : page === "scenario"
        ? "场景配置"
        : "执行机器";
  return (
    <Layout className="console-shell">
      <a
        className="skip-link"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        跳转到主要内容
      </a>
      <Layout.Sider width={216} className="console-sidebar">
        <a
          className="console-brand"
          href="#overview"
          onClick={() => navigate("overview")}
        >
          <span className="console-brand-icon">
            <Icon name="audio" size={23} />
          </span>
          <div>
            <strong>RTC STRESS</strong>
            <span>音频压测控制台</span>
          </div>
        </a>
        <div className="sidebar-section-label">工作空间</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[page]}
          onClick={({ key }) => navigate(key)}
          items={[
            { key: "overview", icon: <Icon name="grid" />, label: "任务总览" },
            { key: "scenario", icon: <Icon name="tune" />, label: "场景配置" },
            {
              key: "machines",
              icon: <Icon name="server" />,
              label: (
                <span className="menu-machine-label">
                  执行机器
                  <Badge count={nodes.length} showZero color="#314c70" />
                </span>
              ),
            },
          ]}
        />
        <div className="sidebar-footer">
          <Badge
            status={online === nodes.length ? "success" : "warning"}
            text={`执行节点 ${online} / ${nodes.length} 在线`}
          />
        </div>
      </Layout.Sider>
      <Layout className="console-main">
        <Layout.Header className="console-topbar">
          <Breadcrumb items={[{ title: "RTC 控制台" }, { title: pageTitle }]} />
          <Space size={16}>
            <Badge
              status={online === nodes.length ? "success" : "warning"}
              text={`${online} 个节点在线`}
            />
            <Divider orientation="vertical" />
            <Typography.Text type="secondary" className="small-text">
              {time(lastUpdated)}
            </Typography.Text>
          </Space>
        </Layout.Header>
        <main id="main-content" tabIndex={-1} className="console-content">
          {page !== "machines" && (
            <div className="page-heading">
              <div>
                <Typography.Title level={1}>{pageTitle}</Typography.Title>
              </div>
              <Space>
                {dirty && page !== "scenario" && (
                  <Tag color="warning">有未保存修改</Tag>
                )}
                {page === "overview" ? (
                  <>
                    <Button
                      icon={<Icon name="tune" />}
                      onClick={() => navigate("scenario")}
                    >
                      编辑场景
                    </Button>
                    <Button
                      type="primary"
                      icon={<Icon name="play" />}
                      loading={busy === "start"}
                      disabled={
                        locked ||
                        planPending ||
                        !plan ||
                        allocationsFor(config).some(
                          (row) =>
                            !nodes.find((node) => node.id === row.nodeId)
                              ?.online,
                        )
                      }
                      onClick={() => void start()}
                    >
                      启动任务
                    </Button>
                  </>
                ) : page === "scenario" ? (
                  <Button
                    icon={<Icon name="arrow" />}
                    onClick={() => navigate("overview")}
                  >
                    返回任务总览
                  </Button>
                ) : null}
              </Space>
            </div>
          )}
          {page === "machines" && dirty && (
            <Tag color="warning">有未保存修改</Tag>
          )}
          {error && (
            <Alert
              className="global-alert"
              type="error"
              title={error}
              showIcon
              closable={{ onClose: () => setError("") }}
            />
          )}
          <Suspense
            fallback={
              <Card className="page-loading">
                <Spin /> 正在加载页面…
              </Card>
            }
          >
            {page === "overview" && (
              <OverviewPage
                config={task?.config || config}
                plan={plan}
                task={task}
                taskLoading={detailLoading}
                taskError={detailError}
                onRetryTask={() => setDetailRetry((value) => value + 1)}
                nodes={nodes}
                hosts={settings.hosts}
                planError={planError}
                dirty={dirty}
                busy={busy}
                onStop={() => {
                  if (task)
                    void perform("stop", async () => {
                      await api(`/tasks/${task.id}/stop`, "POST", {});
                      setNotice("已请求停止本任务。");
                    });
                }}
                onEdit={() => navigate("scenario")}
                onMachines={() => navigate("machines")}
              />
            )}
            {page === "scenario" && (
              <ScenarioWorkspace
                config={config}
                hosts={settings.hosts}
                nodes={nodes}
                audioFiles={audioFiles}
                plan={plan}
                planError={planError}
                checking={planPending}
                locked={locked}
                dirty={dirty}
                saving={busy === "save"}
                onChange={patch}
                onSave={saveSettings}
                onUpload={handleUpload}
                uploading={uploading}
                progress={uploadProgress}
              />
            )}
            {page === "machines" && (
              <MachinesPage
                hosts={settings.hosts}
                nodes={nodes}
                telemetry={telemetry}
                minutes={telemetryMinutes}
                onMinutesChange={setTelemetryMinutes}
                busy={busy}
                locked={locked}
                dirty={dirty}
                output={output}
                onUpdate={updateHost}
                onCheck={(host) => void hostAction(host, "check")}
                onReconnect={(host) => void hostAction(host, "reconnect")}
                onSave={saveSettings}
                onRefresh={refreshNodes}
              />
            )}
          </Suspense>
        </main>
      </Layout>
    </Layout>
  );
}
