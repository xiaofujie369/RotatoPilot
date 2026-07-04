import React, { useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type Row = Record<string, any>;
type Page =
  | "overview"
  | "providers"
  | "machines"
  | "agents"
  | "probes"
  | "dns"
  | "rotations"
  | "logs"
  | "settings";
const nav: [Page, string, string][] = [
  ["overview", "运行总览", "⌁"],
  ["providers", "服务商", "◇"],
  ["machines", "云主机", "▣"],
  ["agents", "节点代理", "⌘"],
  ["probes", "探测规则", "⌁"],
  ["dns", "DNS 同步", "◎"],
  ["rotations", "换 IP 记录", "↻"],
  ["logs", "审计日志", "≡"],
  ["settings", "系统设置", "⚙"],
];
function localizeError(message: string) {
  const rules: [RegExp, string][] = [
    [/invalid credentials/i, "账号或密码错误"],
    [/authentication required/i, "登录状态已失效，请重新登录"],
    [/machine not found/i, "找不到指定云主机"],
    [/provider not found/i, "找不到指定服务商"],
    [/agent not found/i, "找不到指定节点"],
    [/probe not found/i, "找不到指定探测规则"],
    [/no enabled probes configured/i, "当前主机没有启用的探测规则"],
    [/rotation already in progress/i, "该主机正在更换 IP，请勿重复操作"],
    [/daily rotation limit reached/i, "已达到每日换 IP 上限"],
    [/rotation cooldown is active/i, "换 IP 冷却时间尚未结束"],
    [/database error/i, "数据库操作失败"],
    [/invalid request/i, "请求内容无效"],
    [/could not save/i, "保存失败，请稍后重试"],
    [/networkerror|failed to fetch/i, "网络连接失败，请检查控制器状态"],
  ];
  return rules.find(([pattern]) => pattern.test(message))?.[1] || message;
}
async function api(path: string, init?: RequestInit) {
  const r = await fetch(path, {
    credentials: "same-origin",
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
  });
  if (r.status === 204) return null;
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(localizeError(data.error || `HTTP ${r.status}`));
  return data;
}
function useData(path: string, deps: any[] = []) {
  const [data, setData] = useState<any>(null);
  const [error, setError] = useState("");
  const load = useCallback(
    () =>
      api(path)
        .then(setData)
        .catch((e) => setError(e.message)),
    [path, ...deps],
  );
  useEffect(() => {
    load();
  }, [load]);
  return { data, error, load };
}
function time(v?: string) {
  return v ? new Date(v).toLocaleString("zh-CN", { hour12: false }) : "—";
}
function Badge({ value }: { value: string }) {
  const v = (value || "unknown").toLowerCase();
  const labels: Record<string, string> = {
    online: "在线",
    offline: "离线",
    healthy: "健康",
    unhealthy: "异常",
    degraded: "降级",
    suspect: "疑似异常",
    completed: "已完成",
    failed: "失败",
    running: "运行中",
    pending: "等待中",
    enabled: "已启用",
    disabled: "已停用",
    revoked: "已撤销",
    untested: "未测试",
    unknown: "未知",
    ok: "正常",
  };
  return (
    <span
      className={`badge ${["online", "healthy", "completed", "ok", "running"].includes(v) ? "good" : ["failed", "offline", "unhealthy", "error", "revoked"].includes(v) ? "bad" : "warn"}`}
    >
      <i />
      {labels[v] || value || "未知"}
    </span>
  );
}
function Empty({ text }: { text: string }) {
  return (
    <div className="empty">
      <div>◇</div>
      <p>{text}</p>
    </div>
  );
}
function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
}) {
  return (
    <div
      className="shade"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div className="modal">
        <header>
          <h2>{title}</h2>
          <button className="icon" onClick={onClose}>
            ×
          </button>
        </header>
        {children}
      </div>
    </div>
  );
}
function Toast({ message }: { message: string }) {
  return message ? <div className="toast">{message}</div> : null;
}

function Login({ done }: { done: (mustChange: boolean) => void }) {
  const [username, setUsername] = useState("admin"),
    [password, setPassword] = useState(""),
    [error, setError] = useState("");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    try {
      const v = await api("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      });
      done(!!v.mustChangePassword);
    } catch (x: any) {
      setError(x.message);
    }
  }
  return (
    <main className="login">
      <section className="login-art">
        <div className="brand-mark">R</div>
        <h1>
          Rotato<span>Pilot</span>
        </h1>
        <p>自动探测，安全换 IP，基础设施尽在掌控。</p>
        <div className="orbit">
          <i />
          <i />
          <i />
        </div>
      </section>
      <form onSubmit={submit} className="login-card">
        <small>ROTATOPILOT 控制台</small>
        <h2>欢迎回来</h2>
        <p>登录后管理云主机、探测规则与 IP 轮换。</p>
        <label>
          管理员账号
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
          />
        </label>
        <label>
          密码
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {error && <div className="form-error">{error}</div>}
        <button className="primary">进入控制台 →</button>
        <em>使用安全的 HTTP-only 会话保护</em>
      </form>
    </main>
  );
}
function PasswordChange({ done }: { done: () => void }) {
  const [currentPassword, setCurrent] = useState(""),
    [newPassword, setNew] = useState(""),
    [confirm, setConfirm] = useState(""),
    [error, setError] = useState("");
  return (
    <div className="shade">
      <div className="modal">
        <header>
          <h2>保护管理员账户</h2>
        </header>
        <form
          className="form"
          onSubmit={async (e) => {
            e.preventDefault();
            if (newPassword !== confirm) {
              setError("两次输入的新密码不一致");
              return;
            }
            try {
              await api("/api/auth/password", {
                method: "POST",
                body: JSON.stringify({ currentPassword, newPassword }),
              });
              done();
            } catch (x: any) {
              setError(x.message);
            }
          }}
        >
          <div className="warning">
            当前使用的是默认密码。继续使用控制台前，请设置一个至少 12
            位的独立强密码。
          </div>
          <label>
            当前密码
            <input
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrent(e.target.value)}
              autoFocus
            />
          </label>
          <label>
            新密码
            <input
              type="password"
              minLength={12}
              value={newPassword}
              onChange={(e) => setNew(e.target.value)}
            />
          </label>
          <label>
            再次输入新密码
            <input
              type="password"
              minLength={12}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </label>
          {error && <div className="form-error">{error}</div>}
          <footer>
            <button className="primary" disabled={newPassword.length < 12}>
              保存新密码
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}

function Overview() {
  const { data, load } = useData("/api/overview");
  const m = useData("/api/machines");
  const r = useData("/api/rotations?limit=5");
  return (
    <>
      <Title
        title="运行总览"
        sub="集中查看云主机、节点与 IP 轮换状态"
        action={
          <button
            onClick={() => {
              load();
              m.load();
              r.load();
            }}
          >
            刷新数据
          </button>
        }
      />
      <div className="stats">
        <Stat label="云主机" value={data?.machines ?? "—"} note="已纳入管理" />
        <Stat
          label="在线节点"
          value={data?.agentsOnline ?? "—"}
          note="心跳上报正常"
          good
        />
        <Stat
          label="需要关注"
          value={data?.unhealthy ?? "—"}
          note="疑似或确认异常"
        />
        <Stat
          label="今日换 IP"
          value={data?.rotationsToday ?? "—"}
          note="受每日上限保护"
        />
      </div>
      <div className="grid2">
        <Panel title="主机运行状态">
          <MachineTable rows={m.data || []} />
        </Panel>
        <Panel title="最近换 IP">
          {r.data?.length ? (
            <table>
              <thead>
                <tr>
                  <th>云主机</th>
                  <th>IP 变化</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {r.data.map((x: Row) => (
                  <tr key={x.id}>
                    <td>{x.machineId}</td>
                    <td className="mono">
                      {x.oldIp || "—"} → {x.newIp || "—"}
                    </td>
                    <td>
                      <Badge value={x.status} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Empty text="暂时没有换 IP 记录" />
          )}
        </Panel>
      </div>
    </>
  );
}
function Stat({
  label,
  value,
  note,
  good,
}: {
  label: string;
  value: any;
  note: string;
  good?: boolean;
}) {
  return (
    <article className="stat">
      <div>
        <small>{label}</small>
        <strong>{value}</strong>
        <p>{note}</p>
      </div>
      <span className={good ? "pulse" : ""}>↗</span>
    </article>
  );
}
function Title({
  title,
  sub,
  action,
}: {
  title: string;
  sub: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="title">
      <div>
        <h1>{title}</h1>
        <p>{sub}</p>
      </div>
      {action}
    </div>
  );
}
function Panel({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="panel">
      <h3>{title}</h3>
      {children}
    </section>
  );
}

function Providers({ toast }: { toast: (s: string) => void }) {
  const { data, load } = useData("/api/providers");
  const [open, setOpen] = useState(false);
  async function act(id: number, type: string) {
    try {
      const v = await api(`/api/providers/${id}/${type}`, { method: "POST" });
      toast(
        type === "test"
          ? `连接成功，共发现 ${v.machineCount} 台云主机`
          : `已同步 ${v.count} 台云主机`,
      );
      load();
    } catch (e: any) {
      toast(e.message);
    }
  }
  return (
    <>
      <Title
        title="服务商面板"
        sub="安全连接 VPS 服务商 API，敏感凭据全程加密"
        action={
          <button className="primary" onClick={() => setOpen(true)}>
            + 添加服务商
          </button>
        }
      />
      <Panel title="已配置的服务商">
        {data?.length ? (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>API 地址</th>
                <th>凭据</th>
                <th>连接状态</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {data.map((x: Row) => (
                <tr key={x.id}>
                  <td>
                    <b>{x.name}</b>
                  </td>
                  <td className="mono">{x.apiBaseUrl}</td>
                  <td>{x.tokenMasked}</td>
                  <td>
                    <Badge value={x.lastTestStatus || "untested"} />
                  </td>
                  <td className="actions">
                    <button onClick={() => act(x.id, "test")}>测试连接</button>
                    <button onClick={() => act(x.id, "sync-machines")}>
                      同步主机
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <Empty text="添加服务商后即可发现并同步云主机" />
        )}
      </Panel>
      {open && (
        <ProviderForm
          close={() => setOpen(false)}
          done={() => {
            setOpen(false);
            load();
            toast("服务商已安全保存");
          }}
        />
      )}
    </>
  );
}
function ProviderForm({
  close,
  done,
}: {
  close: () => void;
  done: () => void;
}) {
  const [v, setV] = useState({
      name: "",
      apiBaseUrl: "",
      token: "",
      tokenType: "raw_authorization",
    }),
    [error, setError] = useState("");
  return (
    <Modal title="添加服务商面板" onClose={close}>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api("/api/providers", {
              method: "POST",
              body: JSON.stringify(v),
            });
            done();
          } catch (x: any) {
            setError(x.message);
          }
        }}
        className="form"
      >
        <label>
          显示名称
          <input
            required
            value={v.name}
            onChange={(e) => setV({ ...v, name: e.target.value })}
            placeholder="Linus AWS Panel"
          />
        </label>
        <label>
          API 基础地址
          <input
            required
            type="url"
            value={v.apiBaseUrl}
            onChange={(e) => setV({ ...v, apiBaseUrl: e.target.value })}
            placeholder="https://panel.example.com"
          />
        </label>
        <label>
          授权 Token
          <input
            required
            type="password"
            value={v.token}
            onChange={(e) => setV({ ...v, token: e.target.value })}
          />
          <small>Token 会在写入数据库前加密，保存后不会再返回到浏览器。</small>
        </label>
        <label>
          授权方式
          <select
            value={v.tokenType}
            onChange={(e) => setV({ ...v, tokenType: e.target.value })}
          >
            <option value="raw_authorization">原始 Authorization 请求头</option>
            <option value="bearer">Bearer Token</option>
          </select>
        </label>
        {error && <div className="form-error">{error}</div>}
        <footer>
          <button type="button" onClick={close}>
            取消
          </button>
          <button className="primary">保存服务商</button>
        </footer>
      </form>
    </Modal>
  );
}

function MachineTable({
  rows,
  onAgent,
  onRotate,
  onCheck,
}: {
  rows: Row[];
  onAgent?: (x: Row) => void;
  onRotate?: (x: Row) => void;
  onCheck?: (x: Row) => void;
}) {
  return rows.length ? (
    <table>
      <thead>
        <tr>
          <th>云主机</th>
          <th>区域</th>
          <th>公网 IP</th>
          <th>健康状态</th>
          <th>自动换 IP</th>
          {onAgent && <th />}
        </tr>
      </thead>
      <tbody>
        {rows.map((x) => (
          <tr key={x.id}>
            <td>
              <b>{x.name || x.id}</b>
              <small className="block mono">{x.id}</small>
            </td>
            <td>{x.region || "—"}</td>
            <td className="mono">{x.publicIPv4 || "—"}</td>
            <td>
              <Badge value={x.healthStatus} />
            </td>
            <td>{x.autoRotateEnabled ? "已启用" : "未启用"}</td>
            {onAgent && (
              <td className="actions">
                <button onClick={() => onCheck?.(x)}>立即探测</button>
                <button onClick={() => onAgent(x)}>安装节点</button>
                <button className="danger" onClick={() => onRotate?.(x)}>
                  更换 IP
                </button>
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  ) : (
    <Empty text="尚未同步云主机" />
  );
}
function Machines({ toast }: { toast: (s: string) => void }) {
  const { data, load } = useData("/api/machines");
  const [agent, setAgent] = useState<Row | null>(null),
    [rotate, setRotate] = useState<Row | null>(null),
    [generated, setGenerated] = useState<Row | null>(null);
  async function check(x: Row) {
    try {
      const v = await api(`/api/machines/${x.id}/check`, { method: "POST" });
      toast(`探测完成，失败评分：${v.failureScore}`);
      load();
    } catch (e: any) {
      toast(e.message);
    }
  }
  return (
    <>
      <Title title="云主机" sub="管理从服务商面板同步的实例与公网 IP" />
      <Panel title="主机列表">
        <MachineTable
          rows={data || []}
          onAgent={setAgent}
          onRotate={setRotate}
          onCheck={check}
        />
      </Panel>
      {agent && (
        <Modal
          title={`安装节点代理 · ${agent.name || agent.id}`}
          onClose={() => setAgent(null)}
        >
          <div className="modal-body">
            <p>
              生成仅限当前主机使用的节点 Token。Token
              只显示一次，服务端仅保存哈希值。
            </p>
            {generated ? (
              <>
                <label>
                  一键安装命令
                  <Copy text={generated.installCommand} />
                </label>
                <label>
                  Docker 运行命令
                  <Copy text={generated.dockerCommand} />
                </label>
              </>
            ) : (
              <button
                className="primary"
                onClick={async () => {
                  try {
                    setGenerated(
                      await api(
                        `/api/machines/${agent.id}/generate-agent-token`,
                        { method: "POST" },
                      ),
                    );
                  } catch (e: any) {
                    toast(e.message);
                  }
                }}
              >
                生成节点凭据
              </button>
            )}
          </div>
        </Modal>
      )}
      {rotate && (
        <Rotate
          machine={rotate}
          close={() => setRotate(null)}
          done={() => {
            setRotate(null);
            load();
            toast("IP 更换完成");
          }}
        />
      )}
    </>
  );
}
function Copy({ text }: { text: string }) {
  return (
    <div className="copy">
      <code>{text}</code>
      <button onClick={() => navigator.clipboard.writeText(text)}>复制</button>
    </div>
  );
}
function Rotate({
  machine,
  close,
  done,
}: {
  machine: Row;
  close: () => void;
  done: () => void;
}) {
  const [confirm, setConfirm] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  return (
    <Modal title="确认手动更换 IP" onClose={close}>
      <form
        className="form"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api(`/api/machines/${machine.id}/change-ip`, {
              method: "POST",
              body: JSON.stringify({
                confirmMachineId: confirm,
                reason: "manual dashboard request",
              }),
            });
            done();
          } catch (x: any) {
            setError(x.message);
            setBusy(false);
          }
        }}
      >
        <div className="warning">
          此操作会调用服务商 API，可能造成短暂断线。只有明确启用的 DNS
          记录才会同步。
        </div>
        <label>
          请输入主机 ID 以确认：<code>{machine.id}</code>
          <input value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </label>
        {error && <div className="form-error">{error}</div>}
        <footer>
          <button type="button" onClick={close}>
            取消
          </button>
          <button
            disabled={confirm !== machine.id || busy}
            className="danger primary"
          >
            {busy ? "正在更换…" : "确认更换 IP"}
          </button>
        </footer>
      </form>
    </Modal>
  );
}

function Agents({ toast }: { toast: (s: string) => void }) {
  const { data, load } = useData("/api/agents");
  return (
    <>
      <Title
        title="节点代理"
        sub="节点凭据仅绑定单台主机，服务商密钥永远不会下发"
      />
      <Panel title="已注册节点">
        {data?.length ? (
          <table>
            <thead>
              <tr>
                <th>节点 ID</th>
                <th>云主机</th>
                <th>系统信息</th>
                <th>版本</th>
                <th>最后心跳</th>
                <th>状态</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {data.map((x: Row) => (
                <tr key={x.id}>
                  <td className="mono">{x.id}</td>
                  <td className="mono">{x.machineId}</td>
                  <td>
                    {x.hostname || "—"}
                    <small className="block">
                      {x.os} · {x.arch}
                    </small>
                  </td>
                  <td>{x.agentVersion || "—"}</td>
                  <td>{time(x.lastHeartbeatAt)}</td>
                  <td>
                    <Badge value={x.status} />
                  </td>
                  <td>
                    <button
                      disabled={x.revoked}
                      onClick={async () => {
                        await api(`/api/agents/${x.id}/revoke`, {
                          method: "POST",
                        });
                        load();
                        toast("节点凭据已撤销");
                      }}
                    >
                      撤销
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <Empty text="请先从云主机页面生成并安装节点" />
        )}
      </Panel>
    </>
  );
}

function Probes({ toast }: { toast: (s: string) => void }) {
  const p = useData("/api/probes"),
    m = useData("/api/machines");
  const [open, setOpen] = useState(false);
  return (
    <>
      <Title
        title="探测规则"
        sub="通过多信号加权判断，避免单次网络抖动触发换 IP"
        action={
          <button className="primary" onClick={() => setOpen(true)}>
            + 添加规则
          </button>
        }
      />
      <Panel title="已配置规则">
        {p.data?.length ? (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>云主机</th>
                <th>来源</th>
                <th>目标</th>
                <th>失败权重</th>
                <th>状态</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {p.data.map((x: Row) => (
                <tr key={x.id}>
                  <td>
                    <b>{x.name}</b>
                    <small className="block">
                      每 {x.intervalSeconds} 秒 · 超时 {x.timeoutMs} 毫秒
                    </small>
                  </td>
                  <td className="mono">{x.machineId}</td>
                  <td>
                    {x.source} / {x.type}
                  </td>
                  <td className="mono">
                    {x.url || `${x.targetHost || "主机 IP"}:${x.targetPort}`}
                  </td>
                  <td>{x.failureWeight}</td>
                  <td>
                    <Badge value={x.enabled ? "enabled" : "disabled"} />
                  </td>
                  <td>
                    <button
                      onClick={async () => {
                        try {
                          const v = await api(`/api/probes/${x.id}/run`, {
                            method: "POST",
                          });
                          toast(
                            v.success
                              ? `探测通过，耗时 ${v.latencyMs}ms`
                              : v.error,
                          );
                        } catch (e: any) {
                          toast(e.message);
                        }
                      }}
                    >
                      执行
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <Empty text="尚未配置探测规则，自动换 IP 暂无判断信号" />
        )}
      </Panel>
      {open && (
        <ProbeForm
          machines={m.data || []}
          close={() => setOpen(false)}
          done={() => {
            setOpen(false);
            p.load();
            toast("探测规则已创建");
          }}
        />
      )}
    </>
  );
}
function ProbeForm({
  machines,
  close,
  done,
}: {
  machines: Row[];
  close: () => void;
  done: () => void;
}) {
  const [v, setV] = useState({
      machineId: machines[0]?.id || "",
      name: "TCP 443",
      source: "controller",
      type: "tcp",
      targetHost: "",
      targetPort: 443,
      url: "",
      expectedStatus: "200-399",
      timeoutMs: 5000,
      intervalSeconds: 300,
      failureWeight: 2,
      enabled: true,
    }),
    [error, setError] = useState("");
  return (
    <Modal title="添加探测规则" onClose={close}>
      <form
        className="form"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api("/api/probes", {
              method: "POST",
              body: JSON.stringify(v),
            });
            done();
          } catch (x: any) {
            setError(x.message);
          }
        }}
      >
        <div className="cols">
          <label>
            云主机
            <select
              value={v.machineId}
              onChange={(e) => setV({ ...v, machineId: e.target.value })}
            >
              {machines.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.name || x.id}
                </option>
              ))}
            </select>
          </label>
          <label>
            规则名称
            <input
              value={v.name}
              onChange={(e) => setV({ ...v, name: e.target.value })}
            />
          </label>
          <label>
            探测来源
            <select
              value={v.source}
              onChange={(e) => setV({ ...v, source: e.target.value })}
            >
              <option value="controller">控制器</option>
              <option value="agent">本机节点</option>
              <option value="external-agent">外部节点</option>
            </select>
          </label>
          <label>
            探测类型
            <select
              value={v.type}
              onChange={(e) => setV({ ...v, type: e.target.value })}
            >
              <option value="tcp">TCP 端口</option>
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
              <option value="icmp">ICMP</option>
            </select>
          </label>
          <label>
            目标主机
            <input
              value={v.targetHost}
              onChange={(e) => setV({ ...v, targetHost: e.target.value })}
              placeholder="留空则使用云主机公网 IP"
            />
          </label>
          <label>
            端口
            <input
              type="number"
              value={v.targetPort}
              onChange={(e) => setV({ ...v, targetPort: +e.target.value })}
            />
          </label>
          <label>
            失败权重
            <input
              type="number"
              value={v.failureWeight}
              onChange={(e) => setV({ ...v, failureWeight: +e.target.value })}
            />
          </label>
          <label>
            间隔（秒）
            <input
              type="number"
              value={v.intervalSeconds}
              onChange={(e) => setV({ ...v, intervalSeconds: +e.target.value })}
            />
          </label>
        </div>
        {error && <div className="form-error">{error}</div>}
        <footer>
          <button type="button" onClick={close}>
            取消
          </button>
          <button className="primary">创建规则</button>
        </footer>
      </form>
    </Modal>
  );
}

function DNS({ toast }: { toast: (s: string) => void }) {
  const providers = useData("/api/dns-providers"),
    records = useData("/api/dns-records"),
    machines = useData("/api/machines");
  const [mode, setMode] = useState("");
  return (
    <>
      <Title
        title="DNS 同步"
        sub="IP 更换成功后，可按需自动更新域名解析"
        action={
          <div className="actions">
            <button onClick={() => setMode("provider")}>+ DNS 服务商</button>
            <button className="primary" onClick={() => setMode("record")}>
              + DNS 记录
            </button>
          </div>
        }
      />
      <div className="grid2">
        <Panel title="DNS 服务商">
          {providers.data?.length ? (
            <table>
              <tbody>
                {providers.data.map((x: Row) => (
                  <tr key={x.id}>
                    <td>
                      <b>{x.name}</b>
                      <small className="block">{x.providerType}</small>
                    </td>
                    <td>{x.tokenMasked || "未设置 Token"}</td>
                    <td>
                      <Badge value={x.enabled ? "enabled" : "disabled"} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Empty text="在此添加 Cloudflare 或 Webhook 凭据" />
          )}
        </Panel>
        <Panel title="已绑定记录">
          {records.data?.length ? (
            <table>
              <tbody>
                {records.data.map((x: Row) => (
                  <tr key={x.id}>
                    <td>
                      <b>{x.recordName}</b>
                      <small className="block">
                        {x.recordType} · {x.machineId}
                      </small>
                    </td>
                    <td className="mono">{x.lastIp || "尚未同步"}</td>
                    <td>
                      <button
                        onClick={async () => {
                          try {
                            await api(`/api/dns-records/${x.id}/sync`, {
                              method: "POST",
                            });
                            records.load();
                            toast("DNS 记录同步成功");
                          } catch (e: any) {
                            toast(e.message);
                          }
                        }}
                      >
                        同步
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Empty text="添加并启用记录后才会执行 DNS 同步" />
          )}
        </Panel>
      </div>
      {mode === "provider" && (
        <DNSProviderForm
          close={() => setMode("")}
          done={() => {
            setMode("");
            providers.load();
            toast("DNS 服务商已安全保存");
          }}
        />
      )}
      {mode === "record" && (
        <DNSRecordForm
          machines={machines.data || []}
          providers={providers.data || []}
          close={() => setMode("")}
          done={() => {
            setMode("");
            records.load();
            toast("DNS 记录已创建");
          }}
        />
      )}
    </>
  );
}
function DNSProviderForm({
  close,
  done,
}: {
  close: () => void;
  done: () => void;
}) {
  const [v, setV] = useState({
    name: "Cloudflare",
    providerType: "cloudflare",
    token: "",
    extraConfigJSON: "{}",
    enabled: true,
  });
  return (
    <Modal title="添加 DNS 服务商" onClose={close}>
      <form
        className="form"
        onSubmit={async (e) => {
          e.preventDefault();
          await api("/api/dns-providers", {
            method: "POST",
            body: JSON.stringify(v),
          });
          done();
        }}
      >
        <label>
          名称
          <input
            value={v.name}
            onChange={(e) => setV({ ...v, name: e.target.value })}
          />
        </label>
        <label>
          服务商类型
          <select
            value={v.providerType}
            onChange={(e) => setV({ ...v, providerType: e.target.value })}
          >
            <option value="cloudflare">Cloudflare</option>
            <option value="webhook">通用 Webhook</option>
          </select>
        </label>
        <label>
          API Token
          <input
            type="password"
            value={v.token}
            onChange={(e) => setV({ ...v, token: e.target.value })}
          />
        </label>
        {v.providerType === "webhook" && (
          <label>
            Webhook 配置 JSON
            <textarea
              value={v.extraConfigJSON}
              onChange={(e) => setV({ ...v, extraConfigJSON: e.target.value })}
              placeholder='{"url":"https://...","method":"POST"}'
            />
          </label>
        )}
        <footer>
          <button type="button" onClick={close}>
            取消
          </button>
          <button className="primary">保存</button>
        </footer>
      </form>
    </Modal>
  );
}
function DNSRecordForm({
  machines,
  providers,
  close,
  done,
}: {
  machines: Row[];
  providers: Row[];
  close: () => void;
  done: () => void;
}) {
  const [v, setV] = useState({
    machineId: machines[0]?.id || "",
    dnsProviderId: providers[0]?.id || 0,
    recordName: "",
    recordType: "A",
    zoneId: "",
    proxied: false,
    ttl: 120,
    enabled: false,
    syncAfterRotation: false,
  });
  return (
    <Modal title="绑定 DNS 记录" onClose={close}>
      <form
        className="form"
        onSubmit={async (e) => {
          e.preventDefault();
          await api("/api/dns-records", {
            method: "POST",
            body: JSON.stringify(v),
          });
          done();
        }}
      >
        <div className="cols">
          <label>
            云主机
            <select
              value={v.machineId}
              onChange={(e) => setV({ ...v, machineId: e.target.value })}
            >
              {machines.map((x) => (
                <option value={x.id} key={x.id}>
                  {x.name || x.id}
                </option>
              ))}
            </select>
          </label>
          <label>
            DNS 服务商
            <select
              value={v.dnsProviderId}
              onChange={(e) => setV({ ...v, dnsProviderId: +e.target.value })}
            >
              {providers.map((x) => (
                <option value={x.id} key={x.id}>
                  {x.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            完整记录名称
            <input
              required
              value={v.recordName}
              onChange={(e) => setV({ ...v, recordName: e.target.value })}
            />
          </label>
          <label>
            Zone ID
            <input
              value={v.zoneId}
              onChange={(e) => setV({ ...v, zoneId: e.target.value })}
            />
          </label>
        </div>
        <label className="check">
          <input
            type="checkbox"
            checked={v.enabled}
            onChange={(e) => setV({ ...v, enabled: e.target.checked })}
          />{" "}
          启用此记录
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={v.syncAfterRotation}
            onChange={(e) =>
              setV({ ...v, syncAfterRotation: e.target.checked })
            }
          />{" "}
          更换 IP 后自动同步
        </label>
        <footer>
          <button type="button" onClick={close}>
            取消
          </button>
          <button className="primary">创建记录</button>
        </footer>
      </form>
    </Modal>
  );
}

function Rotations() {
  const { data } = useData("/api/rotations");
  return (
    <>
      <Title title="换 IP 记录" sub="完整记录每次公网 IP 变更及其执行结果" />
      <Panel title="全部记录">
        {data?.length ? (
          <table>
            <thead>
              <tr>
                <th>开始时间</th>
                <th>云主机</th>
                <th>IP 变化</th>
                <th>触发方式</th>
                <th>DNS</th>
                <th>后置探测</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              {data.map((x: Row) => (
                <tr key={x.id}>
                  <td>{time(x.startedAt)}</td>
                  <td className="mono">{x.machineId}</td>
                  <td className="mono">
                    {x.oldIp} → {x.newIp || "—"}
                  </td>
                  <td>{x.triggerType}</td>
                  <td>{x.dnsSyncStatus || "—"}</td>
                  <td>{x.postCheckStatus || "—"}</td>
                  <td>
                    <Badge value={x.status} />
                    {x.error && (
                      <small className="block badtext">{x.error}</small>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <Empty text="尚无 IP 更换记录" />
        )}
      </Panel>
    </>
  );
}
function Logs() {
  const { data, load } = useData("/api/logs");
  return (
    <>
      <Title
        title="审计日志"
        sub="结构化记录关键操作，所有敏感信息均已脱敏"
        action={<button onClick={load}>刷新</button>}
      />
      <Panel title="最新事件">
        {data?.length ? (
          <div className="loglist">
            {data.map((x: Row) => (
              <article key={x.id}>
                <span className={`level ${x.level}`}>{x.level}</span>
                <div>
                  <b>{x.message}</b>
                  <small>
                    {x.jobType} · {x.machineId || "控制器"} ·{" "}
                    {time(x.createdAt)}
                  </small>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <Empty text="暂时没有审计事件" />
        )}
      </Panel>
    </>
  );
}
function Settings() {
  return (
    <>
      <Title title="系统设置" sub="查看当前安全策略与运行默认值" />
      <div className="grid2">
        <Panel title="安全默认值">
          <dl>
            <dt>自动换 IP</dt>
            <dd>
              <Badge value="默认关闭" />
            </dd>
            <dt>确认探测</dt>
            <dd>必须通过</dd>
            <dt>换 IP 冷却时间</dt>
            <dd>30 分钟</dd>
            <dt>每日上限</dt>
            <dd>每台主机 10 次</dd>
          </dl>
        </Panel>
        <Panel title="安全状态">
          <p>
            服务商及 DNS 凭据使用 AES-GCM 加密。节点 Token
            仅绑定单台主机、只显示一次，服务端仅保存 SHA-256 哈希。
          </p>
          <div className="warning">
            对外开放前请使用强管理员密码，并确保通过可信的 HTTPS 反向代理访问。
          </div>
        </Panel>
      </div>
    </>
  );
}

function App() {
  const [auth, setAuth] = useState<boolean | null>(null),
    [mustChange, setMustChange] = useState(false),
    [page, setPage] = useState<Page>("overview"),
    [toast, setToast] = useState(""),
    [live, setLive] = useState(false);
  const show = (s: string) => {
    setToast(s);
    setTimeout(() => setToast(""), 4000);
  };
  useEffect(() => {
    api("/api/auth/me")
      .then((v) => {
        setAuth(true);
        setMustChange(!!v.mustChangePassword);
      })
      .catch(() => setAuth(false));
  }, []);
  useEffect(() => {
    if (!auth) return;
    let ws: WebSocket, t: any;
    const connect = () => {
      ws = new WebSocket(
        `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`,
      );
      ws.onopen = () => setLive(true);
      ws.onmessage = (e) => {
        const v = JSON.parse(e.data);
        if (v.event === "rotation.completed")
          show(`IP 更换完成：${v.data.newIp}`);
      };
      ws.onclose = () => {
        setLive(false);
        t = setTimeout(connect, 3000);
      };
    };
    connect();
    return () => {
      clearTimeout(t);
      ws?.close();
    };
  }, [auth]);
  if (auth === null) return <div className="boot">RotatoPilot</div>;
  if (!auth)
    return (
      <Login
        done={(required) => {
          setAuth(true);
          setMustChange(required);
        }}
      />
    );
  const content = {
    overview: <Overview />,
    providers: <Providers toast={show} />,
    machines: <Machines toast={show} />,
    agents: <Agents toast={show} />,
    probes: <Probes toast={show} />,
    dns: <DNS toast={show} />,
    rotations: <Rotations />,
    logs: <Logs />,
    settings: <Settings />,
  }[page];
  return (
    <>
      <div className="shell">
        <aside>
          <div className="logo">
            <b>R</b>
            <span>
              Rotato<em>Pilot</em>
            </span>
          </div>
          <nav>
            {nav.map(([id, label, icon]) => (
              <button
                className={page === id ? "active" : ""}
                onClick={() => setPage(id)}
                key={id}
              >
                <i>{icon}</i>
                {label}
              </button>
            ))}
          </nav>
          <div className="side-foot">
            <span className={live ? "live" : ""} />
            {live ? "实时连接正常" : "正在重新连接…"}
            <button
              onClick={async () => {
                await api("/api/auth/logout", { method: "POST" });
                setAuth(false);
              }}
            >
              退出登录
            </button>
          </div>
        </aside>
        <main className="content">{content}</main>
        <Toast message={toast} />
      </div>
      {mustChange && (
        <PasswordChange
          done={() => {
            setMustChange(false);
            show("管理员密码已更新");
          }}
        />
      )}
    </>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
