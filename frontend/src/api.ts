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

// Sunucu geliştiriciye yönelik İngilizce mesaj ve makinece okunur bir hata kodu döndürür; kullanıcıya gösterilen
// metin koddan üretilir. Bilinmeyen kodlarda sunucu mesajı gösterilir.
const errorMessages: Record<string, string> = {
  unauthorized: "Oturumunuz sona erdi, lütfen yeniden giriş yapın.",
  invalid_credentials: "E-posta ya da parola hatalı.",
  invalid_refresh_token: "Oturumunuz sona erdi, lütfen yeniden giriş yapın.",
  forbidden: "Bu sayfayı ya da işlemi görüntüleme yetkiniz yok.",
  not_found: "Kayıt bulunamadı.",
  validation_failed: "Girilen bilgiler geçersiz.",
  bad_request: "İstek okunamadı.",
  duplicate: "Aynı bilgiyle kayıtlı başka bir kayıt var (ör. aynı mikroçip ya da aynı hayvanın ikinci aktif yatışı).",
  resource_conflict: "Seçilen veteriner ya da oda bu saatte dolu.",
  concurrent_update: "Kayıt aynı anda başka biri tarafından değiştirildi; lütfen yeniden deneyin.",
  constraint_violation: "Değer bir iş kuralını ihlal ediyor.",
  invalid_reference: "Belirtilen kayıt (hasta, sahip, veteriner…) bulunamadı.",
  invalid_resources: "Veteriner ya da oda seçimi geçersiz (ameliyat için ameliyathane seçilmeli).",
  invalid_status_transition: "Randevu bu durumdan seçilen duruma geçemez.",
  no_emergency_capacity: "Önümüzdeki 4 saatte acil vaka için uygun veteriner/oda yok.",
  contraindicated: "Kontrendikasyon: bu ilaç bu tür/ırkta kullanılamaz (gerekçeli onayla da yazılamaz).",
  species_not_supported: "Bu ilacın bu tür için onaylı dozu yok.",
  dose_below_range: "Doz, etkili en düşük dozun altında.",
  max_dose_exceeded: "Doz azami sınırı aşıyor; yazmak için en az 10 karakterlik bir gerekçe girin.",
  dose_hard_limit: "Doz, azami dozun 2 katını aşıyor; hiçbir koşulda yazılamaz.",
  no_practical_tablet_dose: "Bu kiloda güvenli aralıkta kalan ¼ tablet dozu yok; sıvı form kullanın.",
  invalid_dilution: "Seyreltme konsantrasyonu pozitif ve stok konsantrasyonundan düşük olmalı.",
  weight_required: "Hayvanın kayıtlı kilosu yok; önce kilo girin.",
  insufficient_stock: "Yetersiz stok (süresi geçmemiş partilerde yeterli miktar yok).",
  prescription_not_active: "Reçete zaten teslim edilmiş ya da iptal edilmiş.",
  overpayment: "Ödeme, faturanın kalan tutarını aşıyor.",
  invoice_not_open: "Fatura açık değil (ödenmiş ya da iptal).",
  not_an_owner: "Seçilen kişi bu hayvanın sahiplerinden biri değil.",
  no_owner: "Hayvanın faturalanacak sahibi yok.",
  package_exhausted: "Bu hayvanın kalan paket seansı yok.",
  package_species_mismatch: "Paket bu hayvanın türü için değil.",
  dose_not_planned: "Bu doz zaten uygulanmış.",
  cage_invalid_transition: "Kafes için geçersiz durum geçişi (boş → dolu → temizlik → hazır).",
  cage_has_patient: "Kafeste hasta var; önce taburcu edin.",
  cage_not_available: "Kafes boş ya da hazır değil.",
  no_cage_available: "Boş ya da hazır kafes yok.",
  use_admission: "Kafes yalnız hasta yatışıyla dolu durumuna geçer.",
  already_discharged: "Hasta zaten taburcu edilmiş.",
  already_merged: "Kayıtlardan biri zaten başka bir kayıtla birleştirilmiş.",
  already_undone: "Bu birleştirme zaten geri alınmış.",
  merge_undo_conflict: "Birleştirmeden sonra ilgili kayıtlar değiştiği için geri alınamıyor.",
  species_mismatch: "Farklı türdeki hayvanlar birleştirilemez.",
  microchip_mismatch: "İki kaydın mikroçipi farklı; bunlar mükerrer değil.",
  both_hospitalized: "İki kayıt da yatan hasta; önce birini taburcu edin.",
  idempotency_in_progress: "Aynı istek hâlâ işleniyor; biraz sonra yeniden deneyin.",
  idempotency_key_reused: "Bu istek anahtarı farklı bir istekle kullanılmış.",
  appointment_mismatch: "Randevu başka bir hayvana ait.",
  prescription_immutable: "Reçetenin hesap alanları yazıldıktan sonra değiştirilemez.",
  audit_append_only: "Denetim kaydı değiştirilemez.",
  method_not_allowed: "Bu işlem desteklenmiyor.",
  timeout: "İstek zaman aşımına uğradı; yeniden deneyin.",
  internal: "Beklenmeyen bir sunucu hatası oluştu.",
};

export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    let text = errorMessages[e.code] ?? e.message;
    const fields = (e.details?.fields ?? null) as Record<string, string> | null;
    if (fields) text += " Alanlar: " + Object.keys(fields).join(", ") + ".";
    // Kontrendikasyonun gerekçesi sunucu mesajının sonundadır ("…: gerekçe").
    if (e.code === "contraindicated" && e.message.includes(": ")) text += " Gerekçe: " + e.message.split(": ").pop() + ".";
    if (e.code === "overpayment" && typeof e.details?.due_kurus === "number") text += ` Kalan: ${money(e.details.due_kurus)}.`;
    return text;
  }
  if (e instanceof TypeError) return "Sunucuya ulaşılamadı; bağlantınızı kontrol edin.";
  return e instanceof Error ? e.message : String(e);
}

// Tekrar denemekle değişmeyecek hatalar (yetki, bulunamadı, doğrulama).
export const isPermanent = (e: unknown) => e instanceof ApiError && [401, 403, 404, 422].includes(e.status);
