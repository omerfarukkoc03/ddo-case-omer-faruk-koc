// PatiKlinik yük testi (make load). Üç senaryo eşzamanlı çalışır:
//   booking   — T-3: 20 sanal kullanıcı aynı 12 slota sürekli rezervasyon dener (yoğun çakışma).
//               Bitişte her slotta en fazla 1 aktif randevu olduğu sayılır (double_bookings == 0 eşiği).
//   dose      — T-2: saniyede 50 doz hesabı (adım adım açıklamalı, decimal aritmetik).
//   vaccPlan  — T-1: saniyede 30 rastgele hayvanın aşı planı (motor her istekte yeniden hesaplar).
import http from "k6/http";
import { check } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

const BASE = __ENV.API_URL || "http://localhost:8080";
const PW = "Demo123!";

const bookingCreated = new Counter("booking_created");
const bookingConflict = new Counter("booking_conflict_409");
const doubleBookings = new Counter("double_bookings");
const unexpected = new Rate("unexpected_status");
const bookingLatency = new Trend("booking_latency", true);

export const options = {
  scenarios: {
    booking: { executor: "constant-vus", vus: 20, duration: "30s", exec: "booking" },
    dose: { executor: "constant-arrival-rate", rate: 50, timeUnit: "1s", duration: "30s", preAllocatedVUs: 20, exec: "dose" },
    vaccPlan: { executor: "constant-arrival-rate", rate: 30, timeUnit: "1s", duration: "30s", preAllocatedVUs: 20, exec: "vaccPlan" },
  },
  thresholds: {
    double_bookings: ["count==0"],
    unexpected_status: ["rate<0.01"],
    "http_req_duration{scenario:dose}": ["p(95)<300"],
    "http_req_duration{scenario:vaccPlan}": ["p(95)<300"],
    booking_latency: ["p(95)<500"],
  },
  summaryTrendStats: ["avg", "med", "p(90)", "p(95)", "p(99)", "max"],
};

function login(email) {
  const r = http.post(`${BASE}/api/auth/login`, JSON.stringify({ email, password: PW }), { headers: { "Content-Type": "application/json" } });
  if (r.status !== 200) throw new Error(`login ${email}: ${r.status}`);
  return r.json("access_token");
}

// Her çalıştırma geleceğe doğru farklı bir güne yazar; önceki çalıştırmalarla çakışmaz.
export function setup() {
  const dayOffset = 400 + (Math.floor(Date.now() / 60000) % 600);
  const d = new Date(Date.now() + dayOffset * 86400000);
  const day = d.toISOString().slice(0, 10);
  const slots = [];
  for (let vet = 1; vet <= 3; vet++) {
    for (const hhmm of ["10:00", "10:30", "11:00", "11:30"]) slots.push({ vet, start: `${day}T${hhmm}:00+03:00` });
  }
  return { vet: login("vet@patiklinik.test"), rec: login("resepsiyon@patiklinik.test"), day, slots };
}

const auth = (t, extra = {}) => ({ headers: Object.assign({ Authorization: `Bearer ${t}`, "Content-Type": "application/json" }, extra) });
const uuid = () => `${Date.now()}-${Math.random().toString(36).slice(2)}-${__VU}-${__ITER}`;

export function booking(data) {
  const s = data.slots[Math.floor(Math.random() * data.slots.length)];
  const body = { animal_id: 5 + Math.floor(Math.random() * 10000), vet_id: s.vet, room_id: s.vet + 3, kind: "exam", starts_at: s.start, duration_min: 30 };
  const r = http.post(`${BASE}/api/appointments`, JSON.stringify(body), auth(data.rec, { "Idempotency-Key": uuid() }));
  bookingLatency.add(r.timings.duration);
  if (r.status === 201) bookingCreated.add(1);
  else if (r.status === 409) bookingConflict.add(1);
  unexpected.add(r.status !== 201 && r.status !== 409);
}

export function dose(data) {
  const body = { animal_id: 4, drug_id: [1, 5, 6][Math.floor(Math.random() * 3)], doses_per_day: 1, duration_days: 3 };
  const r = http.post(`${BASE}/api/dosage/calculate`, JSON.stringify(body), auth(data.vet));
  unexpected.add(!check(r, { "dose 200": (x) => x.status === 200 && x.json("steps").length > 0 }));
}

export function vaccPlan(data) {
  const id = 5 + Math.floor(Math.random() * 14000);
  const r = http.get(`${BASE}/api/animals/${id}/vaccinations`, auth(data.vet));
  unexpected.add(!check(r, { "plan 200": (x) => x.status === 200 }));
}

export function teardown(data) {
  const r = http.get(`${BASE}/api/appointments?date=${data.day}`, auth(data.rec));
  const items = r.json("items") || [];
  for (const s of data.slots) {
    const start = Date.parse(s.start);
    const active = items.filter((a) => a.vet_id === s.vet && Date.parse(a.starts_at) === start && (a.status === "scheduled" || a.status === "checked_in"));
    if (active.length > 1) doubleBookings.add(active.length - 1);
  }
  console.log(`slot gunu ${data.day}: ${items.length} randevu, ${data.slots.length} slot`);
}
