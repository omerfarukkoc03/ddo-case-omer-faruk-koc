import { useState, type ReactNode } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { errorText, isPermanent } from "./api";

// Query her ekranda aynı üç durumu tutarlı gösterir: yükleniyor, hata (yeniden dene), boş.
export function Query<T>({
  q,
  empty,
  isEmpty,
  children,
}: {
  q: UseQueryResult<T>;
  empty?: string;
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
  if (isEmpty?.(data)) return <div className="state state-empty">{empty ?? "Kayıt bulunamadı."}</div>;
  return <>{children(data)}</>;
}

export function Loading({ label = "Yükleniyor…" }: { label?: string }) {
  return (
    <div className="state" aria-busy="true">
      <span className="spinner" /> {label}
    </div>
  );
}

export function Page({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="page">
      <header className="page-head">
        <h1>{title}</h1>
        {actions && <div className="page-actions">{actions}</div>}
      </header>
      {children}
    </section>
  );
}

export function Card({ title, children, actions }: { title?: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="card">
      {(title || actions) && (
        <div className="card-head">
          {title && <h2>{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </div>
  );
}

const tone: Record<string, string> = {
  scheduled: "info", checked_in: "warn", completed: "ok", cancelled: "muted", no_show: "bad",
  planned: "info", administered: "ok", overdue: "bad",
  open: "warn", paid: "ok", void: "muted",
  active: "warn", dispensed: "ok", discharged: "ok",
  empty: "muted", occupied: "bad", cleaning: "warn", ready: "ok",
  emergency: "bad", normal: "muted",
  ok: "ok", warn: "warn", bad: "bad", info: "info", muted: "muted",
};

const labels: Record<string, string> = {
  scheduled: "Planlandı", checked_in: "Geldi", completed: "Tamamlandı", cancelled: "İptal", no_show: "Gelmedi",
  planned: "Planlı", administered: "Yapıldı", overdue: "Gecikmiş", primary: "Seri", booster: "Rapel",
  open: "Açık", paid: "Ödendi", void: "İptal",
  active: "Aktif", dispensed: "Teslim edildi", discharged: "Taburcu",
  empty: "Boş", occupied: "Dolu", cleaning: "Temizlik", ready: "Hazır",
  emergency: "ACİL", normal: "Normal", exam: "Muayene", vaccination: "Aşı", surgery: "Ameliyat", control: "Kontrol",
  billing_owner: "fatura sorumlusu", first_owner: "ilk kayıtlı sahip", explicit: "elle seçildi",
};

export const label = (s: string) => labels[s] ?? s;

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

export function Toggle({ label: text, children, defaultOpen = false }: { label: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="toggle">
      <button className="btn btn-ghost" onClick={() => setOpen(!open)} aria-expanded={open}>
        {open ? "▾" : "▸"} {text}
      </button>
      {open && <div className="toggle-body">{children}</div>}
    </div>
  );
}

// WeightChart kilo geçmişini bağımlılıksız SVG çizgi grafik olarak gösterir (bonus: kilo takibi).
export function WeightChart({ points }: { points: { weight_kg: string; measured_at: string }[] }) {
  if (points.length < 2) return <div className="state state-empty">Grafik için en az iki ölçüm gerekli.</div>;
  const W = 600, H = 180, P = 32;
  const xs = points.map((p) => new Date(p.measured_at).getTime());
  const ys = points.map((p) => Number(p.weight_kg));
  const [x0, x1] = [Math.min(...xs), Math.max(...xs)];
  const [y0, y1] = [Math.min(...ys) * 0.95, Math.max(...ys) * 1.05];
  const sx = (x: number) => P + ((x - x0) / Math.max(1, x1 - x0)) * (W - 2 * P);
  const sy = (y: number) => H - P - ((y - y0) / Math.max(0.001, y1 - y0)) * (H - 2 * P);
  const d = xs.map((x, i) => `${i ? "L" : "M"}${sx(x).toFixed(1)},${sy(ys[i]).toFixed(1)}`).join(" ");
  return (
    <svg className="chart" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="Kilo geçmişi grafiği">
      <line x1={P} y1={H - P} x2={W - P} y2={H - P} className="axis" />
      <text x={P} y={H - 8} className="tick">{new Date(x0).toLocaleDateString("tr-TR")}</text>
      <text x={W - P} y={H - 8} className="tick" textAnchor="end">{new Date(x1).toLocaleDateString("tr-TR")}</text>
      <text x={4} y={sy(y1 / 1.05) + 4} className="tick">{(y1 / 1.05).toFixed(1)} kg</text>
      <text x={4} y={sy(y0 / 0.95) + 4} className="tick">{(y0 / 0.95).toFixed(1)} kg</text>
      <path d={d} className="line" />
      {xs.map((x, i) => (
        <circle key={i} cx={sx(x)} cy={sy(ys[i])} r={3} className="dot">
          <title>{`${new Date(x).toLocaleDateString("tr-TR")}: ${ys[i]} kg`}</title>
        </circle>
      ))}
    </svg>
  );
}
