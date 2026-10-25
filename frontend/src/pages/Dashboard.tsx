import { useQuery } from "@tanstack/react-query";
import { api, money } from "../api";
import type { AuditRow, LiveStats } from "../types";
import { Card, Page, Query, Stat, ago, label } from "../ui";

// Denetim kaydı aksiyonlarının pano metinleri; backend'deki tüm aksiyonları kapsar.
const actions: Record<string, { text: string; icon: string; tone: string }> = {
  "appointment.create": { text: "Randevu oluşturuldu", icon: "📅", tone: "info" },
  "appointment.status": { text: "Randevu durumu güncellendi", icon: "🔄", tone: "muted" },
  "appointment.emergency": { text: "Acil vaka kabul edildi", icon: "🚨", tone: "bad" },
  "appointment.rescheduled_for_emergency": { text: "Randevu acil vaka için kaydırıldı", icon: "↪️", tone: "warn" },
  "appointment.cancelled_for_emergency": { text: "Randevu acil vaka nedeniyle iptal", icon: "⛔", tone: "bad" },
  "examination.create": { text: "Muayene kaydedildi", icon: "🩺", tone: "info" },
  "invoice.create": { text: "Fatura kesildi", icon: "🧾", tone: "muted" },
  "invoice.payment": { text: "Tahsilat alındı", icon: "💳", tone: "ok" },
  "package.purchase": { text: "Aşı paketi satıldı", icon: "🎁", tone: "violet" },
  "animal.create": { text: "Yeni hasta kaydı", icon: "🐾", tone: "brand" },
  "animal.weight": { text: "Kilo kaydedildi", icon: "⚖️", tone: "muted" },
  "animal.owner_add": { text: "Ortak sahip eklendi", icon: "👥", tone: "muted" },
  "animal.billing_owner": { text: "Fatura sorumlusu değişti", icon: "👤", tone: "muted" },
  "animal.merge": { text: "Mükerrer hayvan birleştirildi", icon: "🔀", tone: "violet" },
  "animal.merge_undo": { text: "Hayvan birleştirmesi geri alındı", icon: "↩️", tone: "warn" },
  "owner.create": { text: "Yeni sahip kaydı", icon: "👤", tone: "brand" },
  "owner.merge": { text: "Mükerrer sahip birleştirildi", icon: "🔀", tone: "violet" },
  "owner.merge_undo": { text: "Sahip birleştirmesi geri alındı", icon: "↩️", tone: "warn" },
  "owner.portal_invite": { text: "Portal daveti oluşturuldu", icon: "✉️", tone: "info" },
  "owner.portal_activate": { text: "Sahip portal hesabını açtı", icon: "🔑", tone: "ok" },
  "vaccine.administer": { text: "Aşı uygulandı", icon: "💉", tone: "ok" },
  "prescription.create": { text: "Reçete yazıldı", icon: "💊", tone: "info" },
  "prescription.create_override": { text: "Sınır üstü reçete (gerekçeli onay)", icon: "⚠️", tone: "warn" },
  "prescription.dispense": { text: "İlaç teslim edildi", icon: "📦", tone: "ok" },
  "stock.receive": { text: "Stok girişi", icon: "🚚", tone: "muted" },
  "cage.transition": { text: "Kafes durumu değişti", icon: "🏥", tone: "muted" },
  "hospitalization.admit": { text: "Hasta yatırıldı", icon: "🛏️", tone: "warn" },
  "hospitalization.discharge": { text: "Hasta taburcu edildi", icon: "🏠", tone: "ok" },
  "system.seed": { text: "Demo verisi yüklendi", icon: "🌱", tone: "muted" },
};

const roleText: Record<string, string> = { vet: "veteriner", technician: "teknisyen", reception: "resepsiyon", pharmacy: "eczane", owner: "sahip" };

function Event({ e }: { e: AuditRow }) {
  const a = actions[e.action] ?? { text: e.action, icon: "•", tone: "muted" };
  return (
    <li>
      <span className={`ev-icon tone-${a.tone}`} aria-hidden>{a.icon}</span>
      <span className="ev-text">
        {a.text} <span className="faint small">#{e.entity_id}{e.actor_role ? ` · ${roleText[e.actor_role] ?? e.actor_role}` : ""}</span>
      </span>
      <span className="ev-time" title={new Date(e.at).toLocaleString("tr-TR")}>{ago(e.at)}</span>
    </li>
  );
}

const cageColors: Record<string, string> = { occupied: "var(--bad)", cleaning: "var(--warn)", ready: "var(--ok)", empty: "var(--faint)" };

export default function Dashboard() {
  // Simülatör API üzerinden sürekli olay üretir; pano 3 sn'de bir yenilenerek sistemin canlı olduğunu gösterir.
  const q = useQuery({ queryKey: ["stats"], queryFn: () => api<LiveStats>("/api/stats/live"), refetchInterval: 3000 });
  return (
    <Page title="Klinik panosu" subtitle="Günün özeti ve sistemde gerçekleşen son olaylar; veriler 3 saniyede bir yenilenir.">
      <Query q={q}>
        {(s) => {
          const live = s.events_last_minute > 0;
          const totalCages = Object.values(s.cages).reduce((a, b) => a + b, 0) || 1;
          return (
            <div className="stack">
              <div className="row small">
                <span className={`live-dot ${live ? "" : "idle"}`} aria-hidden />
                <strong>{live ? "Canlı" : "Sakin"}</strong>
                <span className="muted">{live ? `son dakikada ${s.events_last_minute} olay` : "son dakikada olay yok"}</span>
              </div>
              <div className="grid grid-stats">
                <Stat icon="🐾" tone="brand" value={s.animals.toLocaleString("tr-TR")} label="Aktif hasta" />
                <Stat icon="📅" tone="info" value={s.appointments_today} label="Bugünkü randevu" />
                <Stat icon="🚨" tone="bad" value={s.emergencies_today} label="Bugünkü acil vaka" />
                <Stat icon="🛏️" tone="warn" value={s.active_inpatients} label="Yatan hasta" />
                <Stat icon="💉" tone="violet" value={s.vaccine_doses_due_7d.toLocaleString("tr-TR")} label="7 gün içinde aşı" />
                <Stat icon="🧾" tone="warn" value={s.open_invoices.toLocaleString("tr-TR")} label="Açık fatura" />
                <Stat icon="💰" tone="ok" value={money(s.open_balance_kurus)} label="Tahsil edilecek bakiye" />
                <Stat icon="👤" tone="muted" value={s.owners.toLocaleString("tr-TR")} label="Kayıtlı sahip" />
              </div>
              <div className="grid grid-2">
                <div className="stack">
                  <Card title="Kafes doluluğu" sub={`${s.cages.occupied ?? 0} / ${totalCages} kafes dolu`}>
                    <div className="cage-bar" role="img" aria-label="Kafes durumlarının oranı">
                      {["occupied", "cleaning", "ready", "empty"].map((k) => (
                        <span key={k} style={{ width: `${((s.cages[k] ?? 0) / totalCages) * 100}%`, background: cageColors[k] }} />
                      ))}
                    </div>
                    <div className="row small" style={{ marginTop: 10 }}>
                      {["occupied", "cleaning", "ready", "empty"].map((k) => (
                        <span key={k} className="row" style={{ gap: 5 }}>
                          <span style={{ width: 10, height: 10, borderRadius: 3, background: cageColors[k], display: "inline-block" }} />
                          {label(k)}: <strong>{s.cages[k] ?? 0}</strong>
                        </span>
                      ))}
                    </div>
                  </Card>
                  {s.pending_duplicate_owners > 0 && (
                    <Card title="Veri kalitesi" sub="Aynı telefon numarasıyla kayıtlı olası mükerrer sahipler">
                      <div className="row">
                        <span className="badge badge-violet">{s.pending_duplicate_owners} çift</span>
                        <span className="small muted">Resepsiyon “Mükerrer kayıtlar” ekranından birleştirebilir; birleştirme geri alınabilir.</span>
                      </div>
                    </Card>
                  )}
                </div>
                <Card title="Son olaylar" sub="Denetim kaydından (append-only)">
                  <ul className="events">
                    {s.recent_events.map((e) => <Event key={e.id} e={e} />)}
                  </ul>
                </Card>
              </div>
            </div>
          );
        }}
      </Query>
    </Page>
  );
}
