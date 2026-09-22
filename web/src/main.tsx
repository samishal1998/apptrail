import React, { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  DashboardCanvas,
  type Dashboard,
  type PageLayout,
} from "./dashboard-layout";
import "../../apptrail_working_pack/themes/apptrail-dark.css";
import "../../apptrail_working_pack/themes/apptrail-light.css";
import "./style.css";

type App = {
  id: string;
  name: string;
  description: string;
  url: string;
  urls?: string[];
  icon: string | boolean;
  category: string;
  health: string;
  lifecycle?: string;
  favorite?: boolean;
  hidden?: boolean;
  created?: number;
  seen?: number;
  sources?: string[];
  fields?: Record<string, { value: string; priority: number; source: string }>;
  probe_error?: string;
};
type ProviderType = "docker" | "caddy" | "traefik";
const providerTypes = {
  docker: {
    label: "Docker",
    name: "Local Docker",
    endpoint: "unix:///var/run/docker.sock",
    endpointLabel: "Docker API endpoint",
    placeholder: "unix:///var/run/docker.sock",
    icon: "provider-docker",
    summary: "Docker + Traefik labels",
    help: "Use a Unix socket or an HTTP(S) Docker API proxy. Reads container labels and Docker health.",
  },
  caddy: {
    label: "Caddy",
    name: "Caddy",
    endpoint: "http://127.0.0.1:2019",
    endpointLabel: "Caddy admin endpoint",
    placeholder: "http://caddy:2019",
    icon: "provider-proxy",
    summary: "Caddy active configuration",
    help: "Use an HTTP(S) admin base URL without /config/, or unix:///path/to/admin.sock. In Docker, localhost refers to Apptrail's own container.",
  },
  traefik: {
    label: "Traefik",
    name: "Traefik",
    endpoint: "",
    endpointLabel: "Traefik API endpoint",
    placeholder: "https://traefik.example.com",
    icon: "provider-proxy",
    summary: "Traefik routers & services",
    help: "Enter the protected API origin and any configured base path, not /dashboard/ or a complete /api/... URL. The API must already be enabled.",
  },
} as const;
type Provider = {
  id: string;
  name: string;
  endpoint: string;
  type: ProviderType;
  auth_file: string;
  enabled: boolean;
  scanned: number;
  error: string;
  count: number;
};
function sourceLabel(app: App, providers: Provider[]) {
  const labels = [
    ...new Set(
      providers
        .filter((p) => app.sources?.includes(p.id))
        .map((p) => providerTypes[p.type || "docker"].label),
    ),
  ];
  if (labels.length === 1) return labels[0];
  if (labels.length > 1) return `${labels.length} providers`;
  return app.sources?.some((id) => !id.startsWith("manual:"))
    ? "Discovered"
    : "Manual";
}
type Session = {
  version: string;
  authenticated: boolean;
  setup_required: boolean;
  username: string;
};
type Settings = { private_probes: boolean; scan_interval_seconds: number };
type Page =
  "Overview" | "Apps" | "Discover" | "Layout" | "Providers" | "Settings";
type Modal =
  | { type: "app"; app?: App }
  | { type: "source"; app: App }
  | { type: "provider"; provider?: Provider }
  | { type: "dashboard"; dashboard?: Dashboard }
  | { type: "items"; dashboard: Dashboard };

async function api<T = unknown>(
  path: string,
  method = "GET",
  data?: unknown,
): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers:
      data === undefined ? undefined : { "Content-Type": "application/json" },
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  const payload = await res.json();
  if (!res.ok)
    throw new Error(payload.error || "Something went wrong. Please try again.");
  return payload;
}
const iconAssets = import.meta.glob(
  "../../apptrail_working_pack/iconography/*.svg",
  { query: "?raw", import: "default", eager: true },
) as Record<string, string>;
const extra: Record<string, React.ReactNode> = {
  close: <path d="m6 6 12 12M18 6 6 18" />,
  arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
  down: <path d="m7 10 5 5 5-5" />,
  lock: (
    <>
      <rect x="5" y="10" width="14" height="11" rx="3" />
      <path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3" />
    </>
  ),
  globe: (
    <>
      <circle cx="12" cy="12" r="9" />
      <ellipse cx="12" cy="12" rx="4" ry="9" />
      <path d="M3 12h18" />
    </>
  ),
  logout: (
    <>
      <path d="M9 4H5v16h4M10 12h10m-4-4 4 4-4 4" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 7v5h-5M4 17v-5h5" />
      <path d="M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1" />
    </>
  ),
  more: (
    <>
      <circle cx="5" cy="12" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="19" cy="12" r="1" />
    </>
  ),
  moon: <path d="M20 14A8.5 8.5 0 0 1 10 4a8.5 8.5 0 1 0 10 10Z" />,
  sun: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l2 2m10 10 2 2M5 19l2-2M17 7l2-2" />
    </>
  ),
  check: <path d="m5 12 4 4L19 6" />,
  eye: (
    <>
      <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  up: <path d="m7 14 5-5 5 5" />,
  grip: (
    <>
      <path d="M9 5h.01M15 5h.01M9 12h.01M15 12h.01M9 19h.01M15 19h.01" />
    </>
  ),
  trash: (
    <>
      <path d="M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7" />
    </>
  ),
};
function Icon({ name, className = "" }: { name: string; className?: string }) {
  const asset =
    iconAssets[`../../apptrail_working_pack/iconography/${name}.svg`];
  return asset ? (
    <span
      className={`icon ${className}`}
      aria-hidden="true"
      dangerouslySetInnerHTML={{ __html: asset }}
    />
  ) : (
    <svg
      className={`icon ${className}`}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {extra[name] || extra.globe}
    </svg>
  );
}
function Brand() {
  return (
    <div className="brand">
      apptrail<span className="brand-dot">.</span>
    </div>
  );
}
function AppIcon({ app }: { app: App }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [app.icon]);
  const tone = [...app.name].reduce((n, c) => n + c.charCodeAt(0), 0) % 5;
  return (
    <div className={`app-icon tone-${tone}`}>
      {app.icon && !failed ? (
        <img
          alt=""
          src={`/api/icons/${app.id}`}
          onError={() => setFailed(true)}
        />
      ) : (
        <span>{app.name.slice(0, 1).toUpperCase()}</span>
      )}
    </div>
  );
}
function hostname(raw: string) {
  try {
    return new URL(raw).host;
  } catch {
    return "No launch URL";
  }
}
function ago(time: number) {
  if (!time) return "Not scanned yet";
  const min = Math.max(0, Math.floor((Date.now() / 1000 - time) / 60));
  return min < 1
    ? "Just now"
    : min < 60
      ? `${min}m ago`
      : `${Math.floor(min / 60)}h ago`;
}
function Status({ app }: { app: App }) {
  const state =
    app.lifecycle === "missing" || app.lifecycle === "stale"
      ? app.lifecycle
      : app.health || "unknown";
  const names: Record<string, string> = {
    healthy: "Healthy",
    unknown: "Not checked",
    degraded: "Needs attention",
    unhealthy: "Unhealthy",
    unreachable: "Unreachable",
    missing: "Missing",
    stale: "Stale",
  };
  return (
    <span className={`status ${state}`}>
      <i />
      {names[state] || state}
    </span>
  );
}
function Empty({
  icon = "apps",
  title,
  children,
  action,
}: {
  icon?: string;
  title: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} />
      </div>
      <h2>{title}</h2>
      <p>{children}</p>
      {action}
    </div>
  );
}
function Dialog({
  title,
  subtitle,
  close,
  children,
}: {
  title: string;
  subtitle?: string;
  close: () => void;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
    return () => ref.current?.close();
  }, []);
  return (
    <dialog
      ref={ref}
      onCancel={close}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const b = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < b.left ||
            e.clientX > b.right ||
            e.clientY < b.top ||
            e.clientY > b.bottom
          )
            close();
        }
      }}
      aria-labelledby="dialog-title"
    >
      <div className="dialog-head">
        <div>
          <h2 id="dialog-title">{title}</h2>
          {subtitle && <p>{subtitle}</p>}
        </div>
        <button
          type="button"
          className="icon-button"
          aria-label="Close dialog"
          onClick={close}
        >
          <Icon name="close" />
        </button>
      </div>
      {children}
    </dialog>
  );
}

function Root() {
  const [session, setSession] = useState<Session | null>(null);
  const [error, setError] = useState("");
  const [theme, setTheme] = useState(
    () => localStorage.getItem("apptrail-theme") || "apptrail-dark",
  );
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("apptrail-theme", theme);
  }, [theme]);
  const load = useCallback(
    () =>
      api<Session>("/session")
        .then(setSession)
        .catch((e) => setError(e.message)),
    [],
  );
  useEffect(() => {
    load();
  }, [load]);
  const toggleTheme = () =>
    setTheme((t) =>
      t === "apptrail-dark" ? "apptrail-light" : "apptrail-dark",
    );
  const slug = window.location.pathname.startsWith("/d/")
    ? window.location.pathname.slice(3)
    : "";
  if (slug)
    return <PublicPage slug={slug} toggleTheme={toggleTheme} theme={theme} />;
  if (!session)
    return (
      <div className="loading-screen">
        <Brand />
        <p role="status">{error || "Finding your way home…"}</p>
        {error && <button onClick={load}>Try again</button>}
      </div>
    );
  if (!session.authenticated) return <Auth session={session} done={load} />;
  return (
    <Workspace
      session={session}
      logout={async () => {
        await api("/logout", "POST");
        await load();
      }}
      expired={load}
      theme={theme}
      toggleTheme={toggleTheme}
    />
  );
}

function Auth({ session, done }: { session: Session; done: () => void }) {
  const setup = session.setup_required;
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const f = new FormData(e.currentTarget);
    try {
      await api(setup ? "/setup" : "/login", "POST", {
        username: f.get("username"),
        password: f.get("password"),
        token: f.get("token") || "",
      });
      done();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth-shell">
      <section className="auth-story">
        <Brand />
        <div className="story-content">
          <span className="eyebrow">
            <span className="tiny-dot" /> YOUR SELF-HOSTED HOME
          </span>
          <h1>
            Everything you host.
            <br />
            <span>One place to start.</span>
          </h1>
          <p>
            Your infrastructure changes. Your dashboard should keep up. Discover
            your apps, make room for your favorites, and find your way back.
          </p>
          <div className="story-features">
            <span>
              <Icon name="discover" />
              Infrastructure-aware
            </span>
            <span>
              <Icon name="lock" />
              Yours by default
            </span>
          </div>
        </div>
        <div className="story-footer">
          <Icon name="self-hosted" /> Built for your corner of the internet.
        </div>
        <div className="horizon" aria-hidden="true">
          <div />
          <div />
          <div />
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-form">
          <span className="eyebrow">
            {setup ? "LET’S MAKE THIS YOURS" : "YOUR APPS ARE WAITING"}
          </span>
          <h2>{setup ? "Welcome to Apptrail." : "Welcome home."}</h2>
          <p>
            {setup
              ? "Create your owner account to connect your infrastructure and build your first dashboard."
              : "Sign in to organize your apps and manage your dashboards."}
          </p>
          <form onSubmit={submit}>
            {setup && (
              <label>
                Setup token
                <input
                  name="token"
                  required
                  autoComplete="off"
                  placeholder="Paste the token from your server"
                />
                <small>
                  Find it in the server startup log or data/setup-token.
                </small>
              </label>
            )}
            <label>
              Username
              <input
                name="username"
                required
                maxLength={64}
                autoComplete="username"
                placeholder="Your username"
              />
            </label>
            <label>
              Password
              <input
                name="password"
                type="password"
                required
                minLength={setup ? 12 : undefined}
                maxLength={72}
                autoComplete={setup ? "new-password" : "current-password"}
                placeholder={setup ? "At least 12 characters" : "Your password"}
              />
            </label>
            {error && (
              <p className="form-error" role="alert">
                {error}
              </p>
            )}
            <button className="button primary full" disabled={busy}>
              {busy
                ? "Just a moment…"
                : setup
                  ? "Create your account"
                  : "Sign in"}
              <Icon name="arrow" />
            </button>
          </form>
          <div className="auth-note">
            <Icon name="lock" />
            {setup
              ? "One owner. Private by default. Always self-hosted."
              : "Owner access · Your dashboard, your rules."}
          </div>
        </div>
        <span className="version">APPTRAIL / {session.version}</span>
      </section>
    </main>
  );
}

function Workspace({
  session,
  logout,
  expired,
  theme,
  toggleTheme,
}: {
  session: Session;
  logout: () => Promise<void>;
  expired: () => void;
  theme: string;
  toggleTheme: () => void;
}) {
  const [page, setPage] = useState<Page>("Overview");
  const [apps, setApps] = useState<App[]>([]);
  const [dashboards, setDashboards] = useState<Dashboard[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [settings, setSettings] = useState<Settings>({
    private_probes: false,
    scan_interval_seconds: 300,
  });
  const [activeID, setActiveID] = useState("home");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("All apps");
  const [category, setCategory] = useState("All categories");
  const [modal, setModal] = useState<Modal | null>(null);
  const [notice, setNotice] = useState<{ text: string; error: boolean } | null>(
    null,
  );
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const editingLayout = useRef(false);
  editingLayout.current = page === "Layout";
  const searchRef = useRef<HTMLInputElement>(null);
  const refresh = useCallback(async () => {
    const [a, d, p, s] = await Promise.all([
      api<App[]>("/apps"),
      api<Dashboard[]>("/dashboards"),
      api<Provider[]>("/providers"),
      api<Settings>("/settings"),
    ]);
    setApps(a);
    setDashboards(d);
    setProviders(p);
    setSettings(s);
    setReady(true);
  }, []);
  useEffect(() => {
    const load = () =>
      refresh().catch((e) => {
        setNotice({ text: e.message, error: true });
        if (e.message === "Sign in to continue") expired();
      });
    load();
    const t = setInterval(() => {
      if (!editingLayout.current) load();
    }, 30000);
    return () => clearInterval(t);
  }, [refresh, expired]);
  useEffect(() => {
    if (!notice || notice.error) return;
    const t = setTimeout(() => setNotice(null), 4500);
    return () => clearTimeout(t);
  }, [notice]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (
        (e.key === "/" &&
          !["INPUT", "TEXTAREA", "SELECT"].includes(
            (e.target as HTMLElement).tagName,
          )) ||
        ((e.ctrlKey || e.metaKey) && e.key === "k")
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, []);
  const active = dashboards.find((d) => d.id === activeID) || dashboards[0];
  const unresolved = apps.filter((a) => !a.url || a.lifecycle !== "present");
  const visible = apps.filter((a) => !a.hidden);
  const ordered = active
    ? active.items
        .map((id) => apps.find((a) => a.id === id))
        .filter((a): a is App => !!a && (page === "Layout" || !a.hidden))
    : [];
  const onDashboard = page === "Overview" || page === "Layout";
  const base = onDashboard ? ordered : page === "Discover" ? unresolved : apps;
  const filtered = base.filter(
    (a) =>
      page === "Layout" ||
      ((filter === "Hidden" ? a.hidden : !a.hidden) &&
        (filter !== "Favorites" || a.favorite) &&
        (category === "All categories" || a.category === category) &&
        `${a.name} ${a.description} ${a.category} ${a.url} ${(a.urls || []).join(" ")} ${(a.sources || []).map((id) => providers.find((p) => p.id === id)?.name || id).join(" ")}`
          .toLowerCase()
          .includes(query.toLowerCase())),
  );
  if (page === "Apps")
    filtered.sort((a, b) => Number(!!b.url) - Number(!!a.url));
  const categories = [...new Set(base.map((a) => a.category))].sort();
  async function perform(action: () => Promise<unknown>, success?: string) {
    setBusy(true);
    try {
      await action();
      await refresh();
      if (success) setNotice({ text: success, error: false });
      return true;
    } catch (e) {
      await refresh().catch(() => {});
      setNotice({ text: (e as Error).message, error: true });
      return false;
    } finally {
      setBusy(false);
    }
  }
  function navigate(p: Page) {
    setPage(p);
    setQuery("");
    setCategory("All categories");
    setFilter("All apps");
  }
  async function saveLayout(layout: PageLayout, revision: number) {
    if (!active) return false;
    return perform(
      () =>
        api(`/dashboards/${active.id}`, "PUT", {
          ...active,
          layout,
          revision,
          auto_section: layout.sections.some(
            (s) => s.id === active.auto_section,
          )
            ? active.auto_section
            : layout.sections[0].id,
        }),
      "Layout saved",
    );
  }
  const nav: [Page, string][] = [
    ["Overview", "overview"],
    ["Apps", "apps"],
    ["Discover", "discover"],
    ["Layout", "layout"],
    ["Providers", "providers"],
  ];
  const controls = (
    <>
      <button className="button" onClick={() => setModal({ type: "provider" })}>
        <Icon name="providers" />
        Connect provider
      </button>
      <button
        className="button primary"
        onClick={() => setModal({ type: "app" })}
      >
        <Icon name="add" />
        Add app
      </button>
    </>
  );
  return (
    <div className="workspace">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="sidebar">
        <a className="brand-link" href="/" aria-label="Apptrail home">
          <Brand />
        </a>
        <div className="workspace-label">
          <span className="workspace-avatar">
            <Icon name="self-hosted" />
          </span>
          <div>
            <strong>My workspace</strong>
            <span>Self-hosted & personal</span>
          </div>
        </div>
        <span className="nav-label">YOUR SPACE</span>
        <nav aria-label="Main navigation">
          {nav.map(([p, icon]) => (
            <button
              key={p}
              className={`nav-item ${page === p ? "active" : ""}`}
              aria-current={page === p ? "page" : undefined}
              onClick={() => navigate(p)}
            >
              <Icon name={icon} />
              {p}
              {p === "Discover" && unresolved.length > 0 && (
                <span className="nav-count">{unresolved.length}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="local-note">
            <span className="status-dot" />
            <span>
              Hosted by you.
              <br />
              <strong>Right where it belongs.</strong>
            </span>
          </div>
          <button
            className={`nav-item ${page === "Settings" ? "active" : ""}`}
            onClick={() => navigate("Settings")}
          >
            <Icon name="settings" />
            Settings
          </button>
          <div className="owner-row">
            <div className="avatar">
              {session.username.slice(0, 1).toUpperCase()}
            </div>
            <div>
              <strong>{session.username}</strong>
              <span>Workspace owner</span>
            </div>
            <button
              className="icon-button"
              aria-label="Sign out"
              title="Sign out"
              onClick={() =>
                logout().catch((e) =>
                  setNotice({ text: e.message, error: true }),
                )
              }
            >
              <Icon name="logout" />
            </button>
          </div>
        </div>
      </aside>
      <div className="workspace-main">
        <header className="topbar">
          <div className="breadcrumb">
            <Icon name="overview" />
            <span>/</span>
            {page}
          </div>
          <div className="topbar-right">
            <span className="private-tag">
              <Icon name="lock" />
              Owner workspace
            </span>
            <button
              className="icon-button"
              aria-label={`Switch to ${theme === "apptrail-dark" ? "light" : "dark"} theme`}
              onClick={toggleTheme}
            >
              <Icon name={theme === "apptrail-dark" ? "sun" : "moon"} />
            </button>
            <div className="avatar small">
              {session.username.slice(0, 1).toUpperCase()}
            </div>
          </div>
        </header>
        <main id="main" className="main-content">
          {notice && (
            <div
              className={`toast ${notice.error ? "error" : ""}`}
              role={notice.error ? "alert" : "status"}
            >
              <Icon name={notice.error ? "health" : "check"} />
              <span>{notice.text}</span>
              <button
                className="icon-button"
                aria-label="Dismiss notification"
                onClick={() => setNotice(null)}
              >
                <Icon name="close" />
              </button>
            </div>
          )}
          {!ready ? (
            <div className="empty" role="status">
              Loading your workspace…
            </div>
          ) : (
            <>
              {page === "Overview" ? (
                <>
                  <section className="welcome">
                    <div className="welcome-copy">
                      <span className="eyebrow">
                        {new Intl.DateTimeFormat("en", {
                          weekday: "long",
                          month: "long",
                          day: "numeric",
                        })
                          .format(new Date())
                          .toUpperCase()}
                      </span>
                      <h1>
                        Your apps.
                        <br />
                        <span>Right where you left them.</span>
                      </h1>
                      <p>
                        A home for everything you host. Less searching, more
                        doing.
                      </p>
                      <div className="welcome-actions">{controls}</div>
                    </div>
                    <div className="welcome-art" aria-hidden="true">
                      <div className="orbit orbit-one" />
                      <div className="orbit orbit-two" />
                      <div className="sun-glow" />
                      <div className="floating-tile tile-one">
                        <Icon name="provider-docker" />
                      </div>
                      <div className="floating-tile tile-two">
                        <Icon name="apps" />
                      </div>
                      <div className="floating-tile tile-three">
                        <Icon name="self-hosted" />
                      </div>
                      <div className="art-caption">
                        YOUR CORNER OF THE INTERNET
                      </div>
                    </div>
                    <div className="hero-ridge" aria-hidden="true" />
                  </section>
                  <div className="stats">
                    <div>
                      <span className="stat-icon orange">
                        <Icon name="apps" />
                      </span>
                      <div>
                        <strong>{visible.length}</strong>
                        <span>Applications</span>
                      </div>
                    </div>
                    <div>
                      <span className="stat-icon green">
                        <Icon name="health" />
                      </span>
                      <div>
                        <strong>
                          {
                            visible.filter(
                              (a) =>
                                a.health === "healthy" &&
                                a.lifecycle === "present",
                            ).length
                          }
                          <small> / {visible.length}</small>
                        </strong>
                        <span>Healthy apps</span>
                      </div>
                    </div>
                    <div>
                      <span className="stat-icon purple">
                        <Icon name="providers" />
                      </span>
                      <div>
                        <strong>
                          {providers.filter((p) => p.enabled).length}
                        </strong>
                        <span>Active providers</span>
                      </div>
                    </div>
                    <div>
                      <span className="stat-icon amber">
                        <Icon name="favorite" />
                      </span>
                      <div>
                        <strong>
                          {visible.filter((a) => a.favorite).length}
                        </strong>
                        <span>Favorites</span>
                      </div>
                    </div>
                  </div>
                </>
              ) : (
                <div className="page-heading">
                  <div>
                    <span className="eyebrow">YOUR WORKSPACE</span>
                    <h1>
                      {page === "Apps"
                        ? "All your applications."
                        : page === "Discover"
                          ? "Nothing gets left behind."
                          : page === "Providers"
                            ? "Connected to your infrastructure."
                            : page === "Layout"
                              ? "Arrange your dashboard."
                              : "Make yourself at home."}
                    </h1>
                    <p>
                      {page === "Apps"
                        ? "Everything you’ve discovered or added, all in one place."
                        : page === "Discover"
                          ? "Inspect apps with unresolved URLs or missing infrastructure."
                          : page === "Providers"
                            ? "Connect Docker, Caddy, and Traefik to discover your applications."
                            : page === "Layout"
                              ? "Create sections, move cards, and make room for what matters."
                              : "A few thoughtful defaults. The rest is up to you."}
                    </p>
                  </div>
                  <div className="heading-actions">
                    {page === "Apps" ? (
                      controls
                    ) : page === "Providers" ? (
                      <button
                        className="button primary"
                        onClick={() => setModal({ type: "provider" })}
                      >
                        <Icon name="add" />
                        Connect provider
                      </button>
                    ) : null}
                  </div>
                </div>
              )}
              {["Overview", "Apps", "Discover", "Layout"].includes(page) && (
                <section className="apps-section">
                  <div className="section-title">
                    <div className="section-title-left">
                      {onDashboard && active ? (
                        <>
                          <label className="sr-only" htmlFor="dashboard-select">
                            Dashboard page
                          </label>
                          <select
                            id="dashboard-select"
                            className="dashboard-select"
                            value={active.id}
                            onChange={(e) => {
                              setActiveID(e.target.value);
                              setQuery("");
                              setCategory("All categories");
                            }}
                          >
                            {dashboards.map((d) => (
                              <option value={d.id} key={d.id}>
                                {d.name}
                              </option>
                            ))}
                          </select>
                          <span className="visibility-badge">
                            <Icon name={active.public ? "globe" : "lock"} />
                            {active.public ? "Public" : "Private"}
                          </span>
                        </>
                      ) : (
                        <h2>
                          {page === "Apps"
                            ? "Application registry"
                            : "Needs a closer look"}
                        </h2>
                      )}
                      <span className="count-pill">
                        {base.filter((a) => !a.hidden).length}
                      </span>
                    </div>
                    {onDashboard && active && (
                      <div className="section-actions">
                        <button
                          className="text-button"
                          onClick={() => setModal({ type: "dashboard" })}
                        >
                          <Icon name="add" />
                          New page
                        </button>
                        <button
                          className="button compact"
                          onClick={() =>
                            setModal({ type: "dashboard", dashboard: active })
                          }
                        >
                          <Icon name="settings" />
                          Page settings
                        </button>
                        <button
                          className="button compact"
                          onClick={() =>
                            setModal({ type: "items", dashboard: active })
                          }
                        >
                          <Icon name="organize" />
                          Choose apps
                        </button>
                      </div>
                    )}
                  </div>
                  {active?.rule_error && onDashboard && (
                    <p className="form-error" role="alert">
                      Auto-add rule: {active.rule_error}
                    </p>
                  )}
                  {onDashboard && active?.auto_rule && (
                    <p className="info-line">
                      <Icon name="discover" />
                      CEL auto-add is enabled for this page.
                    </p>
                  )}
                  {page !== "Layout" && (
                    <div className="filterbar">
                      <div
                        className="filter-tabs"
                        role="group"
                        aria-label="App visibility filter"
                      >
                        {(onDashboard
                          ? ["All apps", "Favorites"]
                          : ["All apps", "Favorites", "Hidden"]
                        ).map((f) => (
                          <button
                            key={f}
                            className={filter === f ? "selected" : ""}
                            aria-pressed={filter === f}
                            onClick={() => setFilter(f)}
                          >
                            {f === "Favorites" && <Icon name="favorite" />}
                            {f}
                          </button>
                        ))}
                      </div>
                      <div className="search-controls">
                        <div className="search">
                          <Icon name="search" />
                          <input
                            ref={searchRef}
                            aria-label="Search applications"
                            placeholder="Search your apps…"
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                          />
                          <kbd>/</kbd>
                        </div>
                        <label className="category-select">
                          <Icon name="filter" />
                          <span className="sr-only">Filter by category</span>
                          <select
                            value={category}
                            onChange={(e) => setCategory(e.target.value)}
                          >
                            <option>All categories</option>
                            {categories.map((c) => (
                              <option key={c}>{c}</option>
                            ))}
                          </select>
                        </label>
                      </div>
                    </div>
                  )}
                  {filtered.length === 0 && page !== "Layout" ? (
                    <Empty
                      icon={apps.length ? "search" : "discover"}
                      title={
                        query ||
                        filter !== "All apps" ||
                        category !== "All categories"
                          ? "No apps match just yet."
                          : onDashboard && apps.length
                            ? "Make room for your favorites."
                            : page === "Discover"
                              ? "All clear. Everything has a place."
                              : "Your trail starts here."
                      }
                      action={
                        query ||
                        filter !== "All apps" ||
                        category !== "All categories" ? (
                          <button
                            className="button"
                            onClick={() => {
                              setQuery("");
                              setFilter("All apps");
                              setCategory("All categories");
                            }}
                          >
                            Clear filters
                          </button>
                        ) : onDashboard && apps.length ? (
                          <button
                            className="button primary"
                            onClick={() =>
                              active &&
                              setModal({ type: "items", dashboard: active })
                            }
                          >
                            <Icon name="add" />
                            Choose apps
                          </button>
                        ) : page !== "Discover" ? (
                          <div className="empty-actions">{controls}</div>
                        ) : undefined
                      }
                    >
                      {query
                        ? "Try another name, hostname, or category."
                        : onDashboard && apps.length
                          ? "Choose which applications belong on this page. Your layout stays yours, even when infrastructure changes."
                          : page === "Discover"
                            ? "Apps with missing sources or unresolved launch URLs will appear here."
                            : "Connect a Docker provider to discover your services, or add your first app by hand."}
                    </Empty>
                  ) : (
                    <DashboardCanvas
                      key={active?.id || page}
                      dashboard={onDashboard ? active : undefined}
                      editing={page === "Layout"}
                      busy={busy}
                      onSave={saveLayout}
                      names={Object.fromEntries(
                        apps.map((a) => [a.id, a.name]),
                      )}
                    >
                      {filtered.map((a) => (
                        <div
                          key={a.id}
                          className={`app-card ${a.hidden ? "hidden-card" : ""}`}
                        >
                          <div className="card-top">
                            <AppIcon app={a} />
                            <div className="card-actions">
                              <button
                                className={`icon-button favorite-button ${a.favorite ? "is-favorite" : ""}`}
                                disabled={busy}
                                aria-label={`${a.favorite ? "Unfavorite" : "Favorite"} ${a.name}`}
                                aria-pressed={!!a.favorite}
                                onClick={() =>
                                  perform(() =>
                                    api(`/apps/${a.id}`, "PATCH", {
                                      favorite: !a.favorite,
                                    }),
                                  )
                                }
                              >
                                <Icon name="favorite" />
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`Edit ${a.name}`}
                                onClick={() =>
                                  setModal({ type: "app", app: a })
                                }
                              >
                                <Icon name="more" />
                              </button>
                            </div>
                          </div>
                          <div className="card-content">
                            <h3>
                              {a.url ? (
                                <a
                                  href={a.url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                >
                                  {a.name}
                                  <Icon name="launch" />
                                </a>
                              ) : (
                                a.name
                              )}
                            </h3>
                            {a.url ? (
                              <a
                                className="card-launch-url"
                                href={a.url}
                                target="_blank"
                                rel="noopener noreferrer"
                                title={a.url}
                              >
                                {a.url}
                              </a>
                            ) : (
                              <button
                                className="card-missing-url"
                                onClick={() =>
                                  setModal({ type: "app", app: a })
                                }
                              >
                                No launch URL discovered · Set URL
                              </button>
                            )}
                            {a.fields?.router?.value && (
                              <div className="card-route">
                                <Icon name="provider-proxy" />
                                <span>
                                  Route <code>{a.fields.router.value}</code>
                                  {a.fields.entrypoints?.value && (
                                    <span className="route-entrypoints">
                                      {" "}
                                      · {a.fields.entrypoints.value}
                                    </span>
                                  )}
                                </span>
                              </div>
                            )}
                            <p>
                              {a.description ||
                                "A little piece of your self-hosted world."}
                            </p>
                            <div className="card-tags">
                              <span>{a.category}</span>
                              {a.hidden && <span>Hidden</span>}
                            </div>
                          </div>
                          {(a.urls?.length || 0) > 1 && (
                            <details className="app-addresses">
                              <summary>
                                {a.urls!.length - 1} alternate{" "}
                                {a.urls!.length === 2 ? "address" : "addresses"}
                              </summary>
                              <ul>
                                {a
                                  .urls!.filter((url) => url !== a.url)
                                  .map((url) => (
                                    <li key={url}>
                                      <a
                                        href={url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                      >
                                        {url}
                                        <Icon name="launch" />
                                      </a>
                                    </li>
                                  ))}
                              </ul>
                            </details>
                          )}
                          <div className="card-footer">
                            <Status app={a} />
                            <button
                              className="source-button"
                              title="Inspect discovery sources"
                              aria-label={`Inspect sources for ${a.name}`}
                              onClick={() =>
                                setModal({ type: "source", app: a })
                              }
                            >
                              <Icon
                                name={
                                  a.sources?.some(
                                    (s) => !s.startsWith("manual:"),
                                  )
                                    ? "providers"
                                    : "self-hosted"
                                }
                              />
                              <span>{sourceLabel(a, providers)}</span>
                            </button>
                          </div>
                        </div>
                      ))}
                    </DashboardCanvas>
                  )}
                  <div className="section-foot">
                    <span>
                      <Icon name="self-hosted" />
                      Your infrastructure. Your starting point.
                    </span>
                    {onDashboard && active?.public ? (
                      <a
                        href={`/d/${active.slug}`}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Open public page
                        <Icon name="launch" />
                      </a>
                    ) : (
                      <span>Private until you say otherwise.</span>
                    )}
                  </div>
                </section>
              )}
              {page === "Providers" && (
                <>
                  <div className="info-panel">
                    <Icon name="discover" />
                    <div>
                      <strong>
                        Discovery that follows your infrastructure.
                      </strong>
                      <p>
                        Apptrail reads Docker labels, Caddy configuration, and
                        Traefik routes every five minutes. Configure each API
                        endpoint explicitly; a failed scan never removes your
                        apps.
                      </p>
                    </div>
                  </div>
                  {providers.length === 0 ? (
                    <Empty
                      icon="providers"
                      title="Let your infrastructure do the introductions."
                      action={
                        <button
                          className="button primary"
                          onClick={() => setModal({ type: "provider" })}
                        >
                          <Icon name="add" />
                          Connect your first provider
                        </button>
                      }
                    >
                      Choose Docker, Caddy, or Traefik and connect its API
                      endpoint.
                    </Empty>
                  ) : (
                    <div className="provider-list">
                      {providers.map((p) => (
                        <article className="provider-card" key={p.id}>
                          <div className="provider-main">
                            <div className="provider-logo">
                              <Icon
                                name={providerTypes[p.type || "docker"].icon}
                              />
                            </div>
                            <div>
                              <h2>
                                {p.name}
                                <span
                                  className={`pill ${p.enabled ? "green-pill" : ""}`}
                                >
                                  {p.enabled ? "Enabled" : "Paused"}
                                </span>
                              </h2>
                              <code>{p.endpoint}</code>
                            </div>
                            <div className="provider-actions">
                              <button
                                className="button"
                                disabled={busy || !p.enabled}
                                onClick={() =>
                                  perform(
                                    () =>
                                      api(`/providers/${p.id}/scan`, "POST"),
                                    "Scan complete",
                                  )
                                }
                              >
                                <Icon name="refresh" />
                                {busy ? "Working…" : "Scan now"}
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`Edit provider ${p.name}`}
                                onClick={() =>
                                  setModal({ type: "provider", provider: p })
                                }
                              >
                                <Icon name="settings" />
                              </button>
                            </div>
                          </div>
                          <div className="provider-stats">
                            <span>
                              {providerTypes[p.type || "docker"].summary}
                            </span>
                            <span>
                              <strong>{p.count}</strong>{" "}
                              {p.count === 1 ? "observation" : "observations"}
                            </span>
                            <span>
                              Last successful scan:{" "}
                              <strong>{ago(p.scanned)}</strong>
                            </span>
                          </div>
                          {p.error && (
                            <p className="provider-error">
                              <Icon name="health" />
                              {p.error}
                            </p>
                          )}
                        </article>
                      ))}
                    </div>
                  )}
                  <div className="info-line">
                    <Icon name="lock" />
                    Keep management APIs private. Use a restricted Docker API
                    proxy or protected Caddy/Traefik endpoint. Provider
                    connections do not use the metadata probe policy.
                  </div>
                </>
              )}
              {page === "Settings" && (
                <div className="settings-grid">
                  <section className="settings-card">
                    <div className="settings-heading">
                      <Icon name="eye" />
                      <h2>Appearance</h2>
                    </div>
                    <p>A comfortable place to land, day or night.</p>
                    <div className="theme-options">
                      <button
                        className={theme === "apptrail-dark" ? "chosen" : ""}
                        onClick={() => {
                          if (theme !== "apptrail-dark") toggleTheme();
                        }}
                      >
                        <Icon name="moon" />
                        After sunset
                        {theme === "apptrail-dark" && <Icon name="check" />}
                      </button>
                      <button
                        className={theme === "apptrail-light" ? "chosen" : ""}
                        onClick={() => {
                          if (theme !== "apptrail-light") toggleTheme();
                        }}
                      >
                        <Icon name="sun" />
                        First light
                        {theme === "apptrail-light" && <Icon name="check" />}
                      </button>
                    </div>
                  </section>
                  <section className="settings-card">
                    <div className="settings-heading">
                      <Icon name="health" />
                      <h2>Metadata & health</h2>
                    </div>
                    <p>
                      Fetch titles, descriptions, icons, and Apptrail manifests
                      from your applications.
                    </p>
                    <label className="toggle-row">
                      <span>
                        <strong>Allow private-network probes</strong>
                        <small>
                          Includes LAN, localhost, and Tailscale addresses.
                          Link-local and cloud metadata addresses stay blocked.
                        </small>
                      </span>
                      <input
                        type="checkbox"
                        checked={settings.private_probes}
                        disabled={busy}
                        onChange={async (e) => {
                          const value = e.currentTarget.checked;
                          const previous = settings.private_probes;
                          setSettings((s) => ({ ...s, private_probes: value }));
                          if (
                            !(await perform(
                              () =>
                                api("/settings", "PUT", {
                                  private_probes: value,
                                }),
                              "Probe policy saved",
                            ))
                          ) {
                            setSettings((s) => ({
                              ...s,
                              private_probes: previous,
                            }));
                          }
                        }}
                      />
                    </label>
                    <small>
                      Automatic scans and metadata refreshes run every five
                      minutes.
                    </small>
                  </section>
                  <section className="settings-card">
                    <div className="settings-heading">
                      <Icon name="lock" />
                      <h2>Owner account</h2>
                    </div>
                    <p>
                      Signed in as <strong>{session.username}</strong>. Changing
                      your password revokes other sessions.
                    </p>
                    <form
                      onSubmit={async (e) => {
                        e.preventDefault();
                        const form = e.currentTarget;
                        const f = new FormData(form);
                        if (
                          await perform(
                            () =>
                              api("/account", "POST", {
                                current: f.get("current"),
                                password: f.get("password"),
                              }),
                            "Password changed",
                          )
                        )
                          form.reset();
                      }}
                    >
                      <label>
                        Current password
                        <input
                          name="current"
                          type="password"
                          autoComplete="current-password"
                          required
                        />
                      </label>
                      <label>
                        New password
                        <input
                          name="password"
                          type="password"
                          autoComplete="new-password"
                          minLength={12}
                          maxLength={72}
                          required
                          placeholder="At least 12 characters"
                        />
                      </label>
                      <button className="button" disabled={busy}>
                        Update password
                      </button>
                    </form>
                    <button
                      className="button mobile-signout"
                      onClick={() =>
                        logout().catch((e) =>
                          setNotice({ text: e.message, error: true }),
                        )
                      }
                    >
                      <Icon name="logout" />
                      Sign out
                    </button>
                  </section>
                  <section className="settings-card about-card">
                    <Brand />
                    <p>Discover, organize, and launch your self-hosted apps.</p>
                    <dl>
                      <div>
                        <dt>Storage</dt>
                        <dd>SQLite · local & persistent</dd>
                      </div>
                      <div>
                        <dt>Access</dt>
                        <dd>One owner · public or private pages</dd>
                      </div>
                      <div>
                        <dt>Version</dt>
                        <dd>{session.version}</dd>
                      </div>
                    </dl>
                    <span className="eyebrow">A HOME FOR WHAT YOU HOST.</span>
                  </section>
                </div>
              )}
            </>
          )}
        </main>
        <footer className="main-footer">
          <Brand />
          <span>A little order for your digital world.</span>
          <span>SELF-HOSTED. ALWAYS.</span>
        </footer>
      </div>
      {modal && (
        <Editor
          modal={modal}
          close={() => setModal(null)}
          apps={apps}
          providers={providers}
          busy={busy}
          perform={perform}
          onCreatedDashboard={setActiveID}
        />
      )}
    </div>
  );
}

function DashboardRuleFields({ dashboard }: { dashboard?: Dashboard }) {
  const [rule, setRule] = useState(dashboard?.auto_rule || "");
  const [preview, setPreview] = useState<{
    count: number;
    matches: { id: string; name: string; url: string }[];
  } | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const sections = dashboard?.layout.sections || [
    { id: "main", name: "General" },
  ];
  return (
    <section className="rule-editor">
      <h3>Automatically add matching apps</h3>
      <p>Use a CEL expression. Leave it empty for a manually curated page.</p>
      <label>
        Auto-add rule (CEL)
        <textarea
          name="auto_rule"
          value={rule}
          maxLength={4096}
          rows={4}
          spellCheck={false}
          placeholder={'url != "" && "traefik" in provider_types'}
          onChange={(e) => {
            setRule(e.target.value);
            setPreview(null);
            setError("");
          }}
        />
      </label>
      <div className="rule-examples">
        <button
          type="button"
          className="text-button"
          onClick={() => {
            setRule('url != ""');
            setPreview(null);
          }}
        >
          All launchable apps
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => {
            setRule('url != "" && "traefik" in provider_types');
            setPreview(null);
          }}
        >
          Traefik apps
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => {
            setRule('category == "Media"');
            setPreview(null);
          }}
        >
          Media category
        </button>
      </div>
      <label>
        Add matches to section
        <select
          name="auto_section"
          className="provider-type-select"
          defaultValue={dashboard?.auto_section || sections[0].id}
        >
          {sections.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </label>
      <small>
        Fields: name, description, url, urls, host, path, category, health,
        lifecycle, favorite, hidden, provider_types, provider_ids, facts.
      </small>
      <button
        type="button"
        className="button compact"
        disabled={loading || !rule.trim()}
        onClick={async () => {
          setLoading(true);
          setError("");
          setPreview(null);
          try {
            setPreview(await api("/dashboards/rule-preview", "POST", { rule }));
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setLoading(false);
          }
        }}
      >
        {loading ? "Checking…" : "Preview matching apps"}
      </button>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {preview && (
        <div className="rule-preview" role="status">
          <strong>
            {preview.count} matching {preview.count === 1 ? "app" : "apps"}
          </strong>
          <ul>
            {preview.matches.map((a) => (
              <li key={a.id}>
                <span>
                  {a.name}
                  <small>{a.url || "No launch URL"}</small>
                </span>
                <span>
                  {dashboard?.excluded.includes(a.id)
                    ? "Excluded"
                    : dashboard?.items.includes(a.id)
                      ? "Already on page"
                      : "Will be added"}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {!!dashboard?.excluded.length && (
        <label className="checkbox-line">
          <input type="checkbox" name="reset_exclusions" />
          Reset {dashboard.excluded.length} manually excluded{" "}
          {dashboard.excluded.length === 1 ? "app" : "apps"}
        </label>
      )}
      <p className="field-note">
        Rules add present, non-hidden apps without removing existing cards.
        Manual removals stay excluded. On public pages, new matches are
        immediately visible to visitors.
      </p>
      {dashboard?.rule_error && (
        <p className="form-error">Last evaluation: {dashboard.rule_error}</p>
      )}
    </section>
  );
}

function Editor({
  modal,
  close,
  apps,
  providers,
  busy,
  perform,
  onCreatedDashboard,
}: {
  modal: Modal;
  close: () => void;
  apps: App[];
  providers: Provider[];
  busy: boolean;
  perform: (a: () => Promise<unknown>, msg?: string) => Promise<boolean>;
  onCreatedDashboard: (id: string) => void;
}) {
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState(
    modal.type === "items" ? modal.dashboard.items : [],
  );
  const [itemSearch, setItemSearch] = useState("");
  const launchURLField = useRef<HTMLInputElement>(null);
  const initialProvider =
    modal.type === "provider" ? modal.provider : undefined;
  const [providerType, setProviderType] = useState<ProviderType>(
    initialProvider?.type || "docker",
  );
  const [providerName, setProviderName] = useState(
    initialProvider?.name ||
      providerTypes[initialProvider?.type || "docker"].name,
  );
  const [providerEndpoint, setProviderEndpoint] = useState(
    initialProvider?.endpoint ||
      providerTypes[initialProvider?.type || "docker"].endpoint,
  );
  async function save(action: () => Promise<unknown>, message: string) {
    setPending(true);
    setError("");
    let failure = "";
    const ok = await perform(async () => {
      try {
        await action();
      } catch (e) {
        failure = (e as Error).message;
        throw e;
      }
    }, message);
    if (ok) close();
    else setError(failure);
    setPending(false);
  }
  const buttons = (label = "Save changes") => (
    <div className="dialog-actions">
      <button type="button" className="button" onClick={close}>
        Cancel
      </button>
      <button className="button primary" disabled={pending || busy}>
        {pending ? "Saving…" : label}
        <Icon name="check" />
      </button>
    </div>
  );
  const formError = error && (
    <p className="form-error" role="alert">
      {error}
    </p>
  );
  if (modal.type === "app") {
    const a = modal.app;
    return (
      <Dialog
        title={a ? "Make this app your own." : "Add a little to your world."}
        subtitle={
          a
            ? "Your preferences stay put when discovery refreshes."
            : "Start with a name and the URL you use to open it."
        }
        close={close}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            const data: Record<string, unknown> = Object.fromEntries(f);
            if (a) {
              data.hidden = f.has("hidden");
              data.favorite = f.has("favorite");
              for (const key of [
                "name",
                "url",
                "description",
                "category",
                "icon",
                "hidden",
                "favorite",
              ] as const) {
                if (data[key] === a[key]) delete data[key];
              }
            }
            save(
              () =>
                api(a ? `/apps/${a.id}` : "/apps", a ? "PATCH" : "POST", data),
              a
                ? "App preferences saved"
                : "App added. Choose apps on a dashboard to publish it there.",
            );
          }}
        >
          <div className="form-grid">
            <label>
              App name
              <input
                name="name"
                defaultValue={a?.name}
                required
                maxLength={120}
                placeholder="e.g. Photo library"
              />
            </label>
            <label>
              Category
              <input
                name="category"
                defaultValue={a?.category || "Other"}
                maxLength={60}
                placeholder="e.g. Media"
              />
            </label>
          </div>
          <label>
            Launch URL
            <input
              ref={launchURLField}
              name="url"
              type="url"
              defaultValue={a?.url}
              required
              maxLength={2000}
              placeholder="https://photos.example.com"
            />
          </label>
          {a && (a.urls?.length || 0) > 1 && (
            <label>
              Available launch addresses
              <select
                className="provider-type-select"
                defaultValue=""
                onChange={(e) => {
                  if (e.target.value && launchURLField.current)
                    launchURLField.current.value = e.target.value;
                }}
              >
                <option value="">Choose a primary launch URL…</option>
                {a.urls!.map((url) => (
                  <option key={url} value={url}>
                    {url}
                  </option>
                ))}
              </select>
              <small>
                The primary URL is the address shown on public dashboard pages.
              </small>
            </label>
          )}
          <label>
            Description
            <textarea
              name="description"
              defaultValue={a?.description}
              maxLength={1000}
              rows={3}
              placeholder="A few words about this app"
            />
          </label>
          <label>
            Icon URL <span className="optional">optional</span>
            <input
              name="icon"
              type="url"
              defaultValue={typeof a?.icon === "string" ? a.icon : ""}
              maxLength={2000}
              placeholder="https://photos.example.com/icon.png"
            />
          </label>
          {a && (
            <div className="checkbox-line">
              <label>
                <input
                  type="checkbox"
                  name="favorite"
                  defaultChecked={a.favorite}
                />
                Favorite
              </label>
              <label>
                <input
                  type="checkbox"
                  name="hidden"
                  defaultChecked={a.hidden}
                />
                Hide from dashboards
              </label>
            </div>
          )}
          {formError}
          {buttons(a ? "Save preferences" : "Add app")}
        </form>
        {a && (
          <div className="danger-zone">
            <button
              className="text-button"
              disabled={busy}
              onClick={() =>
                perform(
                  () => api(`/apps/${a.id}/refresh`, "POST"),
                  "Metadata refreshed",
                )
              }
            >
              <Icon name="refresh" />
              Refresh metadata
            </button>
            <button
              className="text-button danger-text"
              onClick={() => {
                if (
                  window.confirm(
                    `Forget ${a.name}? Discovered apps may return on the next scan.`,
                  )
                )
                  save(() => api(`/apps/${a.id}`, "DELETE"), "App forgotten");
              }}
            >
              <Icon name="trash" />
              Forget app
            </button>
          </div>
        )}
      </Dialog>
    );
  }
  if (modal.type === "provider") {
    const p = modal.provider;
    return (
      <Dialog
        title={p ? "Provider settings" : "Connect your infrastructure."}
        subtitle="Choose the infrastructure source you want to discover."
        close={close}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            save(
              () =>
                api(
                  p ? `/providers/${p.id}` : "/providers",
                  p ? "PATCH" : "POST",
                  {
                    name: f.get("name"),
                    endpoint: f.get("endpoint"),
                    enabled: f.has("enabled"),
                    type: providerType,
                    auth_file: f.get("auth_file") || "",
                  },
                ),
              "Provider saved. Use Scan now to discover apps.",
            );
          }}
        >
          <label>
            Provider type
            <select
              className="provider-type-select"
              value={providerType}
              disabled={!!p}
              onChange={(e) => {
                const next = e.target.value as ProviderType;
                setProviderName((name) =>
                  name === providerTypes[providerType].name
                    ? providerTypes[next].name
                    : name,
                );
                setProviderEndpoint((endpoint) =>
                  endpoint === providerTypes[providerType].endpoint
                    ? providerTypes[next].endpoint
                    : endpoint,
                );
                setProviderType(next);
              }}
            >
              {Object.entries(providerTypes).map(([type, meta]) => (
                <option key={type} value={type}>
                  {meta.label}
                </option>
              ))}
            </select>
            {p && (
              <small>To change the source type, create a new provider.</small>
            )}
          </label>
          <label>
            Provider name
            <input
              name="name"
              value={providerName}
              onChange={(e) => setProviderName(e.target.value)}
              required
              maxLength={100}
            />
          </label>
          <label>
            {providerTypes[providerType].endpointLabel}
            <input
              name="endpoint"
              value={providerEndpoint}
              onChange={(e) => setProviderEndpoint(e.target.value)}
              required
              placeholder={providerTypes[providerType].placeholder}
              maxLength={2048}
            />
            <small>{providerTypes[providerType].help}</small>
          </label>
          <details className="provider-auth" open={!!p?.auth_file}>
            <summary>Authentication (optional)</summary>
            <label>
              Authorization file
              <input
                name="auth_file"
                defaultValue={p?.auth_file || ""}
                placeholder="/run/secrets/proxy-authorization"
                maxLength={4096}
                autoComplete="off"
                spellCheck={false}
              />
              <small>
                Absolute path on the Apptrail server containing one
                Authorization value, such as Bearer TOKEN or Basic BASE64. Mount
                the file read-only in Docker. Only its path is stored;
                credentials are reread on each scan.
              </small>
            </label>
          </details>
          <label className="checkbox-line">
            <input
              type="checkbox"
              name="enabled"
              defaultChecked={p?.enabled ?? true}
            />
            Enable automatic discovery
          </label>
          {formError}
          {buttons(p ? "Save provider" : "Connect provider")}
        </form>
        {p && (
          <div className="danger-zone">
            <button
              className="text-button danger-text"
              onClick={() => {
                if (
                  window.confirm(
                    "Remove this provider? Its apps will be retained as missing.",
                  )
                )
                  save(
                    () => api(`/providers/${p.id}`, "DELETE"),
                    "Provider removed",
                  );
              }}
            >
              <Icon name="trash" />
              Remove provider
            </button>
          </div>
        )}
      </Dialog>
    );
  }
  if (modal.type === "dashboard") {
    const d = modal.dashboard;
    return (
      <Dialog
        title={d ? "A page that feels like yours." : "Start a new page."}
        subtitle="Choose a name and decide who can visit."
        close={close}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            save(async () => {
              const result = await api<Dashboard>(
                d ? `/dashboards/${d.id}` : "/dashboards",
                d ? "PUT" : "POST",
                {
                  ...d,
                  name: f.get("name"),
                  slug: f.get("slug"),
                  public: f.get("visibility") === "public",
                  auto_rule: String(f.get("auto_rule") || ""),
                  auto_section: String(f.get("auto_section") || "main"),
                  excluded: f.has("reset_exclusions") ? [] : d?.excluded || [],
                  items:
                    d?.items.filter((id) => apps.some((a) => a.id === id)) ||
                    [],
                },
              );
              onCreatedDashboard(result.id);
            }, "Dashboard saved");
          }}
        >
          <label>
            Page name
            <input
              name="name"
              defaultValue={d?.name}
              required
              maxLength={80}
              placeholder="e.g. My everyday apps"
            />
          </label>
          <label>
            Page URL
            <div className="slug-input">
              <span>/d/</span>
              <input
                name="slug"
                defaultValue={d?.slug}
                required
                pattern="[a-z0-9-]+"
                maxLength={64}
                placeholder="everyday"
              />
            </div>
          </label>
          <fieldset className="visibility-options">
            <legend>Who can view this page?</legend>
            <label>
              <input
                type="radio"
                name="visibility"
                value="private"
                defaultChecked={!d?.public}
              />
              <Icon name="lock" />
              <span>
                <strong>Just me</strong>
                <small>Only available when you’re signed in.</small>
              </span>
            </label>
            <label>
              <input
                type="radio"
                name="visibility"
                value="public"
                defaultChecked={d?.public}
              />
              <Icon name="globe" />
              <span>
                <strong>Anyone</strong>
                <small>
                  Visitors can see this page’s app names, descriptions, launch
                  links, icons, and health. Editing stays yours.
                </small>
              </span>
            </label>
          </fieldset>
          <p className="field-note">
            Publishing this page does not change sign-in requirements on the
            linked applications.
          </p>
          {formError}
          <DashboardRuleFields dashboard={d} />
          {buttons(d ? "Save page" : "Create page")}
        </form>
        {d && d.id !== "home" && (
          <div className="danger-zone">
            <button
              className="text-button danger-text"
              onClick={() => {
                if (
                  window.confirm(
                    "Delete this dashboard page? Your apps will stay in the registry.",
                  )
                )
                  save(
                    () => api(`/dashboards/${d.id}`, "DELETE"),
                    "Page deleted",
                  );
              }}
            >
              <Icon name="trash" />
              Delete page
            </button>
          </div>
        )}
      </Dialog>
    );
  }
  if (modal.type === "items")
    return (
      <Dialog
        title="Make room for your favorites."
        subtitle={`Choose the apps on ${modal.dashboard.name}. Arrange sections and card sizes in Layout.`}
        close={close}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save(
              () =>
                api(`/dashboards/${modal.dashboard.id}`, "PUT", {
                  ...modal.dashboard,
                  items: selected.filter((id) => apps.some((a) => a.id === id)),
                }),
              "Dashboard apps saved",
            );
          }}
        >
          <div className="search">
            <Icon name="search" />
            <input
              aria-label="Search apps to add"
              value={itemSearch}
              onChange={(e) => setItemSearch(e.target.value)}
              placeholder="Find an app…"
            />
          </div>
          <div className="app-picker">
            {apps
              .filter(
                (a) =>
                  !a.hidden &&
                  `${a.name} ${a.url} ${(a.urls || []).join(" ")}`
                    .toLowerCase()
                    .includes(itemSearch.toLowerCase()),
              )
              .map((a) => (
                <label key={a.id}>
                  <input
                    type="checkbox"
                    checked={selected.includes(a.id)}
                    onChange={(e) =>
                      setSelected((ids) =>
                        e.target.checked
                          ? [...ids, a.id]
                          : ids.filter((id) => id !== a.id),
                      )
                    }
                  />
                  <AppIcon app={a} />
                  <span>
                    <strong>{a.name}</strong>
                    <small>{hostname(a.url)}</small>
                  </span>
                  <Icon name="add" />
                </label>
              ))}
            {apps.every((a) => a.hidden) && (
              <p>Add an app to your registry first.</p>
            )}
          </div>
          <p className="field-note">
            {selected.length} app{selected.length === 1 ? "" : "s"} selected.
            Hidden apps never appear on public pages.
          </p>
          {formError}
          {buttons("Save selection")}
        </form>
      </Dialog>
    );
  if (modal.type === "source") {
    const a = apps.find((a) => a.id === modal.app.id) || modal.app;
    return (
      <Dialog
        title="Every detail has a story."
        subtitle={`Discovery and field provenance for ${a.name}.`}
        close={close}
      >
        <div className="source-summary">
          <AppIcon app={a} />
          <div>
            <h3>{a.name}</h3>
            <p>{hostname(a.url)}</p>
          </div>
          <Status app={a} />
        </div>
        <dl className="source-details">
          <div>
            <dt>Lifecycle</dt>
            <dd>{a.lifecycle}</dd>
          </div>
          <div>
            <dt>Last observed</dt>
            <dd>{ago(a.seen || 0)}</dd>
          </div>
          <div>
            <dt>Providers</dt>
            <dd>
              {a.sources
                ?.map(
                  (id) =>
                    providers.find((p) => p.id === id)?.name ||
                    (id.startsWith("manual:")
                      ? "Manual entry"
                      : "Removed provider"),
                )
                .join(", ")}
            </dd>
          </div>
        </dl>
        {a.probe_error && <p className="form-error">{a.probe_error}</p>}
        <div className="provenance-list">
          {Object.entries(a.fields || {})
            .filter(([k]) => k !== "probe_error")
            .map(([key, value]) => (
              <div key={key}>
                <span className="field-name">{key}</span>
                <strong>{value.value || "—"}</strong>
                <span>{value.source}</span>
              </div>
            ))}
        </div>
        <div className="dialog-actions">
          <button
            className="button"
            disabled={busy}
            onClick={() =>
              perform(
                () => api(`/apps/${a.id}/refresh`, "POST"),
                "Metadata refreshed",
              )
            }
          >
            <Icon name="refresh" />
            {busy ? "Checking…" : "Refresh metadata"}
          </button>
          <button className="button primary" onClick={close}>
            Done
          </button>
        </div>
      </Dialog>
    );
  }
  return null;
}

function PublicPage({
  slug,
  theme,
  toggleTheme,
}: {
  slug: string;
  theme: string;
  toggleTheme: () => void;
}) {
  const [data, setData] = useState<{
    name: string;
    apps: App[];
    layout: PageLayout;
  } | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  useEffect(() => {
    let live = true;
    const load = () =>
      api<{ name: string; apps: App[]; layout: PageLayout }>(
        `/public/${encodeURIComponent(slug)}`,
      )
        .then((d) => {
          if (live) {
            setData(d);
            setError("");
            document.title = `${d.name} · Apptrail`;
          }
        })
        .catch(() => {
          if (live) {
            setData(null);
            setError("This page is private or no longer available.");
          }
        });
    load();
    const timer = setInterval(load, 30000);
    const focus = () => load();
    window.addEventListener("focus", focus);
    return () => {
      live = false;
      clearInterval(timer);
      window.removeEventListener("focus", focus);
    };
  }, [slug]);
  const apps =
    data?.apps.filter((a) =>
      `${a.name} ${a.description} ${a.category} ${a.url}`
        .toLowerCase()
        .includes(query.toLowerCase()),
    ) || [];
  return (
    <div className="public-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="public-header">
        <a href="/" className="brand-link">
          <Brand />
        </a>
        <div>
          <span className="private-tag">
            <Icon name="globe" />
            Public dashboard
          </span>
          <button
            className="icon-button"
            onClick={toggleTheme}
            aria-label={`Switch to ${theme === "apptrail-dark" ? "light" : "dark"} theme`}
          >
            <Icon name={theme === "apptrail-dark" ? "sun" : "moon"} />
          </button>
          <a className="button compact" href="/">
            Owner sign in
            <Icon name="arrow" />
          </a>
        </div>
      </header>
      <main id="main">
        <div className="public-hero">
          <span className="eyebrow">A HOME FOR WHAT WE HOST</span>
          <h1>
            {data?.name || (error ? "A quieter corner." : "Finding your way…")}
          </h1>
          <p>
            {error ||
              "Good things, all in one place. Find an app and make yourself at home."}
          </p>
        </div>
        {data && (
          <>
            <div className="public-filter">
              <h2>
                Applications{" "}
                <span className="count-pill">{data.apps.length}</span>
              </h2>
              <div className="search">
                <Icon name="search" />
                <input
                  aria-label="Search this dashboard"
                  placeholder="Find your next stop…"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </div>
            </div>
            {apps.length ? (
              <DashboardCanvas
                dashboard={{ id: slug, layout: data.layout, revision: 0 }}
              >
                {apps.map((a) => (
                  <article key={a.id} className="app-card public-card">
                    <div className="card-top">
                      <AppIcon app={a} />
                      <Icon name="launch" />
                    </div>
                    <div className="card-content">
                      <h3>
                        {a.url ? (
                          <a
                            href={a.url}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            {a.name}
                          </a>
                        ) : (
                          a.name
                        )}
                      </h3>
                      <p>{a.description || "Part of our self-hosted world."}</p>
                      <div className="card-tags">
                        <span>{a.category}</span>
                      </div>
                    </div>
                    <div className="card-footer">
                      <Status app={a} />
                      <span className="public-host">{hostname(a.url)}</span>
                    </div>
                  </article>
                ))}
              </DashboardCanvas>
            ) : (
              <Empty
                title={
                  query
                    ? "No matching apps."
                    : "A little space for what’s next."
                }
              >
                {query
                  ? "Try a different name or category."
                  : "The owner hasn’t added any visible apps to this page yet."}
              </Empty>
            )}
          </>
        )}
        {error && (
          <a className="button" href="/">
            Owner sign in
            <Icon name="arrow" />
          </a>
        )}
      </main>
      <footer className="public-footer">
        <Brand />
        <span>Your infrastructure. Your starting point.</span>
        <a href="/">Powered by Apptrail</a>
      </footer>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
);
