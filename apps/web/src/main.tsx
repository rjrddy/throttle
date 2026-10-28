import "./motion";
import { useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import "./style.css";

type Policy = {
  mode: "fixed" | "adaptive";
  algorithm: "token" | "sliding" | "gcra";
  rate: number;
  burst: number;
};
type Client = {
  id: string;
  attempts: number;
  success: number;
  limited: number;
  capacity_rejected: number;
  origin_errors: number;
  client_errors: number;
  circuit_rejected: number;
  error_ewma: number;
  pressure: number;
  unknown_share: number;
  score: number;
  multiplier: number;
  effective_rate: number;
  reason: string;
};
type Event = {
  at: number;
  client: string;
  endpoint: string;
  outcome: string;
  status: number;
  latency_ms: number;
};
type Bucket = {
  second: number;
  success: number;
  limited: number;
  capacity: number;
  failed: number;
};
type ClassResult = {
  offered: number;
  success: number;
  limited: number;
  capacity: number;
  other: number;
  success_rate: number;
  p99_ms: number;
};
type Result = {
  mode: string;
  algorithm: string;
  classes: Record<string, ClassResult>;
  elapsed_ms: number;
};
type State = {
  engine: {
    policy: Policy;
    version: number;
    clients: Client[];
    events: Event[];
    timeline: Bucket[];
  };
  inflight: number;
  capacity: number;
  breaker: string;
  campaign_busy: boolean;
  campaign_result: Result | null;
  campaign_error: string;
  demo_enabled: boolean;
  origin_mode: string;
};
const integer = (n: number) => n.toLocaleString();
const percent = (n: number) => `${(n * 100).toFixed(1)}%`;
const outcomeLabel: Record<string, string> = {
  success: "Passed",
  limited: "Rate limited",
  capacity: "Capacity protected",
  circuit: "Circuit open",
  client_error: "Client error",
  origin_error: "Origin error",
};

function Traffic({ buckets }: { buckets: Bucket[] }) {
  const max = Math.max(
    1,
    ...buckets.map((b) => b.success + b.limited + b.capacity + b.failed),
  );
  return (
    <div className="traffic-chart">
      <svg
        viewBox="0 0 1000 170"
        role="img"
        aria-label="Actual request outcomes by completion second, rolling sixty-second window"
      >
        {[0, 1, 2, 3].map((n) => (
          <line
            key={n}
            x1="0"
            x2="1000"
            y1={n * 45 + 10}
            y2={n * 45 + 10}
            className="gridline"
          />
        ))}
        {buckets.map((b, i) => {
          let bottom = 150;
          return (
            <g key={b.second}>
              <title>
                {new Date(b.second * 1000).toLocaleTimeString()}: {b.success}{" "}
                passed, {b.limited} limited, {b.capacity} capacity, {b.failed}{" "}
                other
              </title>
              {(["success", "limited", "capacity", "failed"] as const).map(
                (kind) => {
                  const h = (b[kind] / max) * 130;
                  bottom -= h;
                  return (
                    <rect
                      key={kind}
                      x={(60 - buckets.length + i) * 16.6 + 1}
                      y={bottom}
                      width="12"
                      height={h}
                      className={`bar-${kind}`}
                    />
                  );
                },
              )}
            </g>
          );
        })}
      </svg>
      <div className="chart-axis">
        <span>−60 seconds</span>
        <span>SINCE PROCESS START · SCALE {max}/s</span>
        <span>now</span>
      </div>
    </div>
  );
}
function App() {
  const [token, setToken] = useState("");
  const [state, setState] = useState<State | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState("");
  const [policy, setPolicy] = useState<Policy>({
    mode: "adaptive",
    algorithm: "token",
    rate: 20,
    burst: 12,
  });
  const request = useCallback(
    async (path: string, method = "GET", body?: unknown) => {
      const response = await fetch(path, {
        method,
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
      });
      if (!response.ok)
        throw new Error(`${response.status}: ${await response.text()}`);
      return response.json();
    },
    [token],
  );
  const refresh = useCallback(async () => {
    try {
      const next = (await request("/api/state")) as State;
      setState(next);
      setError("");
    } catch (e) {
      setError(String(e));
    }
  }, [request]);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 1000);
    return () => clearInterval(timer);
  }, [refresh]);
  useEffect(() => {
    if (state) setPolicy(state.engine.policy);
  }, [state?.engine.version]);
  async function action(path: string, method: string, body?: unknown) {
    setPending(true);
    setNotice("");
    try {
      await request(path, method, body);
      await refresh();
      setNotice(
        path === "/api/campaign"
          ? "Synthetic traffic started. Every result below comes from an actual HTTP response."
          : "Change applied to the live engine.",
      );
    } catch (e) {
      setNotice(String(e));
    } finally {
      setPending(false);
    }
  }
  const clients = state?.engine.clients ?? [];
  const focused = clients.find((c) => c.id === selected) ?? clients[0];
  const total = (field: keyof Client) =>
    clients.reduce((sum, c) => sum + Number(c[field]), 0);
  const busy = pending || Boolean(state?.campaign_busy);
  const last = state?.campaign_result?.classes.legitimate;
  return (
    <>
      <header className="topbar">
        <a href="/" className="brand">
          <svg width="31" height="31" viewBox="0 0 31 31" aria-hidden="true">
            <path
              d="M3 3h10v10H3zm15 0h10v10H18zM3 18h10v10H3zm15 0h10v10H18z"
              fill="currentColor"
            />
          </svg>
          throttle<span>↗</span>
        </a>
        <span className="top-label">TRAFFIC CONTROL / ONE INSTANCE</span>
        <div className={`connection ${error ? "bad" : ""}`} role="status">
          {error ? "Disconnected" : state ? "Live connection" : "Connecting"}
          <span className="connection-dot" />
        </div>
      </header>
      <main>
        <section className="hero">
          <div className="hero-copy">
            <p className="kicker">ADMISSION, MADE VISIBLE.</p>
            <h1>
              Room for
              <br />
              <span>good traffic.</span>
            </h1>
            <p className="hero-description">
              Shape the surge. Protect the origin. See exactly why a request
              gets through.
            </p>
          </div>
          <div className="hero-right">
            <div className="route-sign">
              <span>01 / LIVE GATEWAY</span>
              <svg viewBox="0 0 250 130" aria-hidden="true">
                <path d="M0 20h90c30 0 30 45 60 45h100M0 65h250M0 110h90c30 0 30-45 60-45" />
                <path d="m225 45 20 20-20 20" />
              </svg>
              <p>
                Bursts are a signal.
                <br />
                They aren’t a verdict.
              </p>
            </div>
            <label className="token-label">
              Operator token, if configured
              <input
                type="password"
                autoComplete="off"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Held in this tab only"
              />
            </label>
          </div>
        </section>
        {error && (
          <div className="alert" role="alert">
            {error}
          </div>
        )}
        <section className="switchboard">
          <div className="section-number">A</div>
          <div className="section-title">
            <h2>The admission desk</h2>
            <p>
              Policy changes reset budgets and signals. Outcome counters remain.
            </p>
          </div>
          <form
            className="policy-form"
            onSubmit={(e) => {
              e.preventDefault();
              void action("/api/policy", "PUT", policy);
            }}
          >
            <fieldset>
              <legend>Policy</legend>
              {(["fixed", "adaptive"] as const).map((mode) => (
                <label
                  className={`radio ${policy.mode === mode ? "checked" : ""}`}
                  key={mode}
                >
                  <input
                    type="radio"
                    name="mode"
                    value={mode}
                    checked={policy.mode === mode}
                    onChange={() => setPolicy({ ...policy, mode })}
                    disabled={busy}
                  />
                  {mode}
                </label>
              ))}
            </fieldset>
            <div className="field">
              <label htmlFor="algorithm">Algorithm</label>
              <select
                id="algorithm"
                value={policy.algorithm}
                disabled={busy}
                onChange={(e) =>
                  setPolicy({
                    ...policy,
                    algorithm: e.target.value as Policy["algorithm"],
                  })
                }
              >
                <option value="token">Token bucket</option>
                <option value="sliding">Rolling window</option>
                <option value="gcra">GCRA</option>
              </select>
            </div>
            <div className="field">
              <label htmlFor="base-rate">Rate / second</label>
              <input
                id="base-rate"
                type="number"
                min="1"
                max="1000"
                step="1"
                value={policy.rate}
                disabled={busy}
                onChange={(e) =>
                  setPolicy({ ...policy, rate: Number(e.target.value) })
                }
              />
            </div>
            <div className="field">
              <label htmlFor="burst">Burst</label>
              <input
                id="burst"
                type="number"
                min="1"
                max="1000"
                value={policy.burst}
                disabled={busy}
                onChange={(e) =>
                  setPolicy({ ...policy, burst: Number(e.target.value) })
                }
              />
            </div>
            <button type="submit" disabled={!state || busy}>
              Apply policy ↗
            </button>
          </form>
        </section>
        <section className="flow" aria-label="Live admission counters">
          <div className="flow-stage">
            <span className="flow-label">REGISTERED CLIENTS</span>
            <strong>{clients.length || "—"}</strong>
            <small>{integer(total("attempts"))} authenticated attempts</small>
          </div>
          <div className="flow-arrow" aria-hidden="true">
            →
          </div>
          <div className="flow-stage">
            <span className="flow-label">PASSED TO A RESULT</span>
            <strong>{integer(total("success"))}</strong>
            <small>successful responses since process start</small>
          </div>
          <div className="flow-arrow" aria-hidden="true">
            →
          </div>
          <div className="flow-stage">
            <span className="flow-label">ORIGIN CAPACITY</span>
            <strong>
              {state ? `${state.inflight} / ${state.capacity}` : "—"}
            </strong>
            <small>active requests / concurrency cap</small>
          </div>
          <div className="breaker">
            <span className="flow-label">CIRCUIT</span>
            <strong>{state?.breaker ?? "—"}</strong>
            <small>origin faults only; client errors excluded</small>
          </div>
        </section>
        <section className="timeline-section">
          <div className="section-heading">
            <div>
              <span className="section-number">B</span>
              <h2>The last minute</h2>
            </div>
            <div className="legend">
              <span>
                <i className="success" />
                Passed
              </span>
              <span>
                <i className="limited" />
                Rate limited
              </span>
              <span>
                <i className="capacity" />
                Capacity
              </span>
              <span>
                <i className="failed" />
                Other
              </span>
            </div>
          </div>
          <Traffic buckets={state?.engine.timeline ?? []} />
          <div className="totals-strip">
            <span>
              <b>{integer(total("limited"))}</b> per-client denials
            </span>
            <span>
              <b>{integer(total("capacity_rejected"))}</b> capacity rejections
            </span>
            <span>
              <b>{integer(total("client_errors"))}</b> origin 4xx responses
            </span>
            <span>
              <b>{integer(total("origin_errors"))}</b> origin failures
            </span>
          </div>
        </section>
        <section className="client-section">
          <div className="section-heading">
            <div>
              <span className="section-number">C</span>
              <h2>Every client has a reason</h2>
            </div>
            <p>
              Pressure + client errors + unclassified endpoint share. Select a
              row to inspect.
            </p>
          </div>
          <div className="client-layout">
            <div className="client-table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>CLIENT</th>
                    <th>SUCCESS</th>
                    <th>LIMITED</th>
                    <th>SCORE</th>
                    <th>RATE / S</th>
                    <th>BUDGET</th>
                  </tr>
                </thead>
                <tbody>
                  {clients.map((c) => (
                    <tr
                      key={c.id}
                      className={focused?.id === c.id ? "selected" : ""}
                    >
                      <td>
                        <button
                          className="client-button"
                          onClick={() => setSelected(c.id)}
                          aria-pressed={focused?.id === c.id}
                        >
                          {c.id}
                          <span>↗</span>
                        </button>
                      </td>
                      <td>{integer(c.success)}</td>
                      <td>{integer(c.limited)}</td>
                      <td>{c.score.toFixed(2)}</td>
                      <td>{c.effective_rate.toFixed(1)}</td>
                      <td>
                        <svg
                          viewBox="0 0 70 10"
                          className="budget"
                          role="img"
                          aria-label={`${percent(c.multiplier)} of base budget`}
                        >
                          <rect
                            width="70"
                            height="10"
                            className="budget-track"
                          />
                          <rect
                            width={70 * c.multiplier}
                            height="10"
                            className="budget-value"
                          />
                        </svg>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <aside className="client-inspector">
              <span className="inspector-label">DECISION NOTES</span>
              <h3>{focused?.id ?? "No client selected"}</h3>
              <p className="reason">
                {focused?.reason ?? "Waiting for the live registry."}
              </p>
              <dl>
                <dt>Request pressure</dt>
                <dd>{focused ? percent(focused.pressure) : "—"}</dd>
                <dt>Client-error EWMA</dt>
                <dd>{focused ? percent(focused.error_ewma) : "—"}</dd>
                <dt>Unclassified share</dt>
                <dd>{focused ? percent(focused.unknown_share) : "—"}</dd>
                <dt>Budget multiplier</dt>
                <dd>{focused ? `${focused.multiplier.toFixed(2)}×` : "—"}</dd>
              </dl>
              <p className="fine-print">
                Signals update on requests and responses. Bursts alone keep the
                full budget. A new, unclassified endpoint can still be
                penalized: review the catalog before using this policy.
              </p>
            </aside>
          </div>
        </section>
        <section className="bottom-grid">
          <div className="test-drive">
            <span className="section-number">D</span>
            <h2>Give it some traffic.</h2>
            <p>
              A four-second synthetic campaign: healthy bursts and clients
              repeatedly requesting missing endpoints. Requests pass through the
              real proxy and local origin.
            </p>
            <button
              className="primary"
              disabled={!state?.demo_enabled || busy}
              onClick={() => void action("/api/campaign", "POST")}
            >
              {state?.campaign_busy
                ? "Traffic in flight…"
                : "Run synthetic traffic"}
              <span>↗</span>
            </button>
            <div className="campaign-result" aria-live="polite">
              {last ? (
                <>
                  <span>
                    LAST CAMPAIGN / {state?.campaign_result?.mode.toUpperCase()}
                  </span>
                  <strong>
                    {last.success} <small>/ {last.offered}</small>
                  </strong>
                  <p>
                    legitimate requests succeeded · {percent(last.success_rate)}
                  </p>
                  <p>
                    {last.limited} rate denials · {last.capacity}{" "}
                    capacity/circuit denials · {last.p99_ms.toFixed(2)} ms
                    request p99
                  </p>
                </>
              ) : (
                <p>
                  {state?.campaign_busy
                    ? "Recording actual HTTP responses…"
                    : "No campaign result yet. Counts will appear after the run."}
                </p>
              )}
            </div>
            <div className="origin-controls">
              <span>LOCAL ORIGIN / {state?.origin_mode ?? "—"}</span>
              <div>
                {["healthy", "fail", "slow"].map((mode) => (
                  <button
                    key={mode}
                    disabled={!state?.demo_enabled || busy}
                    onClick={() => void action("/api/origin", "PUT", { mode })}
                  >
                    {mode}
                  </button>
                ))}
              </div>
            </div>
            <p className="notice" role="status">
              {notice || state?.campaign_error}
            </p>
          </div>
          <div className="event-feed">
            <div className="section-heading">
              <div>
                <span className="section-number">E</span>
                <h2>Decision receipts</h2>
              </div>
              <span className="fine-print">LATEST 200 COMPLETIONS</span>
            </div>
            <div className="event-list">
              {state?.engine.events.length ? (
                [...state.engine.events].reverse().map((event, i) => (
                  <div
                    className={`event event-${event.outcome}`}
                    key={`${event.at}-${i}`}
                  >
                    <time>
                      {new Date(event.at).toLocaleTimeString([], {
                        hour12: false,
                      })}
                    </time>
                    <div>
                      <strong>{event.client}</strong>
                      <span>
                        {event.endpoint} ·{" "}
                        {outcomeLabel[event.outcome] ?? event.outcome}
                      </span>
                    </div>
                    <span className="event-status">{event.status}</span>
                    <span className="event-latency">
                      {event.latency_ms.toFixed(2)} ms
                    </span>
                  </div>
                ))
              ) : (
                <div className="empty">
                  The road is quiet.
                  <br />
                  <span>Run traffic to see real admission receipts here.</span>
                </div>
              )}
            </div>
          </div>
        </section>
        <footer>
          <a className="footer-wordmark" href="/">
            throttle ↗
          </a>
          <p>
            Synthetic demo. Limits are local to this process.
            <br />
            No simulated counters. No claim of universal fairness.
          </p>
          <span>BUILD / GO + REACT</span>
        </footer>
      </main>
    </>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
