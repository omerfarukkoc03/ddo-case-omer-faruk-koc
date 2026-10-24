// API istemcisi: access token'ı ekler, 401'de refresh token ile bir kez yeniler,
// durum değiştiren POST isteklerine otomatik Idempotency-Key koyar (çift tıklama/yeniden deneme güvenli).

export type Role = "owner" | "vet" | "technician" | "reception" | "pharmacy";

export interface User {
  user_id: number;
  role: Role;
  owner_id?: number;
  name: string;
}

interface Session {
  access_token: string;
  refresh_token: string;
  user: User;
}

const KEY = "patiklinik.session";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export function loadSession(): Session | null {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as Session) : null;
  } catch {
    return null;
  }
}

function saveSession(s: Session | null) {
  try {
    if (s) localStorage.setItem(KEY, JSON.stringify(s));
    else localStorage.removeItem(KEY);
  } catch {
    /* tarayıcı depolaması kapalıysa oturum sayfa ömrüyle sınırlı kalır */
  }
  listeners.forEach((l) => l(s));
}

const listeners = new Set<(s: Session | null) => void>();
export function onSessionChange(fn: (s: Session | null) => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

let refreshing: Promise<boolean> | null = null;

async function refresh(): Promise<boolean> {
  const s = loadSession();
  if (!s) return false;
  refreshing ??= (async () => {
    try {
      const res = await fetch("/api/auth/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: s.refresh_token }),
      });
      if (!res.ok) {
        saveSession(null);
        return false;
      }
      saveSession((await res.json()) as Session);
      return true;
    } finally {
      refreshing = null;
    }
  })();
  return refreshing;
}

function newKey() {
  return crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

export async function api<T = unknown>(path: string, init: { method?: string; body?: unknown; idempotencyKey?: string } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const doFetch = () => {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    const s = loadSession();
    if (s) headers.Authorization = `Bearer ${s.access_token}`;
    if (method !== "GET") headers["Idempotency-Key"] = init.idempotencyKey ?? newKey();
    return fetch(path, { method, headers, body: init.body === undefined ? undefined : JSON.stringify(init.body) });
  };
  let res = await doFetch();
  if (res.status === 401 && !path.startsWith("/api/auth/") && (await refresh())) {
    res = await doFetch();
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const e = data?.error ?? {};
    throw new ApiError(res.status, e.code ?? "error", e.message ?? res.statusText, e.details);
  }
  return data as T;
}

export async function login(email: string, password: string): Promise<User> {
  const s = await api<Session>("/api/auth/login", { method: "POST", body: { email, password } });
  saveSession(s);
  return s.user;
}

export async function logout() {
  const s = loadSession();
  if (s) {
    try {
      await api("/api/auth/logout", { method: "POST", body: { refresh_token: s.refresh_token } });
    } catch {
      /* oturum zaten geçersizse yine de yerelden silinir */
    }
  }
  saveSession(null);
}

export interface List<T> {
  items: T[];
  next_cursor?: string | null;
}

// ---- Biçimlendirme yardımcıları -------------------------------------------------

export const money = (kurus: number) =>
  (kurus / 100).toLocaleString("tr-TR", { style: "currency", currency: "TRY" });

export const date = (s?: string | null) => (s ? new Date(s.length === 10 ? s + "T00:00:00" : s).toLocaleDateString("tr-TR") : "—");

export const dateTime = (s?: string | null) =>
  s ? new Date(s).toLocaleString("tr-TR", { dateStyle: "short", timeStyle: "short" }) : "—";

export const time = (s: string) => new Date(s).toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit" });

export const speciesLabel: Record<string, string> = { dog: "Köpek", cat: "Kedi", rabbit: "Tavşan" };

export const roleLabel: Record<Role, string> = {
  owner: "Hayvan sahibi",
  vet: "Veteriner",
  technician: "Teknisyen",
  reception: "Resepsiyon",
  pharmacy: "Eczane / depo",
};

export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    const fields = (e.details?.fields ?? null) as Record<string, string> | null;
    const extra = fields ? " — " + Object.entries(fields).map(([k, v]) => `${k}: ${v}`).join(", ") : "";
    return `${e.message}${extra} (${e.code})`;
  }
  return e instanceof Error ? e.message : String(e);
}
