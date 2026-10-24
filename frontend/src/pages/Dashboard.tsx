import { useQuery } from "@tanstack/react-query";
import { api, dateTime, money } from "../api";
import type { LiveStats } from "../types";
import { Card, Page, Query, label } from "../ui";

const actionText: Record<string, string> = {
  "appointment.create": "Randevu oluşturuldu",
  "appointment.emergency": "ACİL vaka kabul edildi",
  "appointment.rescheduled_for_emergency": "Randevu acil vaka için kaydırıldı",
  "appointment.status": "Randevu durumu değişti",
  "invoice.payment": "Tahsilat alındı",
  "invoice.create": "Fatura kesildi",
  "animal.weight": "Kilo kaydedildi",
  "animal.create": "Yeni hasta kaydı",
  "vaccine.administer": "Aşı uygulandı",
  "prescription.create": "Reçete yazıldı",
  "prescription.create_override": "Sınır üstü reçete (gerekçeli)",
  "prescription.dispense": "İlaç teslim edildi",
  "cage.transition": "Kafes durumu değişti",
  "system.seed": "Demo verisi yüklendi",
};

export default function Dashboard() {
  // Simülatör API üzerinden sürekli olay üretir; pano 3 sn'de bir yenilenerek sistemin canlı olduğunu gösterir.
  const q = useQuery({ queryKey: ["stats"], queryFn: () => api<LiveStats>("/api/stats/live"), refetchInterval: 3000 });
  return (
    <Page title="Klinik panosu">
      <Query q={q}>
        {(s) => {
          const live = s.events_last_minute > 0;
          return (
            <div className="stack">
              <div className="row">
                <span className={`live-dot ${live ? "" : "idle"}`} aria-hidden />
                <span className="small muted">
                  {live ? `Canlı — son dakikada ${s.events_last_minute} olay` : "Son dakikada olay yok"} · sunucu saati {dateTime(s.server_time)}
                </span>
              </div>
              <div className="grid grid-stats">
                <Stat label="Aktif hasta" value={s.animals.toLocaleString("tr-TR")} />
                <Stat label="Sahip" value={s.owners.toLocaleString("tr-TR")} />
                <Stat label="Bugünkü randevu" value={s.appointments_today} />
                <Stat label="Bugünkü acil vaka" value={s.emergencies_today} />
                <Stat label="Yatan hasta" value={s.active_inpatients} />
                <Stat label="7 gün içinde aşı" value={s.vaccine_doses_due_7d.toLocaleString("tr-TR")} />
                <Stat label="Açık fatura" value={s.open_invoices.toLocaleString("tr-TR")} />
                <Stat label="Açık bakiye" value={money(s.open_balance_kurus)} />
              </div>
              <div className="grid grid-2">
                <Card title="Kafesler">
                  <div className="row">
                    {Object.entries(s.cages).map(([k, v]) => (
                      <span key={k} className={`badge badge-${k === "occupied" ? "bad" : k === "cleaning" ? "warn" : k === "ready" ? "ok" : "muted"}`}>
                        {label(k)}: {v}
                      </span>
                    ))}
                  </div>
                  {s.pending_duplicate_owners > 0 && (
                    <p className="small muted">Birleştirilmeyi bekleyen olası mükerrer sahip çifti: {s.pending_duplicate_owners}</p>
                  )}
                </Card>
                <Card title="Son olaylar">
                  <ul className="events">
                    {s.recent_events.map((e) => (
                      <li key={e.id}>
                        <span>
                          {actionText[e.action] ?? e.action} <span className="muted small">#{e.entity_id}</span>
                        </span>
                        <span className="muted small">{dateTime(e.at)}</span>
                      </li>
                    ))}
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

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="card stat">
      <div className="value">{value}</div>
      <div className="label">{label}</div>
    </div>
  );
}
