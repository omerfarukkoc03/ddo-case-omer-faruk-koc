import { useState, type ReactNode } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { errorText, isPermanent } from "./api";

// Query her ekranda aynı üç durumu tutarlı gösterir: yükleniyor, hata (yeniden dene), boş.
export function Query<T>({
  q,
  empty,
  emptyIcon,
  isEmpty,
  children,
}: {
  q: UseQueryResult<T>;
  empty?: string;
  emptyIcon?: string;
  isEmpty?: (d: T) => boolean;
  children: (d: T) => ReactNode;
}) {
  if (q.isPending) return <Loading />;
  if (q.isError)
    return (
      <div className="state state-error" role="alert">
        <strong>Veri yüklenemedi.</strong> {errorText(q.error)}
        {!isPermanent(q.error) && (
          <button className="btn btn-sm" onClick={() => q.refetch()}>
            Yeniden dene
          </button>
        )}
      </div>
    );
  const data = q.data as T;
  if (isEmpty?.(data)) return <Empty icon={emptyIcon}>{empty ?? "Kayıt bulunamadı."}</Empty>;
  return <>{children(data)}</>;
}

export function Empty({ icon = "🗂️", children }: { icon?: string; children: ReactNode }) {
  return (
    <div className="state state-empty">
      <span className="emoji" aria-hidden>{icon}</span>
      <span>{children}</span>
    </div>
  );
}

export function Loading({ label = "Yükleniyor…" }: { label?: string }) {
  return (
    <div className="state" aria-busy="true">
      <span className="spinner" /> {label}
    </div>
  );
}

export function Page({ title, subtitle, actions, children }: { title: string; subtitle?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="page">
      <header className="page-head">
        <div>
          <h1>{title}</h1>
          {subtitle && <p>{subtitle}</p>}
        </div>
        {actions && <div className="page-actions">{actions}</div>}
      </header>
      {children}
    </section>
  );
}

export function Card({ title, sub, children, actions, className }: { title?: string; sub?: ReactNode; children: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <div className={`card ${className ?? ""}`}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            {title && <h2>{title}</h2>}
            {sub && <div className="sub">{sub}</div>}
          </div>
          {actions}
        </div>
      )}
      {children}
    </div>
  );
}

export function Stat({ icon, tone = "brand", value, label: text }: { icon: string; tone?: string; value: ReactNode; label: string }) {
  return (
    <div className="card stat">
      <span className={`icon tone-${tone}`} aria-hidden>{icon}</span>
      <div>
        <div className="value">{value}</div>
        <div className="label">{text}</div>
      </div>
    </div>
  );
}

const tone: Record<string, string> = {
  scheduled: "info", checked_in: "warn", completed: "ok", cancelled: "muted", no_show: "bad",
  planned: "info", administered: "ok", overdue: "bad",
  open: "warn", paid: "ok", void: "muted",
  active: "warn", dispensed: "ok", discharged: "ok",
  empty: "muted", occupied: "bad", cleaning: "warn", ready: "ok",
  emergency: "bad", normal: "muted", surgery: "violet", vaccination: "info", exam: "muted", control: "muted",
  primary: "muted", booster: "violet",
  ok: "ok", warn: "warn", bad: "bad", info: "info", muted: "muted", violet: "violet",
};

const labels: Record<string, string> = {
  scheduled: "Planlandı", checked_in: "Geldi", completed: "Tamamlandı", cancelled: "İptal", no_show: "Gelmedi",
  planned: "Planlı", administered: "Yapıldı", overdue: "Gecikmiş", primary: "Seri", booster: "Rapel",
  open: "Açık", paid: "Ödendi", void: "İptal",
  active: "Aktif", dispensed: "Teslim edildi", discharged: "Taburcu",
  empty: "Boş", occupied: "Dolu", cleaning: "Temizlik", ready: "Hazır",
  emergency: "ACİL", normal: "Normal", exam: "Muayene", vaccination: "Aşı", surgery: "Ameliyat", control: "Kontrol",
  billing_owner: "fatura sorumlusu", first_owner: "ilk kayıtlı sahip", explicit: "elle seçildi",
  cash: "Nakit", card: "Kart", transfer: "Havale",
  owner: "sahip", co_owner: "ortak sahip", caretaker: "bakıcı",
  male: "Erkek", female: "Dişi", unknown: "Bilinmiyor",
  general: "genel", isolation: "izolasyon",
};

export const label = (s: string) => labels[s] ?? s;

export const breedLabel = (b: string) => (!b || b.toLowerCase() === "mixed" ? "Melez" : b);

export function Badge({ value, text }: { value: string; text?: string }) {
  return <span className={`badge badge-${tone[value] ?? "muted"}`}>{text ?? label(value)}</span>;
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <div className="state state-error" role="alert">
      {errorText(error)}
    </div>
  );
}

export function SuccessBox({ children }: { children: ReactNode }) {
  return (
    <div className="state state-ok" role="status">
      {children}
    </div>
  );
}

// Toggle "yeni kayıt" gibi isteğe bağlı formları açılır panel olarak gösterir.
export function Toggle({ label: text, children, defaultOpen = false }: { label: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className={`disclosure ${open ? "open" : ""}`}>
      <button className="disclosure-head" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span className="plus" aria-hidden>{open ? "−" : "+"}</span> {text}
      </button>
      {open && <div className="disclosure-body">{children}</div>}
    </div>
  );
}

export function initials(name: string) {
  return name
    .replace(/^Dr\.\s*/, "")
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]!.toLocaleUpperCase("tr-TR"))
    .join("");
}

const rtf = new Intl.RelativeTimeFormat("tr-TR", { numeric: "auto" });

// ago "2 dakika önce" gibi göreli zaman üretir.
export function ago(iso: string, now = Date.now()) {
  const sec = Math.round((new Date(iso).getTime() - now) / 1000);
  const abs = Math.abs(sec);
  if (abs < 45) return "az önce";
  if (abs < 3600) return rtf.format(Math.round(sec / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(sec / 3600), "hour");
  return rtf.format(Math.round(sec / 86400), "day");
}

// WeightChart kilo geçmişini bağımlılıksız SVG çizgi grafik olarak gösterir (bonus: kilo takibi).
export function WeightChart({ points }: { points: { weight_kg: string; measured_at: string }[] }) {
  if (points.length < 2) return <Empty icon="⚖️">Grafik için en az iki ölçüm gerekli.</Empty>;
  const W = 600, H = 200, PL = 46, PR = 16, PT = 14, PB = 28;
  const xs = points.map((p) => new Date(p.measured_at).getTime());
  const ys = points.map((p) => Number(p.weight_kg));
  const [x0, x1] = [Math.min(...xs), Math.max(...xs)];
  const lo = Math.min(...ys), hi = Math.max(...ys);
  const pad = Math.max(0.2, (hi - lo) * 0.15);
  const [y0, y1] = [Math.max(0, lo - pad), hi + pad];
  const sx = (x: number) => PL + ((x - x0) / Math.max(1, x1 - x0)) * (W - PL - PR);
  const sy = (y: number) => H - PB - ((y - y0) / Math.max(0.001, y1 - y0)) * (H - PT - PB);
  const line = xs.map((x, i) => `${i ? "L" : "M"}${sx(x).toFixed(1)},${sy(ys[i]).toFixed(1)}`).join(" ");
  const area = `${line} L${sx(x1).toFixed(1)},${H - PB} L${sx(x0).toFixed(1)},${H - PB} Z`;
  const ticks = [y0, (y0 + y1) / 2, y1];
  const last = ys[ys.length - 1], first = ys[0];
  return (
    <div>
      <div className="row small" style={{ marginBottom: 6 }}>
        <span className="muted">Son ölçüm</span> <strong>{last.toLocaleString("tr-TR")} kg</strong>
        <span className={`badge ${last >= first ? "badge-ok" : "badge-warn"}`}>
          {last >= first ? "▲" : "▼"} {Math.abs(last - first).toLocaleString("tr-TR", { maximumFractionDigits: 1 })} kg
        </span>
        <span className="faint">· {points.length} ölçüm</span>
      </div>
      <svg className="chart" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="Kilo geçmişi grafiği">
        {ticks.map((t) => (
          <g key={t}>
            <line x1={PL} x2={W - PR} y1={sy(t)} y2={sy(t)} className="grid-line" />
            <text x={PL - 8} y={sy(t) + 4} className="tick" textAnchor="end">{t.toFixed(1)}</text>
          </g>
        ))}
        <path d={area} className="area" opacity={0.6} />
        <path d={line} className="line" />
        <text x={PL} y={H - 8} className="tick">{new Date(x0).toLocaleDateString("tr-TR")}</text>
        <text x={W - PR} y={H - 8} className="tick" textAnchor="end">{new Date(x1).toLocaleDateString("tr-TR")}</text>
        {xs.map((x, i) => (
          <circle key={i} cx={sx(x)} cy={sy(ys[i])} r={3.5} className="dot">
            <title>{`${new Date(x).toLocaleDateString("tr-TR")}: ${ys[i]} kg`}</title>
          </circle>
        ))}
      </svg>
    </div>
  );
}

// ageText doğum tarihinden okunur yaş üretir: "10 haftalık", "5 aylık", "3 yaş".
export function ageText(birth: string, now = new Date()) {
  const days = Math.floor((now.getTime() - new Date(birth + "T00:00:00").getTime()) / 86_400_000);
  if (days < 0) return "—";
  if (days < 112) return `${Math.max(1, Math.floor(days / 7))} haftalık`;
  if (days < 730) return `${Math.floor(days / 30.44)} aylık`;
  return `${Math.floor(days / 365.25)} yaş`;
}
