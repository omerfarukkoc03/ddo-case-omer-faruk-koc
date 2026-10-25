# TESTING — PatiKlinik nasıl doğrulanır

## Hızlı doğrulama

```bash
cp .env.example .env
docker compose up --build -d   # ya da: make up
make verify                    # ya da: docker compose --profile verify run --rm verify
```

`make verify`, ayağa kalkmış ve seed edilmiş sistemde her özelliği ve her zorlu görevi API ve arayüz vekili üzerinden
uçtan uca dener. Sonunda bir PASS/FAIL tablosu basar; bir kontrol bile başarısızsa çıkış kodu 1 olur. Geliştirme
makinesinde ~5 sn sürer. Doğrulama aracı kendi test verisini rastgele mikroçip ve telefonla oluşturduğu için tekrar
tekrar ve `make reset-demo` sonrasında da çalıştırılabilir.

```
┌─────────┬────────┬──────────────────────────────────────────────────────────────────┬────────┐
│ Kimlik  │ Sonuç  │ Kontrol                                                          │   Süre │
├─────────┼────────┼──────────────────────────────────────────────────────────────────┼────────┤
│ HEALTH  │ PASS   │ GET /health veritabanı dahil sağlıklı                            │   0.0s │
│ AUTH-1  │ PASS   │ Yanlış parola ve bilinmeyen kullanıcı 401                        │   0.2s │
│ AUTH-2  │ PASS   │ Token'sız istek 401, yetkisiz rol 403                            │   0.0s │
│ AUTH-3  │ PASS   │ Yetki ihlali: sahip başkasının kaydına erişemez                  │   0.0s │
│ F-1     │ PASS   │ Sahip ve hayvan kaydı (mikroçip), sahip portal daveti            │   0.2s │
│ F-2     │ PASS   │ Randevu ve muayene                                               │   0.1s │
│ F-3     │ PASS   │ Aşı takvimi ve hatırlatma                                        │   0.7s │
│ F-4     │ PASS   │ İlaç, reçete ve stok                                             │   0.1s │
│ F-5     │ PASS   │ Yatan hasta ve kafes yönetimi                                    │   0.1s │
│ F-6     │ PASS   │ Fatura ve tahsilat                                               │   0.1s │
│ T-1     │ PASS   │ Aşı motoru: gecikme, tahmini yaş, yeniden plan, idempotent job   │   1.4s │
│ T-2     │ PASS   │ Doz: kontrendikasyon, sınır+gerekçe, seyreltme, snapshot         │   0.1s │
│ T-3     │ PASS   │ Kaynak planlama: çifte rezervasyon yarışı, acil vaka, kafes FSM  │   0.2s │
│ T-4     │ PASS   │ Çoklu sahiplik, birleştirme + geri alma, paket, idempotans       │   0.4s │
│ UI      │ PASS   │ Arayüz servis ediliyor ve API'ye vekil oluyor                    │   0.3s │
└─────────┴────────┴──────────────────────────────────────────────────────────────────┴────────┘
15/15 kontrol başarılı · toplam süre 4.7s
```

Diğer komutlar:

| Komut | Ne yapar |
|---|---|
| `make reset-demo` | Şemayı silip migrasyonları ve seed'i baştan uygular (aynı `SEED` → aynı veri). |
| `make test` | Go birim ve entegrasyon testleri (entegrasyon için Docker gerekir; gerçek PostgreSQL 16 testcontainers ile açılır). |
| `make test-unit` | Yalnız birim testleri (Docker gerekmez). |
| `make load` | k6 yük testi, ~40 sn. Sonuçlar ve yorumu: `load/RESULTS.md`. |
| `make lint` | golangci-lint. |

## (a) Demo hesapları

Tüm hesapların parolası: **`Demo123!`**. Giriş ekranında her rol için tek tıkla giriş düğmesi de vardır.

| Rol | E-posta | Not |
|---|---|---|
| Veteriner | `vet@patiklinik.test` | Dr. Ayşe Yılmaz (ayrıca `vet2@`, `vet3@`) |
| Teknisyen | `tekniker@patiklinik.test` | Aşı uygulama, kilo, kafes temizliği |
| Resepsiyon | `resepsiyon@patiklinik.test` | Kayıt, randevu, fatura, birleştirme |
| Eczane / depo | `eczane@patiklinik.test` | Reçete teslimi, stok |
| Hayvan sahibi | `sahip@patiklinik.test` | Deniz Aksoy: Karamel (1), Mırnav (2), Zeytin (4) |
| Hayvan sahibi (ortak) | `sahip2@patiklinik.test` | Ece Aksoy: yalnız Karamel'in ortak sahibi ve fatura sorumlusu |

Demo kayıtları (kimlikler sabittir):

| # | Kayıt | Ne göstermek için |
|---|---|---|
| Hayvan 1 | **Karamel**: 10 haftalık Golden Retriever, 2 sahip (Ece fatura sorumlusu), 4 seanslık aşı paketi (2 kullanılmış) | T-1 yavru serisi, T-4 fatura kuralı ve paket |
| Hayvan 2 | **Mırnav**: 3 yaşında kedi, FVRCP rapeli kaçırılmış | T-1 gecikmiş doz bugüne planlı; T-2 kedide parasetamol yasağı |
| Hayvan 3 | Karamel'in **mükerrer** kaydı: çipsiz, sahibi #3, kendi aşı geçmişi var | T-4 hayvan birleştirme ve geri alma |
| Hayvan 4 | **Zeytin**: Collie | T-2 ırka özgü kontrendikasyon (ivermektin, MDR1) |
| Sahip 3 | "deniz aksoy": Deniz Aksoy ile **aynı telefon**, adres dolu, e-posta yok | T-4 sahip birleştirme (adres hayatta kalan kayda aktarılır) |

## (b) Doğrulama kimlikleri: nasıl denenir, beklenen sonuç

| Kimlik | Nasıl denenir | Beklenen sonuç |
|---|---|---|
| **HEALTH** | `curl localhost:8080/health` | `200 {"status":"ok","database":"ok"}`. Veritabanı kapalıysa `503 degraded`. |
| **AUTH-1** | Yanlış parolayla ya da olmayan e-postayla `POST /api/auth/login` | İkisi de aynı `401 invalid_credentials`; hesabın var olup olmadığı anlaşılmaz. |
| **AUTH-2** | Token'sız `GET /api/animals`; sahip → `GET /api/drugs`; eczane → `POST /api/appointments`; resepsiyon → `POST /api/prescriptions` | Token yoksa `401 unauthorized`; yetkisiz rol `403 forbidden`. |
| **AUTH-3** | `sahip2@` ile `GET /api/animals/2` (Deniz'in kedisi), `/api/owners/1`, hayvan listesi | `403 forbidden`; liste yalnız hayvan 1'i içerir; sahip yalnız kendisine kesilen faturaları görür. |
| **F-1** | Resepsiyon: sahip + ortak sahip kaydı; mikroçipli hayvan kaydı; mikroçiple arama; aynı çiple ikinci kayıt; 15 haneli olmayan çip; sahibe `POST /api/owners/{id}/portal-invite`, sahip `POST /api/auth/activate` | Kayıt `201` (çoklu sahip + fatura sorumlusu); arama kaydı bulur; aynı çip `409 duplicate`; geçersiz çip `422 validation_failed`. Davet `XXXX-XXXX` kodu döner; yanlış kod ya da ikinci kullanım `401 invalid_invite`; etkinleşen sahip yalnız kendi hayvanını görür. |
| **F-2** | Randevu oluştur → `checked_in` → muayene kaydı (kilo, ateş, bulgu) | Randevu `completed` olur, kilo hayvana işlenir; geçersiz durum geçişi (geldi → gelmedi) `409 invalid_status_transition`. |
| **F-3** | Yeni köpek kaydı → `GET /api/animals/{id}/vaccinations`; `POST /api/jobs/reminders/run`; sahip `GET /api/reminders` | DHPP, kuduz ve lepto otomatik planlanır; hatırlatmalar e-posta şablonu metniyle listelenir. |
| **F-4** | Stok girişi → reçete → `POST /api/prescriptions/{id}/dispense` → tekrar teslim | Stok tam reçete miktarı kadar (FEFO) azalır, süresi geçmiş parti kullanılmaz; ikinci teslim `409 prescription_not_active`. |
| **F-5** | Yatış (kafes seçmeden) → aynı hayvanı tekrar yatır → not → taburcu → temizlik → hazır | Uygun ilk kafes `occupied`; ikinci aktif yatış `409 duplicate`; taburcu sonrası kafes `cleaning`, sonra `ready`. |
| **F-6** | 165.000 kuruşluk fatura → 100.000 öde → 100.000 daha öde → 65.000 öde → 1 kuruş öde | Fazla ödeme `422 overpayment`; tam ödemede `paid`; kapalı faturaya ödeme `409 invoice_not_open`. |
| **T-1** | (a) 9 haftalık aşısız yavru kaydı; (b) bugünkü dozu uygula; (c) `estimated_age_months: 30` ile kedi kaydı; (d) hatırlatma job'ını iki kez çalıştır | (a) Gecikmiş 6. hafta dozu **bugüne**, sonraki doz ≥ 14 gün sonraya planlanır (`overdue: true`). (b) Kalan dozlar asgari aralığa göre yeniden planlanır. (c) `birth_date_estimated: true`, plan `estimated_age` ve `late_start` (2 doz, 21 gün arayla). (d) İkinci çalıştırma `created: 0`. |
| **T-2** | Kedi + parasetamol (gerekçeli bile); Collie + ivermektin; meloksikam 0,3 mg/kg (azami 0,2) gerekçesiz, gerekçeli, 0,5 mg/kg; reçeteden sonra kilo değişimi; ketamin seyreltme | Kontrendikasyonlar `422 contraindicated`; gerekçesiz `422 max_dose_exceeded`; gerekçeli `201 exceeds_max: true` (denetim kaydında); 2× tavan `422 dose_hard_limit`; kilo değişince reçete dozu aynı kalır, `weight_changed_note` görünür; seyreltme 1,75 mL stok + 15,75 mL = 17,5 mL (C1·V1 = C2·V2), adım adım açıklamalı. |
| **T-3** | (a) Aynı veteriner/oda/saate 10 eşzamanlı rezervasyon; (b) `POST /api/appointments/emergency`; (c) boş kafesi `ready`'ye, hazır kafesi `cleaning`'e geçirmeyi dene | (a) Tam 1 `201`, 9 `409 resource_conflict`. (b) Acil vaka şimdi başlar (tüm çiftler başlamış ya da acil randevulardaysa ilk boşalan çifte ertelenir, `wait_minutes`); `moves` taşınanları ve maliyeti gösterir; o gün hiçbir kaynakta çakışma kalmaz. (c) `409 cage_invalid_transition`. |
| **T-4** | (a) Ortak sahibi fatura sorumlusu olan hayvana fatura, sonra `owner_id` ile; (b) aynı `Idempotency-Key` ile iki ödeme, aynı anahtarla farklı gövde, 8 eşzamanlı tam ödeme; (c) paket sat, iki aşıyı paketten uygula; (d) aynı telefonlu mükerrer sahibi birleştir, geri al, tekrar geri al | (a) `billing_rule: billing_owner` ortak sahibe; açık seçimde `explicit`. (b) Tekrar istek aynı yanıt + `Idempotent-Replayed: true`, tek ödeme; farklı gövde `422 idempotency_key_reused`; eşzamanlı ödemede yalnız 1 kabul. (c) `sessions_left: 2`, `redeemed: 2`. (d) Hayvan hayatta kalan sahibe geçer, geri almada eski sahibine döner; ikinci geri alma `409 already_undone`. |
| **UI** | `http://localhost:3000`, `/animals/1` derin bağlantı, arayüz kökeni üzerinden giriş + `/api/animals` | `200` (SPA), nginx `/api` isteklerini API'ye iletir. |

## (c) Arayüzde elle denenecek kısa senaryolar (http://localhost:3000)

1. **Canlı pano.** Resepsiyon ile girin. "Canlı" göstergesi yanıp söner, "Son olaylar" listesi simülatörün ürettiği
   randevu, ödeme ve acil vaka olaylarıyla birkaç saniyede bir güncellenir.
2. **Aşı motoru (T-1).** Veteriner → *Hasta kayıtları* → `Mırnav` → *Aşı takvimi*: FVRCP rapeli **Gecikmiş**. "Plan nasıl
   hesaplandı?" bölümünde gerekçeyi okuyun. Seri/lot no yazıp **Uygula**'ya basın: sonraki rapel bir yıl sonraya planlanır.
3. **Tahmini yaş.** Resepsiyon → *Yeni hayvan kaydı* → doğum tarihi boş, "tahmini yaş (ay)" = 2 → kaydedin. Doğum
   tarihinin yanında "tahmini yaştan" etiketi çıkar, yavru serisi planlanır.
4. **Doz hesabı (T-2).** Veteriner → *Reçete & doz* → hasta 4 (Zeytin, Collie):
   - İvermektin seçin → kontrendikasyon uyarısı ve `contraindicated`.
   - Meloksikam, 0,3 mg/kg → *Hesapla* → "azami sınır aşıldı". Gerekçe yazıp yeniden hesaplayın, adımları inceleyin,
     *Reçeteyi kaydet*.
   - Ketamin + seyreltme 10 mg/mL → seyreltme adımları.
5. **Acil vaka (T-3).** Veteriner → *Randevular* → *Acil vaka kabulü* (hasta 30, "trafik kazası"). Sonuç kutusu hangi
   randevuların nereye taşındığını ve bozulma maliyetini gösterir. Takvimde taşınan randevular "kaydırıldı (ilk: …)"
   olarak görünür.
6. **Kafes durum makinesi.** Teknisyen → *Yatan hasta*: yalnız izinli geçişlerin düğmeleri görünür. Veteriner olarak
   hasta yatırın, sonra taburcu edin: kafes **Temizlik** olur, teknisyen **→ Hazır** yapar.
7. **Fatura kuralı ve tahsilat (F-6, T-4).** Resepsiyon → *Fatura & tahsilat* → *Yeni fatura*, hasta 1 → fatura "Ece
   Aksoy (fatura sorumlusu)" adına kesilir. Kısmi ödeme alın, kalanından fazlasını girmeyi deneyin.
8. **Birleştirme (T-4).** Resepsiyon → *Mükerrer kayıtlar*: `#1 Deniz Aksoy ↔ #3 deniz aksoy` → **Birleştir** →
   geçmişte **Geri al**. Hayvanlarda `Karamel ↔ Karamel (çipsiz)` için aynı akış.
9. **Yeni sahibin hesabı.** Resepsiyon → *Hasta kayıtları* → *Yeni sahip kaydı* (e-postasıyla) → *Yeni hayvan kaydı*
   (sahip no ile). Açılan hasta detayında sahibin yanında **Portal daveti** → *Kod üret*. Çıkış yapın, giriş ekranında
   **Hesabımı etkinleştir** → aynı e-posta, kod ve yeni parola. Yalnız o hayvan görünür. Kodu ikinci kez kullanmayı
   deneyin: "kod geçersiz…".
10. **Rol sınırları.** `sahip2@` ile girin: menüde yalnız sahip ekranları görünür, *Sağlık karnesi* yalnız Karamel'i
   gösterir. Adres çubuğuna `/animals/2` yazın: "Veri yüklenemedi … (forbidden)".
11. **Mobil.** Tarayıcı genişliğini ~375 px'e indirin: menü ☰ düğmesine taşınır, tablolar kart içinde yatay kayar.

## (d) Zorlu görevlerin kanıt komutları

Tüm komutlar repo kökünden çalıştırılır. `-tags integration` olan komutlar Docker gerektirir (testcontainers
PostgreSQL 16 açar).

| Görev | Kanıt komutu | Ne kanıtlar |
|---|---|---|
| **T-1** | `cd backend && go test -v -count=1 ./internal/vaccine/` | 13 tablo testi: zamanında seri, gecikme zinciri, geç başlangıç, asgari aralıktan erken/asgari yaştan önce doz sayılmaz, ırka özel 20. hafta, tek dozluk kuduz, tahmini yaş, determinizm. Kapsam %93. |
| | `cd backend && go test -tags integration -count=1 -v -run 'TestAdministeringDoseReplansSchedule\|TestReminderJobIsIdempotent' ./internal/api/` | Gecikmiş rapel bugüne planlı, uygulama sonrası bir yıl sonraya yeniden planlanır; 5 eşzamanlı + 1 sıralı job çalıştırmasında yinelenen hatırlatma yok. |
| **T-2** | `cd backend && go test -v -count=1 ./internal/dosage/` | 12 test: decimal kesinliği, kontrendikasyon, ırk kontrendikasyonu, gerekçeli aşım, tek doz tavanı, 2× mutlak tavan, ¼ tablet güvenli yuvarlama, seyreltme. Kapsam %93. |
| | `cd backend && go test -tags integration -count=1 -v -run 'TestPrescriptionSafetyAndSnapshot\|TestDispenseUnderContentionNeverOversells' ./internal/api/` | Kilo değişince reçete aynı kalır, veritabanı tetikleyicisi doz güncellemesini reddeder; sınırlı stokta 6 eşzamanlı teslimden tam 2'si başarılı, süresi geçmiş parti hiç kullanılmaz. |
| **T-3** | `cd backend && go test -v -count=1 ./internal/schedule/ ./internal/cagefsm/` | 10 algoritma testi (en az bozulma, başlamış randevuya dokunmama, mesai sonu → ertesi sabah, ameliyathane, bekleme, tam aramanın açgözlüden kötü olmaması); kafes FSM'nin 16 geçişinin tamamı. |
| | `cd backend && go test -tags integration -count=1 -v -run 'TestDoubleBookingRaceOnlyOneWins\|TestEmergencyReschedulesConflictingAppointments\|TestCageStateMachine' ./internal/api/` | 20 eşzamanlı rezervasyondan 1'i kabul edilir; acil vaka sonrası hiçbir kaynakta çakışma yok ve taşımalar denetim kaydında; tetikleyici doğrudan SQL'de de geçersiz geçişi reddeder. |
| | `make load` | Yük altında doğruluk ve gecikme: 4.678 rezervasyon denemesinde çifte rezervasyon 0, rezervasyon p99 425 ms (`load/RESULTS.md`). |
| **T-4** | `cd backend && go test -tags integration -count=1 -v -run 'TestBillingOwnerRule\|TestOwnerMergeAndUndoRestoresState\|TestAnimalMergeCombinesHistoryAndUndo\|TestPackageSessionDeductionIsConsistent\|TestIdempotencyKeyReplaysSameResult\|TestConcurrentPaymentsNeverOverpay' ./internal/api/` | Fatura kuralının üç dalı; birleştirme + geri almadan sonra ilgili satırlar birebir eski hali; hayvan birleştirmede aşı geçmişi birleşir ve geri ayrılır; 20 eşzamanlı koşullu düşümde 5 seansın tam 5'i düşülür, seans bitince işlem geri alınır; idempotent tekrar; eşzamanlı ödemede fazla tahsilat yok. |
| **Portal** | `cd backend && go test -tags integration -count=1 -v -run TestOwnerPortalInviteAndActivation ./internal/api/` | Yalnız resepsiyon davet üretir; kod idempotency önbelleğine ve denetim kaydına sızmaz; zayıf parola, yanlış e-posta, yanlış kod, iptal edilmiş ve süresi dolmuş davet reddedilir; kod tek kullanımlık ve büyük/küçük harf ile tireden bağımsız; yeni sahip yalnız kendi hayvanını görür. |
| **Seed** | `cd backend && go test -v -count=1 -run TestGenerate ./internal/seed/` | Aynı tohum → aynı SHA-256 özeti; farklı tohum → farklı özet; tekil mikroçip, çakışmasız seed randevuları, fazla tahsil edilmemiş faturalar. |
| **Hepsi** | `make test` | 58 test (40 birim + 18 entegrasyon). CI'da `-race` ile çalışır. |
