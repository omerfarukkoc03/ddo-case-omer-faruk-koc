import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, dateTime, speciesLabel, time } from "../api";
import { useHasRole, useUser } from "../auth";
import type { Appointment, Resource } from "../types";
import { Badge, Card, ErrorBox, Field, Page, Query, SuccessBox, Toggle, label } from "../ui";

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

  return (
    <Page
      title={isOwner ? "Randevularım" : "Randevu takvimi"}
      actions={
        !isOwner && (
          <div className="row">
            <button className="btn btn-sm" onClick={() => shift(-1)}>‹ Önceki</button>
            <input type="date" value={day} onChange={(e) => setDay(e.target.value)} style={{ width: 160 }} />
            <button className="btn btn-sm" onClick={() => shift(1)}>Sonraki ›</button>
            <button className="btn btn-sm" onClick={() => setDay(today())}>Bugün</button>
          </div>
        )
      }
    >
      <div className="stack">
        {(canEmergency || canCreate) && (
          <div className="grid grid-2">
            {canEmergency && <EmergencyForm />}
            {canCreate && <Toggle label="Yeni randevu"><AppointmentForm day={day} /></Toggle>}
          </div>
        )}
        <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Bu gün için randevu yok.">
          {(d) => (isOwner ? <OwnerList items={d.items} /> : <DayBoard items={d.items} />)}
        </Query>
      </div>
    </Page>
  );
}

function OwnerList({ items }: { items: Appointment[] }) {
  return (
    <Card>
      <div className="timeline">
        {[...items].reverse().map((a) => (
          <div key={a.id} className={`appt ${a.priority === "emergency" ? "emergency" : ""}`}>
            <strong>{dateTime(a.starts_at)}</strong>
            <span>{a.animal_name} · {label(a.kind)} · {a.vet_name}</span>
            <Badge value={a.status} />
          </div>
        ))}
      </div>
    </Card>
  );
}

function DayBoard({ items }: { items: Appointment[] }) {
  const qc = useQueryClient();
  const canStatus = useHasRole("vet", "technician", "reception");
  const m = useMutation({
    mutationFn: ({ id, status }: { id: number; status: string }) => api(`/api/appointments/${id}/status`, { method: "POST", body: { status } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appointments"] }),
  });
  const byVet = new Map<string, Appointment[]>();
  items.forEach((a) => byVet.set(a.vet_name, [...(byVet.get(a.vet_name) ?? []), a]));
  return (
    <div className="stack">
      <ErrorBox error={m.error} />
      <div className="grid grid-2">
        {[...byVet.entries()].map(([vet, list]) => (
          <Card key={vet} title={`${vet} · ${list.filter((a) => a.status !== "cancelled").length} randevu`}>
            <div className="timeline">
              {list.map((a) => (
                <div key={a.id} className={`appt ${a.priority === "emergency" ? "emergency" : ""}`}>
                  <div>
                    <strong>{time(a.starts_at)}–{time(a.ends_at)}</strong>
                    <div className="small muted">{a.room_name}</div>
                  </div>
                  <div>
                    <Link to={`/animals/${a.animal_id}`}>{a.animal_name}</Link> <span className="small muted">{speciesLabel[a.species]}</span>
                    <div className="small">
                      <Badge value={a.priority === "emergency" ? "emergency" : a.kind} />{" "}
                      {a.reschedule_count > 0 && a.original_starts_at && (
                        <span className="muted">kaydırıldı (ilk: {time(a.original_starts_at)})</span>
                      )}
                    </div>
                  </div>
                  <div className="row">
                    <Badge value={a.status} />
                    {canStatus && a.status === "scheduled" && (
                      <>
                        <button className="btn btn-sm" onClick={() => m.mutate({ id: a.id, status: "checked_in" })}>Geldi</button>
                        <button className="btn btn-sm" onClick={() => m.mutate({ id: a.id, status: "cancelled" })}>İptal</button>
                      </>
                    )}
                    {canStatus && a.status === "checked_in" && (
                      <button className="btn btn-sm" onClick={() => m.mutate({ id: a.id, status: "completed" })}>Tamamla</button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </Card>
        ))}
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
  const m = useMutation({
    mutationFn: () =>
      api<Appointment>("/api/appointments", {
        method: "POST",
        body: {
          animal_id: Number(f.animal_id), vet_id: Number(f.vet_id), room_id: Number(f.room_id), kind: f.kind,
          starts_at: new Date(`${day}T${f.time}:00`).toISOString(), duration_min: Number(f.duration_min),
        },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appointments"] }),
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const vets = res.data?.items.filter((r) => r.kind === "vet") ?? [];
  const rooms = res.data?.items.filter((r) => r.kind === (f.kind === "surgery" ? "surgery_room" : "exam_room")) ?? [];
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
        <Field label="Oda"><select value={f.room_id} onChange={set("room_id")}>{rooms.map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}</select></Field>
        <Field label={`Saat (${day})`}><input type="time" step={300} value={f.time} onChange={set("time")} /></Field>
        <Field label="Süre (dk)"><input type="number" step={5} value={f.duration_min} onChange={set("duration_min")} /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Randevu #{m.data.id} oluşturuldu.</SuccessBox>}
      <button className="btn btn-primary" disabled={m.isPending}>Randevu oluştur</button>
    </form>
  );
}

interface EmergencyResult {
  emergency: Appointment;
  moves: { appointment_id: number; from_start: string; to_start: string; from_vet_id: number; to_vet_id: number; cost: number }[];
  cancelled_for_manual_rebooking: number[];
  disruption_cost: number;
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
    <Card title="🚨 Acil vaka kabulü">
      <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <div className="form-row">
          <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={set("animal_id")} required /></Field>
          <Field label="Süre (dk)"><input type="number" value={f.duration_min} onChange={set("duration_min")} /></Field>
          <Field label="Oda türü">
            <select value={f.room_kind} onChange={set("room_kind")}>
              <option value="exam_room">Muayene odası</option><option value="surgery_room">Ameliyathane</option>
            </select>
          </Field>
        </div>
        <Field label="Şikâyet"><input value={f.reason} onChange={set("reason")} placeholder="ör. trafik kazası" required /></Field>
        <ErrorBox error={m.error} />
        <button className="btn btn-danger" disabled={m.isPending}>Acil vakayı şimdi başlat</button>
      </form>
      {m.data && (
        <div className="stack" style={{ marginTop: 12 }}>
          <SuccessBox>{m.data.explanation}</SuccessBox>
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
