import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, dateTime, speciesLabel, time } from "../api";
import { useHasRole, useUser } from "../auth";
import type { Appointment, Resource } from "../types";
import { Badge, Card, Empty, ErrorBox, Field, Page, Query, SuccessBox, Toggle, label } from "../ui";

const today = () => new Date().toLocaleDateString("sv-SE"); // YYYY-MM-DD yerel

export default function Appointments() {
  const user = useUser();
  const isOwner = user?.role === "owner";
  const [day, setDay] = useState(today());
  const shift = (n: number) => {
    const d = new Date(day + "T12:00:00");
    d.setDate(d.getDate() + n);
    setDay(d.toLocaleDateString("sv-SE"));
  };
  const q = useQuery({
    queryKey: ["appointments", day],
    queryFn: () => api<{ items: Appointment[] }>(`/api/appointments?date=${day}`),
    refetchInterval: 10_000,
  });
  const canCreate = useHasRole("reception");
  const canEmergency = useHasRole("vet", "reception");
  const dayText = new Date(day + "T12:00:00").toLocaleDateString("tr-TR", { weekday: "long", day: "numeric", month: "long", year: "numeric" });

  return (
    <Page
      title={isOwner ? "Randevularım" : "Randevu takvimi"}
      subtitle={isOwner ? "Hayvanlarınızın yaklaşan ve geçmiş randevuları." : `${dayText} · veteriner bazında günlük plan, 10 saniyede bir yenilenir.`}
      actions={
        !isOwner && (
          <div className="row">
            <button className="btn btn-sm" onClick={() => shift(-1)} aria-label="Önceki gün">‹</button>
            <input type="date" value={day} onChange={(e) => setDay(e.target.value)} style={{ width: 160 }} aria-label="Tarih" />
            <button className="btn btn-sm" onClick={() => shift(1)} aria-label="Sonraki gün">›</button>
            <button className="btn btn-sm" onClick={() => setDay(today())}>Bugün</button>
          </div>
        )
      }
    >
      <div className="stack">
        {(canEmergency || canCreate) && (
          <div className="grid grid-2">
            {canEmergency && <EmergencyForm />}
            {canCreate && <Toggle label="Yeni randevu oluştur"><AppointmentForm day={day} /></Toggle>}
          </div>
        )}
        <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Bu gün için randevu yok." emptyIcon="📅">
          {(d) => (isOwner ? <OwnerList items={d.items} /> : <DayBoard items={d.items} />)}
        </Query>
      </div>
    </Page>
  );
}

function OwnerList({ items }: { items: Appointment[] }) {
  const now = Date.now();
  const upcoming = items.filter((a) => new Date(a.starts_at).getTime() >= now);
  const past = items.filter((a) => new Date(a.starts_at).getTime() < now).reverse();
  const render = (list: Appointment[]) => (
    <div className="timeline">
      {list.map((a) => (
        <div key={a.id} className={`appt s-${a.status} ${a.priority === "emergency" ? "emergency" : ""}`}>
          <div className="time">
            {time(a.starts_at)}
            <small>{new Date(a.starts_at).toLocaleDateString("tr-TR")}</small>
          </div>
          <div className="body">
            <div className="title">
              <strong>{a.animal_name}</strong>
              <Badge value={a.priority === "emergency" ? "emergency" : a.kind} />
              <span className="spacer" />
              <Badge value={a.status} />
            </div>
            <span className="sub-line">{a.vet_name} · {a.room_name}</span>
          </div>
        </div>
      ))}
    </div>
  );
  return (
    <div className="grid grid-2">
      <Card title="Yaklaşan" sub={`${upcoming.length} randevu`}>
        {upcoming.length ? render(upcoming) : <Empty icon="📅">Yaklaşan randevu yok.</Empty>}
      </Card>
      <Card title="Geçmiş" sub={`${past.length} randevu`}>
        {past.length ? render(past) : <Empty icon="🕘">Geçmiş randevu yok.</Empty>}
      </Card>
    </div>
  );
}

type Filter = "all" | "waiting" | "emergency" | "done" | "cancelled";

const filters: { key: Filter; text: string; match: (a: Appointment) => boolean }[] = [
  { key: "all", text: "Tümü", match: () => true },
  { key: "waiting", text: "Bekleyen", match: (a) => a.status === "scheduled" || a.status === "checked_in" },
  { key: "emergency", text: "Acil", match: (a) => a.priority === "emergency" },
  { key: "done", text: "Tamamlanan", match: (a) => a.status === "completed" },
  { key: "cancelled", text: "İptal / gelmedi", match: (a) => a.status === "cancelled" || a.status === "no_show" },
];

function DayBoard({ items }: { items: Appointment[] }) {
  const qc = useQueryClient();
  const canStatus = useHasRole("vet", "technician", "reception");
  const [filter, setFilter] = useState<Filter>("all");
  const m = useMutation({
    mutationFn: ({ id, status }: { id: number; status: string }) => api(`/api/appointments/${id}/status`, { method: "POST", body: { status } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appointments"] }),
  });
  const active = filters.find((f) => f.key === filter)!;
  const byVet = new Map<string, Appointment[]>();
  items.forEach((a) => byVet.set(a.vet_name, [...(byVet.get(a.vet_name) ?? []), a]));
  return (
    <div className="stack">
      <div className="chips" role="tablist" aria-label="Randevu filtresi">
        {filters.map((f) => (
          <button key={f.key} role="tab" aria-selected={filter === f.key} className={`chip ${filter === f.key ? "active" : ""}`} onClick={() => setFilter(f.key)}>
            {f.text}
            <span className="count">{items.filter(f.match).length}</span>
          </button>
        ))}
      </div>
      <ErrorBox error={m.error} />
      <div className="board">
        {[...byVet.entries()].map(([vet, list]) => {
          const shown = list.filter(active.match);
          return (
            <Card
              key={vet}
              className="board-col"
              title={vet}
              sub={`${list.filter((a) => a.status !== "cancelled").length} randevu · ${list.filter((a) => a.priority === "emergency").length} acil`}
            >
              {shown.length === 0 ? (
                <Empty icon="✅">Bu filtrede randevu yok.</Empty>
              ) : (
                <div className="timeline">
                  {shown.map((a) => (
                    <div key={a.id} className={`appt s-${a.status} ${a.priority === "emergency" ? "emergency" : ""}`}>
                      <div className="time">
                        {time(a.starts_at)}
                        <small>{time(a.ends_at)}</small>
                      </div>
                      <div className="body">
                        <div className="title">
                          <Link to={`/animals/${a.animal_id}`}>{a.animal_name}</Link>
                          <span className="faint small">{speciesLabel[a.species]}</span>
                          <span className="spacer" />
                          <Badge value={a.status} />
                        </div>
                        <div className="row small" style={{ gap: 6 }}>
                          <Badge value={a.priority === "emergency" ? "emergency" : a.kind} />
                          <span className="muted">{a.room_name}</span>
                          {a.reschedule_count > 0 && a.original_starts_at && (
                            <span className="badge badge-warn" title="Acil vaka nedeniyle yeniden planlandı">↪ ilk saat {time(a.original_starts_at)}</span>
                          )}
                        </div>
                        {a.priority === "emergency" && a.reason && <span className="sub-line">{a.reason}</span>}
                        {canStatus && (a.status === "scheduled" || a.status === "checked_in") && (
                          <div className="actions">
                            {a.status === "scheduled" && (
                              <>
                                <button className="btn btn-sm" disabled={m.isPending} onClick={() => m.mutate({ id: a.id, status: "checked_in" })}>Geldi</button>
                                <button className="btn btn-sm btn-ghost" disabled={m.isPending} onClick={() => m.mutate({ id: a.id, status: "no_show" })}>Gelmedi</button>
                                <button className="btn btn-sm btn-ghost" disabled={m.isPending} onClick={() => m.mutate({ id: a.id, status: "cancelled" })}>İptal</button>
                              </>
                            )}
                            {a.status === "checked_in" && (
                              <button className="btn btn-sm btn-primary" disabled={m.isPending} onClick={() => m.mutate({ id: a.id, status: "completed" })}>Tamamla</button>
                            )}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          );
        })}
      </div>
    </div>
  );
}

function useResources() {
  return useQuery({ queryKey: ["resources"], queryFn: () => api<{ items: Resource[] }>("/api/resources"), staleTime: 300_000 });
}

function AppointmentForm({ day }: { day: string }) {
  const qc = useQueryClient();
  const res = useResources();
  const [f, setF] = useState({ animal_id: "", vet_id: "1", room_id: "4", kind: "exam", time: "10:00", duration_min: "30" });
  const vets = res.data?.items.filter((r) => r.kind === "vet") ?? [];
  const rooms = res.data?.items.filter((r) => r.kind === (f.kind === "surgery" ? "surgery_room" : "exam_room")) ?? [];
  // Tür değişince oda listesi değişir; seçili oda listede yoksa ilk uygun oda kullanılır.
  const roomId = rooms.some((r) => String(r.id) === f.room_id) ? f.room_id : String(rooms[0]?.id ?? "");
  const m = useMutation({
    mutationFn: () =>
      api<Appointment>("/api/appointments", {
        method: "POST",
        body: {
          animal_id: Number(f.animal_id), vet_id: Number(f.vet_id), room_id: Number(roomId), kind: f.kind,
          starts_at: new Date(`${day}T${f.time}:00`).toISOString(), duration_min: Number(f.duration_min),
        },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appointments"] }),
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={set("animal_id")} required /></Field>
        <Field label="Tür">
          <select value={f.kind} onChange={set("kind")}>
            {["exam", "vaccination", "control", "surgery"].map((k) => <option key={k} value={k}>{label(k)}</option>)}
          </select>
        </Field>
        <Field label="Veteriner"><select value={f.vet_id} onChange={set("vet_id")}>{vets.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></Field>
        <Field label="Oda"><select value={roomId} onChange={set("room_id")}>{rooms.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></Field>
        <Field label="Saat" hint={new Date(day + "T12:00:00").toLocaleDateString("tr-TR")}><input type="time" step={300} value={f.time} onChange={set("time")} /></Field>
        <Field label="Süre (dk)"><input type="number" step={5} min={10} max={240} value={f.duration_min} onChange={set("duration_min")} /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Randevu #{m.data.id} oluşturuldu: {m.data.animal_name}, {time(m.data.starts_at)}.</SuccessBox>}
      <div><button className="btn btn-primary" disabled={m.isPending}>Randevu oluştur</button></div>
    </form>
  );
}

interface EmergencyResult {
  emergency: Appointment;
  moves: { appointment_id: number; from_start: string; to_start: string; from_vet_id: number; to_vet_id: number; cost: number }[];
  cancelled_for_manual_rebooking: number[];
  disruption_cost: number;
  wait_minutes: number;
  explanation: string;
}

function EmergencyForm() {
  const qc = useQueryClient();
  const [f, setF] = useState({ animal_id: "", reason: "", duration_min: "30", room_kind: "exam_room" });
  const m = useMutation({
    mutationFn: () =>
      api<EmergencyResult>("/api/appointments/emergency", {
        method: "POST",
        body: { animal_id: Number(f.animal_id), reason: f.reason, duration_min: Number(f.duration_min), room_kind: f.room_kind },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appointments"] }),
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <Card title="🚨 Acil vaka kabulü" sub="Şimdi başlar; çakışan randevular en az bozulmayla boş saatlere taşınır.">
      <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="form-row">
          <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={set("animal_id")} required /></Field>
          <Field label="Süre (dk)"><input type="number" min={10} max={240} value={f.duration_min} onChange={set("duration_min")} /></Field>
          <Field label="Oda türü">
            <select value={f.room_kind} onChange={set("room_kind")}>
              <option value="exam_room">Muayene odası</option>
              <option value="surgery_room">Ameliyathane</option>
            </select>
          </Field>
        </div>
        <Field label="Şikâyet"><input value={f.reason} onChange={set("reason")} placeholder="ör. trafik kazası" required /></Field>
        <ErrorBox error={m.error} />
        <div><button className="btn btn-danger" disabled={m.isPending}>Acil vakayı şimdi başlat</button></div>
      </form>
      {m.data && (
        <div className="stack" style={{ marginTop: 14 }}>
          <SuccessBox>{m.data.explanation}</SuccessBox>
          <div className="summary">
            <div><b>{time(m.data.emergency.starts_at)}</b>başlangıç{m.data.wait_minutes > 0 ? ` (${m.data.wait_minutes} dk bekleme)` : ""}</div>
            <div><b>{m.data.moves.length}</b>taşınan randevu</div>
            <div><b>{m.data.disruption_cost}</b>bozulma maliyeti</div>
          </div>
          {m.data.moves.length > 0 && (
            <div className="table-wrap">
              <table>
                <thead><tr><th>Randevu</th><th>Eski saat</th><th>Yeni saat</th><th>Veteriner</th><th className="num">Maliyet</th></tr></thead>
                <tbody>
                  {m.data.moves.map((mv) => (
                    <tr key={mv.appointment_id}>
                      <td>#{mv.appointment_id}</td>
                      <td>{dateTime(mv.from_start)}</td>
                      <td>{dateTime(mv.to_start)}</td>
                      <td>{mv.from_vet_id === mv.to_vet_id ? "aynı" : `#${mv.from_vet_id} → #${mv.to_vet_id}`}</td>
                      <td className="num">{mv.cost}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
