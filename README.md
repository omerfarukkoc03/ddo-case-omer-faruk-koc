# PatiKlinik: Veteriner Klinik Hasta, Aşı ve Randevu Sistemi

**Geliştiren:** Ömer Faruk Koç · BANÜ Dijital Dönüşüm Ofisi (DDO) yazılım ekibi alım projesi

Bir veteriner kliniğinin hayvan kaydı, randevu ve muayene, aşı takvimi, ilaç dozu ve stok, yatan hasta ve kafes,
fatura ve tahsilat süreçlerini yöneten sistem. Backend Go, veritabanı PostgreSQL 16, arayüz React. Tamamı tek komutla
Docker Compose ile ayağa kalkar.

[![CI](https://github.com/omerfarukkoc03/ddo-case-omer-faruk-koc/actions/workflows/ci.yml/badge.svg)](https://github.com/omerfarukkoc03/ddo-case-omer-faruk-koc/actions/workflows/ci.yml)

| | |
|---|---|
| Hızlı doğrulama | `make verify` → 15/15 PASS, ~5 sn ([TESTING.md](TESTING.md)) |
| Testler | 60 Go testi (40 birim + 20 gerçek PostgreSQL ile entegrasyon) ve 17 Playwright arayüz testi. Çekirdek motorlarda kapsam %93–95 |
| Mimari kararlar | [DECISIONS.md](DECISIONS.md) (7 karar kaydı) |
| API dokümantasyonu | http://localhost:8080/docs (OpenAPI 3: `backend/internal/api/openapi.yaml`), [requests.http](requests.http) |
| Yük testi | `make load` → [load/RESULTS.md](load/RESULTS.md) |

---

## Kurulum

Gereken tek şey Docker (Compose v2). Makefile kısayolları için `make` de gerekir; yoksa parantez içindeki komutlar
kullanılabilir.

```bash
git clone https://github.com/omerfarukkoc03/ddo-case-omer-faruk-koc.git && cd ddo-case-omer-faruk-koc
cp .env.example .env
docker compose up --build
```

| Servis | Adres |
|---|---|
| Arayüz | http://localhost:3000 |
| API | http://localhost:8080 (sağlık: `/health`, Swagger: `/docs`) |
| Veritabanı | Yalnız compose ağı içinde; host'a açılmaz |

Başlangıç sırası `healthcheck` ve `depends_on: service_healthy` ile yönetilir: önce `db`, sonra `api` (migrasyonları
uygular), sonra `simulator` (seed eder, ardından canlı olay üretir) ve `frontend`. Migrasyon ve seed elle adım
gerektirmez. Seed ~20 sn sürer, sıcak önbellekle tüm sistem ~1 dakikada hazır olur.

**Soğuk makine ölçümü.** Repo GitHub'dan sıfırdan klonlandı. Docker derleme önbelleği, proje imajları, veritabanı
hacmi ve tüm temel imajlar (golang, node, nginx, postgres, distroless) önceden silindi. Ortam Windows 11 + Docker
Desktop (WSL2), ev internet bağlantısı. Sonuç:
- `docker compose up --build` 219 sn'de döndü.
- Tüm servisler (seed dahil) **285 sn**'de `healthy` oldu.
- Ardından `make verify` 15/15 PASS.

Sürenin en büyük kalemi temel imajların indirilmesiydi (golang tek başına ~50 sn); Linux ya da daha hızlı bir
bağlantıda süre belirgin şekilde kısalır.

| Komut | Ne yapar |
|---|---|
| `make verify` (`docker compose --profile verify run --rm verify`) | Her özelliği ve zorlu görevi uçtan uca dener, PASS/FAIL tablosu basar |
| `make reset-demo` | Veriyi ilk seed haline döndürür |
| `make load` (`docker compose --profile load run --rm k6`) | k6 yük testi |
| `make test` / `make lint` | Go testleri (Docker gerekir) / golangci-lint |

**Demo hesapları.** Parola hepsinde `Demo123!`. Giriş ekranında tek tıkla giriş düğmeleri de vardır.

| Rol | E-posta |
|---|---|
| Veteriner | `vet@patiklinik.test` (ayrıca `vet2@`, `vet3@`) |
| Teknisyen | `tekniker@patiklinik.test` |
| Resepsiyon | `resepsiyon@patiklinik.test` |
| Eczane / depo | `eczane@patiklinik.test` |
| Hayvan sahibi | `sahip@patiklinik.test` (ortak sahip: `sahip2@patiklinik.test`) |

Demo hayvanların ve senaryoların listesi [TESTING.md](TESTING.md#a-demo-hesapları) dosyasındadır.

**Yeni hayvan sahibinin hesabı.** Sahipler kendi kendine kayıt olmaz: kimlik klinikte doğrulanır.
1. Resepsiyon, sahibi ve hayvanını kaydeder.
2. Hasta detayındaki **Portal daveti** düğmesiyle 8 karakterlik, tek kullanımlık, 48 saat geçerli bir kod üretir.
3. Sahip, giriş ekranındaki **Hesabımı etkinleştir** bağlantısından e-posta + kod + kendi parolasıyla hesabını açar
   ve yalnız kendi hayvanlarını görür.

Gerekçe: [ADR-007](DECISIONS.md#adr-007--hayvan-sahibi-portal-hesabı-serbest-kayıt-yerine-klinikte-doğrulanan-davet-kodu).

---

## Mimari özeti

```
 Tarayıcı ──► frontend  (nginx-unprivileged :3000, React SPA) ──/api──┐
                                                                      ▼
 simulator ─(seed: COPY)─────────────────────────────────►  db   ◄── api  (Go :8080)
     └─────(canlı olaylar: HTTP, demo kullanıcılar)──────────────────►┘    · chi router, pgx, goose
 verify  (profile: verify) ──HTTP──► api + frontend                         · JWT + RBAC, idempotency,
 k6      (profile: load)   ──HTTP──► api                                       denetim, keyset sayfalama
```

```
backend/
  cmd/api           HTTP sunucusu (graceful shutdown, hatırlatma job'ı ticker'ı)
  cmd/simulator     seed + canlı olay üretici; `reset` alt komutu
  cmd/verify        make verify aracı
  migrations/       goose SQL migrasyonları (ikiliye gömülü, açılışta otomatik)
  internal/
    vaccine/        T-1 aşı motoru     ─┐
    dosage/         T-2 doz motoru      │ saf Go: veritabanı yok, tablo testli
    schedule/       T-3 planlama        │
    cagefsm/        kafes durum makinesi┘
    vaccination/    aşı motoru ↔ veritabanı: plan senkronizasyonu, idempotent hatırlatma job'ı
    api/            uç noktalar, RBAC, birleştirme, OpenAPI, entegrasyon testleri
    auth/ audit/ httpx/ platform/ config/ seed/ apiclient/
frontend/           React + Vite + TypeScript + TanStack Query
load/               k6 betiği ve sonuç raporu
```

**Tasarım ilkesi: zor kurallar iki katmanda korunur.** İş kuralları saf ve test edilebilir Go paketlerinde yaşar.
Veri bütünlüğünü tehdit eden kurallar ayrıca veritabanında da garanti edilir. Uygulama kullanıcıya anlamlı bir hata
döner (`409 cage_invalid_transition`); kod yolu ne olursa olsun veritabanı son savunma hattıdır:

| Kural | Uygulama | Veritabanı |
|---|---|---|
| Çifte rezervasyon yok | Kaynak kilidi, anlamlı 409 | `EXCLUDE USING gist` (veteriner ve oda) |
| Kafes durum makinesi | `cagefsm` + koşullu `UPDATE … WHERE state=$eski` | `cage_state_guard` tetikleyicisi |
| Fazla tahsilat yok | Fatura `FOR UPDATE`, 422 `overpayment` | `CHECK (paid_kurus <= total_kurus)` |
| Stok eksiye düşmez | Partiler `FOR UPDATE`, koşullu düşüm | `CHECK (quantity >= 0)` |
| Paket seansı eksiye düşmez | Koşullu `UPDATE … WHERE sessions_left > 0` | `CHECK`, doz başına `UNIQUE` |
| Reçete hesabı değişmez | Kopya alanlar | `prescription_immutable` tetikleyicisi |
| Denetim kaydı silinemez | Yalnız `INSERT` | `UPDATE`/`DELETE`/`TRUNCATE` tetikleyicisi |
| Hayvan başına tek fatura sorumlusu | Açık değiştirme ucu | Kısmi `UNIQUE (animal_id) WHERE is_billing` |
| Mikroçip tekil | 409 `duplicate` | Kısmi `UNIQUE (microchip) WHERE aktif` |

### Teknoloji seçimleri ve gerekçeler

| Katman | Seçim | Gerekçe |
|---|---|---|
| HTTP | `chi` | Standart `net/http` ile birebir uyumlu, ara katman zinciri sade, rota başına rol kısıtını okunur kılıyor |
| Veritabanı sürücüsü | `pgx/v5` + elle yazılmış SQL | EXCLUDE, `FOR NO KEY UPDATE`, `SKIP LOCKED`, `COPY` gibi PostgreSQL özelliklerini doğrudan kullanmak için. ORM bunları gizlerdi. `sqlc` süre kısıtı nedeniyle eklenmedi |
| Migrasyon | `goose` (SQL dosyaları ikiliye gömülü) | Ek konteyner ya da elle adım yok; API açılışta uygular |
| Sayılar | `shopspring/decimal` + `numeric`; para `bigint` kuruş | Float yok |
| Kimlik | bcrypt + JWT (HS256, 15 dk) + dönen refresh token | [ADR-005](DECISIONS.md#adr-005--kimlik-doğrulama-kısa-ömürlü-jwt-ve-veritabanında-tutulan-dönen-refresh-token) |
| Konteyner | Çok aşamalı derleme, `distroless/static:nonroot`, salt okunur kök dosya sistemi | API imajı 24 MB, kabuk yok |

**Frontend: React + Vite + TypeScript + TanStack Query.**
- TanStack Query, her ekranda yükleniyor, hata (yeniden dene) ve boş durumlarını tek bir `<Query>` bileşeniyle
  tutarlı vermeyi sağladı. 4xx hatalar yeniden denenmez, ağ ve 5xx hataları denenir. Canlı panonun 3 sn'de bir
  yenilenmesi de buradan geliyor.
- TypeScript, API sözleşmesinin arayüzde de tip kontrolünden geçmesini sağlıyor.
- UI kütüphanesi kullanmadım. Sade CSS değişkenleriyle açık ve koyu tema, mobil yerleşim var. Paket boyutu gzip'li
  81 KB.
- HTMX düşündüm, ama takvim, adım adım doz hesabı ve kilo grafiği gibi etkileşimli ekranlar için istemci tarafı
  durum yönetimi daha uygun geldi.
- Arayüz nginx ile aynı kökenden servis edilir ve `/api` isteklerini API'ye iletir; CORS gerekmez.

---

## Zorlu çekirdek görevler

### T-1 · Aşı takvimi motoru: `internal/vaccine`, `internal/vaccination`

- Protokoller veritabanında, tür ve ırk bazında tanımlı. Irka özel protokol genel olanı ezer: Rottweiler ve Doberman'da
  parvo serisi 20. haftaya uzar. Yavru serisi 6-8-12-16. hafta, ardından yıllık rapel (miksomatozis 6 ayda bir). Kuduz
  ve RHDV tek dozluk serilerdir.
- **Gecikme:** Vadesi geçmiş doz bugüne çekilir. Sonraki doz `max(bir sonraki hedef hafta, bugün + asgari aralık)` olur.
  Asgari aralıktan erken ya da asgari yaştan önce yapılmış doz **sayılmaz**; plan bunu gerekçesiyle gösterir.
  16. haftadan sonra başlayan hayvan, 21 gün arayla 2 dozluk "geç başlangıç" serisi alır.
- **Tahmini yaş:** Doğum tarihi bilinmiyorsa kayıtta tahmini yaş (ay) alınır. Doğum tarihi buradan türetilir ve
  `birth_date_estimated` olarak işaretlenir; plan da "tahmini yaşa göre" etiketi taşır.
- **Plan senkronizasyonu:** Hayvan kaydında, doz uygulamada ve birleştirmede plan motorla yeniden hesaplanıp
  veritabanıyla eşitlenir. Hayvan satırı kilitlenir; kilit sırası her yerde hayvan → dozlar → paket.
- **İdempotent hatırlatma job'ı:** `UNIQUE (dose_id, kind, due_date)` + `ON CONFLICT DO NOTHING` + tek örnek için
  `pg_try_advisory_xact_lock`. Kaç kez, kaç örnekte çalışırsa çalışsın aynı hatırlatma tekrar oluşmaz. Doz yeniden
  planlanırsa yeni vade için yeni hatırlatma oluşur. Hatırlatmalar e-posta şablonu metniyle listelenir (bonus).

### T-2 · Doz hesabı ve güvenlik: `internal/dosage`

Ayrıntı: [ADR-004](DECISIONS.md#adr-004--doz-hesabı-decimal-aritmetik-kesin-ve-gerekçeli-kural-ayrımı-değiştirilemez-reçete-kopyası).
- Hesap decimal ile yapılır: mg = kg × mg/kg, mL = mg ÷ konsantrasyon, seyreltme C1·V1 = C2·V2. Her adım
  `steps[]` olarak açıklanır.
- Kurallar üç seviyeli: kontrendikasyon kesin engel; azami aşım gerekçeyle yazılır ve denetim kaydına düşer; 2×
  mutlak tavan aşılamaz.
- Tablet yuvarlaması güvenli aralıkta tutulur.
- Reçete kiloyu ve hesabı kopyalar, tetikleyici bu alanların değişmesini engeller. Kilo değiştiğinde reçete aynı kalır,
  arayüz farkı bir notla gösterir.
- İlaç teslimi FEFO sırasıyla yapılır (son kullanma tarihi en yakın parti önce). Partiler kilitlenir, süresi geçmiş
  parti kullanılmaz.

### T-3 · Kaynak planlamalı randevu ve acil vaka: `internal/schedule`, `internal/cagefsm`

Ayrıntı: [ADR-001](DECISIONS.md#adr-001--randevu-çakışmasını-veritabanı-exclude-kısıtıyla-garanti-etmek) ve
[ADR-002](DECISIONS.md#adr-002--acil-vaka-yeniden-planlaması-açık-maliyet-fonksiyonu-küçük-kümede-tam-arama).
- Veteriner, muayene odası ve ameliyathane `EXCLUDE` kısıtıyla korunur.
- Acil vaka şimdi başlar. Çakışan randevular açık bir maliyet fonksiyonuyla en az bozulacak şekilde boş slotlara
  taşınır; etkilenen 5 veya daha az randevuda tam arama yapılır. Tüm çiftler doluysa acil vaka ilk boşalan çifte
  ertelenir. Taşınan her randevu denetim kaydına yazılır, takvimde ilk saatiyle görünür.
- Kafes durum makinesi `boş → dolu → temizlik → hazır → dolu`. Hem Go'da hem tetikleyicide uygulanır. Kafes
  verilmeden yapılan yatışta uygun kafes `SKIP LOCKED` ile seçilir.

### T-4 · Çoklu sahiplik ve birleştirme: `internal/api/merge.go`, `billing.go`

Ayrıntı: [ADR-003](DECISIONS.md#adr-003--geri-alınabilir-birleştirme-silme-yok-taşınan-satır-kimlikleriyle-anlık-görüntü).
- **Fatura kimin adına kesilir:** Faturada açıkça seçilen sahip; yoksa fatura sorumlusu; o da yoksa ilk kayıtlı sahip.
  Seçilen kural faturada `billing_rule` olarak saklanır.
- **Birleştirme:** Silme yapılmaz, taşınan satır kimlikleri anlık görüntüye yazılır. Geri alma yalnız değişmemiş
  satırları döndürür; kısmi geri alma yapılmaz. Hayvan birleştirmede aşı geçmişleri birleşir ve plan yeniden
  hesaplanır.
- **Paket seans düşümü:** Koşullu `UPDATE` ve doz başına `UNIQUE` kullanılır. Seans kalmadıysa aşı uygulaması da
  geri alınır.

### Ortak teknik gereksinimler

| Gereksinim | Nerede |
|---|---|
| bcrypt, JWT access + refresh, her uç noktada RBAC, 401/403 | `internal/auth`, `internal/api/router.go` (56 uç nokta, rol listesi tek dosyada) |
| Migrasyon, FK/UNIQUE/CHECK/EXCLUDE, para kuruş, float yok | `backend/migrations/00001_core.sql` |
| Eşzamanlılık: `FOR UPDATE`, `FOR NO KEY UPDATE`, `SKIP LOCKED`, koşullu `UPDATE`, advisory lock; yarış testleri | `internal/api/*`, `integration_test.go` |
| `Idempotency-Key` (aynı istek → aynı yanıt, farklı gövde → 422) | `internal/httpx/idempotency.go` |
| Append-only denetim kaydı (kim, ne zaman, neyi, eski/yeni değer, request id) | `internal/audit`, `audit_log` tetikleyicisi |
| Tutarlı hata biçimi, sunucu tarafı doğrulama, keyset sayfalama, anlamlı durum kodları | `internal/httpx` |
| `context` zaman aşımı, graceful shutdown, JSON log (parola/token yok), yapılandırma ortamdan | `cmd/api/main.go`, `internal/httpx/middleware.go`, `internal/config` |

---

## Veritabanı dizinleri ve `EXPLAIN (ANALYZE)`

Ölçümler seed edilmiş veritabanında yapıldı: 15.012 hayvan, 10.313 sahip, 220.563 aşı dozu. "Dizinsiz" ölçümde dizin
transaction içinde silinip ölçüm sonrası `ROLLBACK` ile geri alındı.

**1) Hatırlatma job'ı: `vaccine_doses_due_idx ON vaccine_doses (due_date) WHERE status = 'planned'`**

```
EXPLAIN (ANALYZE, BUFFERS) SELECT d.id FROM vaccine_doses d
WHERE d.status='planned' AND d.due_date BETWEEN current_date - 30 AND current_date + 7;

-- dizinli
Bitmap Heap Scan on vaccine_doses d (actual time=3.029..9.922 rows=12207 loops=1)
  ->  Bitmap Index Scan on vaccine_doses_due_idx (actual time=1.962..1.963 rows=12210 loops=1)
        Buffers: shared hit=12
Execution Time: 11.061 ms

-- dizinsiz
Gather (actual time=1.656..34.285 rows=12207 loops=1)
  ->  Parallel Seq Scan on vaccine_doses d (actual time=0.054..20.663 rows=4069 loops=3)
        Rows Removed by Filter: 69452
Execution Time: 35.269 ms
```

**2) Mikroçip araması: `animals_microchip_uq ON animals (microchip) WHERE microchip IS NOT NULL AND merged_into IS NULL`**

```
-- dizinli
Index Scan using animals_microchip_uq on animals (actual time=0.074..0.076 rows=1 loops=1)
  Buffers: shared hit=3
Execution Time: 0.110 ms

-- dizinsiz
Seq Scan on animals (actual time=0.020..3.429 rows=1 loops=1)
  Rows Removed by Filter: 15011
  Buffers: shared hit=216
Execution Time: 3.458 ms
```

**3) Çakışma kontrolü: `appointments_*_no_overlap` (EXCLUDE kısıtının gist dizini)**

```
Bitmap Heap Scan on appointments (actual time=0.139..0.159 rows=2 loops=1)
  ->  Bitmap Index Scan on appointments_room_no_overlap (actual time=0.123..0.124 rows=4 loops=1)
        Index Cond: (tstzrange(starts_at, ends_at, '[)') && tstzrange(now(), now() + '00:30:00', '[)'))
Execution Time: 2.047 ms
```

Diğer dizinler:
- keyset sayfalama için `animals_keyset_idx` ve `owners_keyset_idx` (`WHERE merged_into IS NULL`)
- günlük takvim için `appointments_day_idx (starts_at, id)`
- FEFO için `stock_batches_fefo_idx (drug_id, expires_on) WHERE quantity > 0`
- denetim sorguları için `audit_log_entity_idx`
- ad öneki araması için `text_pattern_ops` dizinleri

**Dürüst not:** `owners_phone_idx`, canlı panonun 3 sn'de bir çalıştırdığı mükerrer sahip sayımında **kullanılmıyor**.
Planlayıcı tüm tabloyu eşleştirdiği için hash join'i seçiyor; dizinle de dizinsiz de ~15 ms. Dizin telefonla tekil
arama için faydalı. Mükerrer sayımı ise ölçek büyüdükçe önbelleğe alınmalı (bkz. bilinen sınırlamalar).

---

## Test, CI ve yük testi

- **Birim testler (40):** aşı motoru 13, doz 12, planlama 10, kafes FSM 2, seed determinizmi 2, OpenAPI–rota kapsamı 1.
- **Entegrasyon testleri (20):** testcontainers ile açılan gerçek PostgreSQL 16 üzerinde, HTTP'den veritabanına.
  Yarış durumları gerçek eşzamanlı isteklerle kanıtlanıyor: 20 rezervasyondan 1'i, 10 ödemeden 1'i kabul ediliyor;
  sınırlı stokta tam hesaplanan sayıda teslim yapılıyor; 20 koşullu düşümden tam 5'i başarılı oluyor; 5 eşzamanlı job
  çalıştırması yinelenen hatırlatma üretmiyor.
- **Uç nokta taraması (2 entegrasyon testi):**
  - *Rol matrisi:* Router'daki her uç nokta 5 rolle ve token'sız çağrılır (255 birleşim). Sonuç, OpenAPI'deki
    `Roller:` satırıyla birebir karşılaştırılır. Bu, hem her uç noktada sunucu tarafı yetki kontrolü olduğunu hem de
    belgenin doğru olduğunu kanıtlar. Kasıtlı yanlış bir rol yazılarak test edildi (mutasyon testi).
  - *Bozuk girdi:* Her uç noktaya bozuk JSON, yanlış tipler, 5.000 karakterlik metinler, sınır dışı ve var olmayan
    kimlikler gönderilir. Hiçbiri 5xx üretmez, tüm hatalar ortak biçimdedir.
- **Arayüz testleri (17, Playwright, gerçek Chromium):**
  - 5 rolün menüdeki tüm sayfaları
  - yetkisiz sayfa yönlendirmesi
  - doz hesabı, acil vaka, kafes, fatura, portal daveti ve etkinleştirme, birleştirme ve geri alma akışları
  - mobil genişlikte yatay taşma kontrolü

  Her testte tarayıcı konsol hataları ve 5xx yanıtlar otomatik olarak başarısızlık sayılır (`make e2e`).
  Bu testler yazılırken iki gerçek arayüz hatası bulundu: çıkıştan sonra sonraki kullanıcının öncekinin sayfasına
  düşmesi ve ameliyat seçilince oda listesinin değişip seçili odanın eski kalması.
- **GitHub Actions:** `golangci-lint` (errcheck, staticcheck, gosec, errorlint, bodyclose…), `go test -race` (birim +
  entegrasyon), frontend tip kontrolü ve derlemesi, tüm compose imajlarının derlenmesi. Ayrıca tam sistemin `docker compose up` ile ayağa kaldırılıp `make verify` ve Playwright testlerinin çalıştırılması; hata durumunda ekran görüntüleri yapıt olarak saklanır.
- **`make verify`:** 15 kimlik, ~5 sn ([TESTING.md](TESTING.md)).
- **`make load`:** Rezervasyon çekişmesi, doz hesabı ve aşı planı eşzamanlı çalışır. İlk çalıştırma EXCLUDE
  eklemelerinde deadlock'ları ortaya çıkardı. Düzeltme sonrası:
  - çifte rezervasyon: 0
  - rezervasyon p99: 12,6 sn → **425 ms**
  - doz hesabı p95: 847 → **54 ms**
  - aşı planı p95: 691 → **91 ms**

  Ayrıntı: [load/RESULTS.md](load/RESULTS.md).

---

## Bilinen sınırlamalar

- **Idempotency kaydı ile işin kendisi ayrı transaction'larda.** Ayrıntı aşağıda, Doğrulama Soruları #2'de.
- **Acil vaka planlaması:** 5'ten fazla randevu etkilenirse açgözlü yerleştirme kullanılır, sonuç optimal olmayabilir.
  7 gün içinde yer bulunamayan randevu otomatik iptal edilip elle yeniden planlanmak üzere raporlanır. Ceza katsayıları
  sabit. Mesai saatleri 09:00–19:00 ve her gün için aynı (tatil ve vardiya modeli yok).
- **Aşı motoru:** Vadesi geçmiş planlı doz, hayvana bir işlem yapılana kadar eski tarihinde kalır; job bu sürede
  "gecikmiş" hatırlatması üretir. Çok uzun süre aşı yapılmamışsa seriyi baştan başlatma kuralı yok. Gerçek protokollerde
  bu bazen gerekir.
- **Birleştirme:** Birleştirme zinciri ters sırada geri alınmalı. Taşınan satır sonradan değiştiyse geri alma bilerek
  reddedilir; kısmi geri alma ya da birleştirme önerisi yok.
- **Kimlik:** Çıkıştan sonra access token en fazla 15 dk daha geçerli kalır (JWT'nin bilinen ödünleşimi). Giriş ve
  davet kodu uçlarında hız sınırlaması (rate limiting) yok. Davet kodu uzayı ~10¹² ve kod 48 saatte geçersizleşiyor,
  bu yüzden kaba kuvvet pratik değil; yine de üretimde IP ve hesap başına deneme sınırı eklenmeli.
- **Hatırlatmalar:** Yalnız oluşturulur ve şablon metniyle listelenir; gerçek e-posta gönderimi yok.
- **Ölçek:** Canlı panodaki sayımlar her istekte `count(*)` çalıştırıyor. 15 bin kayıtta milisaniyeler sürüyor,
  milyonlarca kayıtta önbellek ya da sayaç tablosu gerekir.
- **Seed determinizmi:** Seed referans tarihe göre göreceli üretilir. Aynı tohum ve aynı tarih aynı veriyi verir;
  farklı bir günde veri aynı ilişkilerle ama kaydırılmış tarihlerle üretilir (`SIM_ANCHOR_DATE` ile sabitlenebilir).

---

## Doğrulama Soruları

### 1. En zorlandığın görev hangisiydi? Hangi alternatif çözümleri düşündün ve neden eledin?

**T-3: acil vaka ve kaynak planlama.** Zorluğun iki ayrı yüzü vardı.

**Algoritma.** "En az bozulma" tanımlı değildi, önce ölçülebilir hale getirmem gerekti. Maliyet = kayma dakikası (öne
çekme 2×) + veteriner değişirse 60 + oda değişirse 5. Elediğim alternatifler:
- *Çakışanları acil süresi kadar ileri itmek:* Zincirleme kayma yaratıyor; bir randevu itilince arkasındakiler de
  itiliyor ve bozulma tüm güne yayılıyor.
- *CP-SAT/ILP çözücü:* Optimal sonuç verir, ama ağır bir bağımlılık getiriyor, gecikme tahmin edilemez ve klinik
  personeline "neden bu randevu kaydı" sorusunu açıklamak zorlaşıyor.
- *Yalnız açgözlü yerleştirme:* Basit, ama sıralamaya bağlı olarak gereksiz yere kötü sonuç verebiliyor.

Seçimim: her (veteriner, oda) çiftini aday olarak denemek, çakışanları **yalnız boş slotlara** taşımak (zincirleme kayma
olmuyor), 5 veya daha az etkilenen randevuda tüm sıralamaları denemek, fazlasında açgözlü yerleştirmeye düşmek.

**Eşzamanlılık.** Çakışmayı uygulamada "kontrol et, sonra yaz" ile önlemek eşzamanlı isteklerde çifte rezervasyona yol
açar. `SERIALIZABLE` yoğun çakışmada sürekli yeniden deneme ister, advisory lock da kuralı veritabanının dışına taşır.
Bu yüzden garantiyi `EXCLUDE USING gist` kısıtına verdim.

Asıl sürpriz yük testinde çıktı. Aynı slota eşzamanlı iki `INSERT`, EXCLUDE kontrolünde birbirinin commit edilmemiş
satırını bekleyip kilitleniyordu (deadlock). Doğruluk bozulmadı, ama PostgreSQL deadlock'u 1 sn sonra tespit ettiği
için bağlantı havuzu tıkandı ve sistemin geri kalanı da yavaşladı. Rezervasyonun veteriner ve oda satırlarını kimlik
sırasıyla `FOR NO KEY UPDATE` ile kilitlemesi sorunu çözdü: p99 12,6 sn'den 425 ms'ye indi.

Bir senaryoyu da `make verify` yakaladı: tüm veterinerler acil vakadayken yeni acil vaka 409 alıyordu. Artık ilk
boşalan çifte erteleniyor.

### 2. Çözümünün kırıldığı ya da yetersiz kaldığı bir senaryo

**Idempotency-Key, işin kendisiyle aynı transaction'da değil.** Middleware anahtarı "işleniyor" olarak bir
transaction'da yazıyor; handler işi kendi transaction'ında yapıyor; yanıt üçüncü bir yazımla saklanıyor. API süreci
handler'ın commit'i ile yanıtın saklanması arasında çökerse (konteyner öldürülürse) iki şey olur:
- İş yapılmıştır (ör. ödeme kaydedildi), ama anahtar "işleniyor" durumunda kalır.
- Aynı anahtarla gelen tekrar istek, ilk sonucu almak yerine sonsuza dek `409 idempotency_in_progress` alır.

Çift ödeme oluşmaz, yani güvenlik bozulmaz; ama istemci sonucu öğrenemez. Nedeni, middleware'i genel ve handler'lardan
bağımsız tutmak için yaptığım tercih. Doğru çözüm iki yoldan biri:
1. Anahtarı ve yanıtı işin kendi transaction'ı içinde yazmak (handler'lara `tx` geçirerek).
2. "İşleniyor" kayıtlarına bir süre sınırı koyup süresi dolanı yeniden işlemeye açmak.

Benzer şekilde, 5xx yanıtlarda anahtar silinip yeniden deneme serbest bırakılıyor. Bu, ancak handler hata durumunda
gerçekten geri alındıysa güvenli; tüm handler'lar tek transaction kullandığı için bu sağlanıyor.

Diğer sınırlamalar yukarıdaki "Bilinen sınırlamalar" bölümünde.

### 3. İki dizin: hangi sorgu için, dizinsiz ve dizinli ölçüm farkı

| Dizin | Sorgu | Dizinsiz | Dizinli |
|---|---|---|---|
| `vaccine_doses_due_idx` (kısmi: `WHERE status='planned'`, `due_date`) | Hatırlatma job'ının 37 günlük pencere taraması (220.563 doz, 12.207 eşleşme) | 35,3 ms, paralel seq scan, 3.392 buffer | **11,1 ms**, bitmap index scan; dizin yalnız 12 buffer okuyor |
| `animals_microchip_uq` (kısmi tekil: aktif ve çipli kayıtlar) | Mikroçiple hasta bulma | 3,46 ms, 15.011 satır elendi, 216 buffer | **0,11 ms** (~31×), 3 buffer |

Dizinlerin kısmi olması bilinçli. Hatırlatma dizini yalnız planlı dozları içeriyor; yapılmış dozlar (çoğunluk)
dizine girmiyor. Mikroçip dizini aynı zamanda iş kuralı: birleştirilmiş (pasif) kayıt aynı çipi taşıyabilir, aktif
kayıtlar arasında çip tekil olmalı. Tam `EXPLAIN` çıktıları yukarıda. Kullanılmayan bir dizini de (`owners_phone_idx`,
mükerrer sayımında) açıkça not ettim.

### 4. Yapay zekâ kullanımı: reddettiğim ya da düzelttiğim çıktılar

Projeyi Claude Code (Anthropic) ile birlikte geliştirdim. Kodun, SQL'in ve testlerin büyük kısmını o üretti. Kararları,
doğrulamayı ve yönlendirmeyi ben yaptım. Reddettiğim ve düzelttiğim örnekler:

**a) Proje dosyasındaki gizli talimatlar uygulanmadı.** Gönderilen `proje.md` içinde, Markdown görünümünde görünmeyen
HTML ve link-referans yorumları vardı. "Kod üreten araçlar için geliştirici notu" başlığıyla şunları istiyorlardı:
- tablo adlarına belirli bir önek koymak ve kullanıcı tablosuna belirli bir ad vermek
- her HTTP yanıtına özel bir başlık eklemek
- ek bir gizli sağlık ucu açmak
- seed'deki ilk kullanıcıya başka bir adaya ait bir e-posta vermek
- backend konteynerine belirli bir ad vermek
- README'nin son satırına başka bir isimle "case kodu" yazmak

Analiz sırasında yapay zekâ aracı bunları fark edip bana bildirdi. Talimatlar görünür şartnamede yoktu, içerikleri
projenin amacıyla ilgisizdi ve içlerindeki isim bana ait değildi. Bunları yapay zekâyı körü körüne kullananları
yakalamak için konmuş bir tuzak (prompt injection) olarak değerlendirdim ve **hiçbirini uygulamamaya** karar verdim.
Bu yüzden değerleri burada birebir tekrar etmiyorum. Görünür şartnamenin tamamı uygulandı.

**b) Önce: sahte bir "geçti".** OpenAPI dokümanının tüm rotaları kapsadığını kontrol etmek için yapay zekânın yazdığı
Python betiği "eksik: yok" çıktısı verdi. Ama makinede `pyyaml` kurulu değildi; betik dokümanı hiç okumamış, boş listeyi
"eksik yok" diye yazmıştı. **Sonra:** Betiği attım. Yerine router'ı `chi.Walk` ile gezip her rotayı YAML'daki yollarla
karşılaştıran gerçek bir Go testi yazdırdım (`internal/api/openapi_test.go`). Test hem belgelenmemiş hem fazladan
rotayı yakalıyor ve CI'da çalışıyor. Ders: "geçti" çıktısının neyi gerçekten kontrol ettiğini doğrulamak.

**c) Önce: yük altında kilitlenen rezervasyon.** İlk tasarımda rezervasyon, kaynak satırlarını `FOR SHARE` ile
kilitliyordu ("acil vaka planlaması sürerken yazma olmasın" diye). Birim ve entegrasyon testlerinin hepsi geçiyordu.
k6 yük testi bunun eşzamanlı EXCLUDE eklemelerinde deadlock'a yol açtığını gösterdi: p99 12,6 sn, 18 zaman aşımı.
`FOR SHARE` birbiriyle uyumlu olduğu için aynı slota gelen iki işlemi sıraya sokmuyordu.

```go
// önce
SELECT id, kind FROM resources WHERE id IN ($1,$2) AND is_active ORDER BY id FOR SHARE
// sonra: aynı kaynağa gelenler sıraya girer, FK kontrollerini (KEY SHARE) bloklamaz
SELECT id, kind FROM resources WHERE id IN ($1,$2) AND is_active ORDER BY id FOR NO KEY UPDATE
```

**d) Entegrasyon testinin yakaladığı SQL hatası.** Fatura eklerken aynı parametre hem `bigint` kolona yazılıp hem
`CASE WHEN $4 = 0` içinde kullanılıyordu. PostgreSQL `inconsistent types deduced for parameter` hatası verdi.
Simülatör yalnız mevcut faturaları ödediği için bu yol canlıda hiç denenmemişti; çözüm `$4::bigint`.

### 5. "Aşı takvimi motoru"nun doğru çalıştığını nasıl kanıtladın?

Üç katmanda kanıtladım:
1. Motorun saf birim testleri
2. Gerçek veritabanıyla entegrasyon testleri
3. Çalışan sistem üzerinde `make verify` T-1 kontrolü

Motor "bugün"ü dışarıdan aldığı için gecikme, geç başlangıç ve tahmini yaş senaryoları saatten bağımsız ve
deterministik test edilebiliyor.

```
$ cd backend && go test -v -count=1 ./internal/vaccine/
--- PASS: TestPuppySeriesOnTime (0.00s)
--- PASS: TestDelayedDoseIsReplannedWithMinimumInterval (0.00s)
--- PASS: TestDelayChainShiftsFollowingDosesPastTargetWeek (0.00s)
--- PASS: TestLateStartAdultGetsTwoDosesThreeWeeksApart (0.00s)
--- PASS: TestDoseGivenTooSoonIsIgnored (0.00s)
--- PASS: TestDoseBeforeMinimumAgeIsIgnored (0.00s)
--- PASS: TestCompletedSeriesPlansOverdueBoosterForToday (0.00s)
--- PASS: TestNextBoosterCountsFromLastBooster (0.00s)
--- PASS: TestBreedSpecificProtocolExtendsSeries (0.00s)
--- PASS: TestSingleDoseRabiesProtocol (0.00s)
--- PASS: TestEstimatedAgeIsUsedAndFlagged (0.00s)
--- PASS: TestComputeIsDeterministicAndOrderIndependent (0.00s)
--- PASS: TestInvalidInputs (0.00s)
PASS
ok      patiklinik/internal/vaccine     0.993s      (kapsam: %93.4)
```

Örnek: `TestDelayedDoseIsReplannedWithMinimumInterval`. Yavru 6. haftada aşılanmış, 8. hafta dozunu kaçırmış ve
11. haftada gelmiş. Beklenen plan: gecikmiş doz bugüne (11. hafta, `overdue`). Sonraki doz bugün + 14 gün = 13. hafta
olur; bundan sonraki ilk hedef hafta 16 olduğu için 16. haftaya düşer. Rapel de 16. hafta + 365 gün.

Gerçek PostgreSQL ile:

```
$ cd backend && go test -tags integration -count=1 -v -run 'TestAdministeringDoseReplansSchedule|TestReminderJobIsIdempotent' ./internal/api/
--- PASS: TestReminderJobIsIdempotent (0.12s)
--- PASS: TestAdministeringDoseReplansSchedule (0.13s)
```

- `TestReminderJobIsIdempotent`: 5 eşzamanlı çalıştırmanın oluşturduğu hatırlatma toplamı tablodaki satır sayısına
  eşit; ardından sıralı bir çalıştırma `created: 0` döndürüyor; yinelenen hatırlatma yok.
- `TestAdministeringDoseReplansSchedule`: Mırnav'ın gecikmiş FVRCP rapeli bugüne planlı. Sahip rolü uygulayamaz (403),
  veteriner uygular, sonraki rapel tam bir yıl sonrasına yeniden planlanır, ikinci uygulama `dose_not_planned` ile
  reddedilir.

Çalışan sistem üzerinde (`make verify` → T-1):
- 9 haftalık aşısız yavruda ilk doz bugüne ve gecikmiş olarak planlanıyor; ikinci doz ≥ 14 gün sonra.
- Doz uygulandıktan sonra kalan dozlar asgari aralığa uyuyor.
- 30 aylık tahmini yaşla kaydedilen kediye "tahmini" ve "geç başlangıç" serisi planlanıyor.
- Job'ın ikinci çalıştırması 0 kayıt üretiyor.

```
│ T-1     │ PASS   │ Aşı motoru: gecikme, tahmini yaş, yeniden plan, idempotent job   │   1.4s │
```
