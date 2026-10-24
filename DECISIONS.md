# Mimari karar kayıtları (ADR)

Her kayıt dört bölümden oluşuyor: bağlam, seçenekler, karar ve sonuç. Kayıtlar, karar verildiği sıraya göre dizildi.

---

## ADR-001 — Randevu çakışmasını veritabanı EXCLUDE kısıtıyla garanti etmek

**Bağlam.** Veteriner, muayene odası ve ameliyathane aynı anda iki aktif randevuda kullanılamaz. Rezervasyonlar resepsiyondan,
acil vaka akışından ve simülatörden eşzamanlı gelir. Şartname, yarış durumunun bir testle kanıtlanmasını istiyor.

**Seçenekler.**
1. Uygulamada önce kontrol edip sonra yazmak (`SELECT … WHERE çakışan` ardından `INSERT`). Eşzamanlı iki istek kontrolü
   birlikte geçip birlikte yazabilir (TOCTOU), bu yüzden doğru değil.
2. `SERIALIZABLE` izolasyon ve yeniden deneme. Doğru, ama yoğun çakışmada sürekli yeniden deneme gerekir. Garanti de
   uygulamanın her yolda bu izolasyonu kullanmasına bağlı kalır.
3. Kaynak başına advisory lock. Doğru, ama kuralı veritabanından bağımsız bir sözleşmeye taşır: doğrudan SQL ya da
   unutulmuş bir kod yolu kuralı aşabilir.
4. `btree_gist` ile `EXCLUDE USING gist (vet_id WITH =, tstzrange(starts_at, ends_at) WITH &&) WHERE (aktif)`, oda için
   de aynısı.

**Karar.** Seçenek 4. Kısıt `DEFERRABLE INITIALLY IMMEDIATE`. Acil vaka akışı, randevuları birbirinin boşalttığı
yerlere taşırken ara adımlarda geçici çakışma olabileceği için `SET CONSTRAINTS … DEFERRED` ile kontrolü commit anına
erteler. Garanti yine commit'te uygulanır.

**Sonuç.** Çifte rezervasyon hangi kod yolundan gelirse gelsin imkânsız. Kanıtları:
- `TestDoubleBookingRaceOnlyOneWins`: 20 eşzamanlı istekten 1'i kabul edilir.
- `make verify` T-3 kontrolü.
- k6: 4.678 denemede 12 slota tam 12 randevu.

Bir yan etkiyi yük testi ortaya çıkardı. Aynı slota eşzamanlı iki `INSERT`, EXCLUDE kontrolünde birbirinin commit
edilmemiş satırını bekleyip kilitleniyordu (deadlock). Doğruluk bozulmadı, ama PostgreSQL deadlock'u ancak 1 sn sonra
tespit ettiği için bağlantı havuzu tıkandı. Bu yüzden rezervasyon artık veteriner ve oda satırlarını kimlik sırasıyla
`FOR NO KEY UPDATE` ile kilitliyor. Rezervasyon p99 12,6 sn'den 425 ms'ye indi (`load/RESULTS.md`). Kısıt garantiyi
veriyor, kilit sırası da performansı koruyor.

---

## ADR-002 — Acil vaka yeniden planlaması: açık maliyet fonksiyonu, küçük kümede tam arama

**Bağlam.** Acil vaka "şimdi" başlamalı. Çakıştığı randevular "en az bozulmayla" yeniden planlanmalı. Şartname bozulmanın
nasıl ölçüleceğini tanımlamıyor.

**Seçenekler.**
1. Çakışan her şeyi acil vaka süresi kadar ileri itmek. Basit, ama zincirleme kayma yaratır: bir randevu itilince
   arkasındaki de itilir ve bozulma tüm güne yayılır.
2. Kısıt çözücü ya da tam sayılı programlama (CP-SAT, ILP). Optimal sonuç verir, ama ağır bir bağımlılık getirir,
   gecikme tahmin edilemez ve sonucu açıklamak zorlaşır.
3. Açık maliyet fonksiyonu ile arama. Maliyet = kayma dakikası (öne çekme 2×) + veteriner değişirse 60 + oda değişirse 5.
   Her (veteriner, oda) çifti aday olarak denenir. Etkilenen randevu 5 veya daha azsa tüm yerleştirme sıraları denenir
   (en fazla 120 permütasyon), fazlaysa başlangıç saatine göre açgözlü yerleştirme yapılır.

**Karar.** Seçenek 3 (`internal/schedule`). Randevular yalnız boş slotlara taşınır, bu yüzden zincirleme kayma olmaz.
Başlamış (`checked_in`) ve başka acil randevular taşınmaz. Hiçbir çift şimdi müsait değilse acil vaka ilk boşalan çifte
ertelenir (en fazla 4 saat) ve bekleme süresi yanıtta bildirilir. Bu davranışı canlı simülatöre karşı çalışan
`make verify` ortaya çıkardı. 7 gün içinde yer bulunamayan randevu iptal edilir ve elle yeniden planlanmak üzere
yanıtta listelenir.

**Sonuç.** Algoritma saf Go. 10 birim testle doğrulandı, test kapsamı %95. Arama hiçbir zaman açgözlü yerleştirmeden
daha kötü sonuç vermez (`TestExhaustiveOrderingNeverWorseThanGreedy`). Yanıt hangi randevunun nereye taşındığını ve
toplam maliyeti açıkça gösterir. Sınırlamalar: 5'ten fazla etkilenen randevuda sonuç optimal olmayabilir; ceza
katsayıları sabittir (klinik politikasına göre yapılandırılabilir olmalı).

---

## ADR-003 — Geri alınabilir birleştirme: silme yok, taşınan satır kimlikleriyle anlık görüntü

**Bağlam.** Mükerrer sahip ve hayvan kayıtları birleştirilebilmeli, birleştirme geri alınabilmeli. Birleştirmeden sonra
sistem çalışmaya devam eder: yeni randevu, fatura ve aşı kayıtları eklenir.

**Seçenekler.**
1. Mükerreri silip bağlı kayıtları taşımak, silinenin tam yedeğini JSON olarak tutmak. Geri almada tüm satırları
   yeniden yazmak gerekir. Bu sırada eklenen kayıtlarla çakışır, kimlikler değişebilir.
2. Olay kaynağı (event sourcing). Bu ölçek için orantısız karmaşıklık.
3. Mükerrer kaydı silmemek (`merged_into` ile pasifleştirmek), yabancı anahtarları hayatta kalan kayda taşımak ve
   taşınan **her satırın kimliğini** `merge_operations.snapshot` içine yazmak.

**Karar.** Seçenek 3 (`internal/api/merge.go`). Geri alma yalnız snapshot'taki satırları, hâlâ hayatta kalan kayda
bağlılarsa geri taşır. Tek bir satır bile sonradan değişmişse işlem kısmi geri alma yapmaz, `409 merge_undo_conflict`
döner ve transaction'ın tamamı geri alınır. İki kayıtta da aynı sahip bağlantısı olduğunda mükerrer bağlantı silinir;
tam satırı snapshot'ta tutulur ve geri almada yeniden eklenir. Hayvan birleştirmede yapılmış aşı dozları taşınır,
mükerrerin planlı dozları iptal edilir. Plan, birleşik geçmişten T-1 motoruyla yeniden hesaplanır. Geri almada da
her iki kayıt yeniden hesaplanır.

**Sonuç.** Birleştirmeden sonra eklenen kayıtlar etkilenmez. Birleştirme zinciri ters sırada geri alınmalıdır; bu
kural zorunlu tutulur. Kanıtlar: `TestOwnerMergeAndUndoRestoresState` (birleştirme ve geri almadan sonra ilgili tüm
satırlar birebir eski halinde) ve `TestAnimalMergeCombinesHistoryAndUndo`.

---

## ADR-004 — Doz hesabı: decimal aritmetik, kesin ve gerekçeli kural ayrımı, değiştirilemez reçete kopyası

**Bağlam.** Doz hesabı hataya kapalı olmalı. Kilo değiştiğinde eski reçeteler değişmemeli. Sınırı aşan doz yalnız
gerekçeli onayla yazılabilmeli.

**Seçenekler.**
- Sayı tipi: `float64` (0,1 + 0,2 ≠ 0,3; doz ve seyreltmede yuvarlama hatası) ya da `shopspring/decimal` ile
  PostgreSQL `numeric`.
- Reçete saklama: yalnız girdileri saklayıp okumada yeniden hesaplamak (kilo değişince sonuç da değişir) ya da hesabı
  ve girdileri yazma anında kopyalamak.
- Güvenlik kuralları: tek bir "uyarı" seviyesi ya da kesin engel ile gerekçeli onayın ayrılması.

**Karar.** Tüm hesap `decimal` ile yapılıyor. Veritabanında `numeric`, pgx'te shopspring tipi kayıtlı. Reçete; kiloyu,
türü, mg/kg değerini, sonucu ve adım adım hesabı (`calculation jsonb`) kopyalıyor. Bir veritabanı tetikleyicisi bu
alanların sonradan güncellenmesini reddediyor. Kural seviyeleri:
- Tür/ırk kontrendikasyonu (kedide parasetamol, Collie'de ivermektin) kesin engeldir, gerekçeyle de aşılamaz.
- Azami doz aşımı yalnız gerekçeyle (en az 10 karakter) yazılabilir ve denetim kaydına `prescription.create_override`
  olarak düşer.
- Azaminin 2 katı mutlak tavandır, hiçbir koşulda aşılamaz.

Tabletler ¼'e yuvarlanır. Yuvarlanmış doz aralık dışına çıkıyorsa aralıkta kalan çeyrek seçilir. Hiçbiri uymuyorsa sıvı
form önerilir. Örnek: 30 kg köpeğe 15 mg/kg parasetamol 0,9 tablet eder; en yakın çeyrek olan 1 tablet azami dozu aşar,
bu yüzden 0,75 tablet seçilir.

**Sonuç.** 12 birim test (kapsam %93) ve entegrasyon testleri. Ayrıca veritabanı seviyesinde bir test, reçetedeki dozu
değiştiren `UPDATE`'in reddedildiğini doğrular.

---

## ADR-005 — Kimlik doğrulama: kısa ömürlü JWT ve veritabanında tutulan, dönen refresh token

**Bağlam.** Şartname JWT (access + refresh) ya da sunucu taraflı oturum istiyor. Her uç noktada RBAC zorunlu.
Hayvan sahibi yalnız kendi kayıtlarını görmeli.

**Seçenekler.** (1) Yalnız uzun ömürlü JWT: iptal edilemez. (2) Sunucu taraflı oturum: basit, ama her istekte
veritabanı sorgusu gerekir. (3) 15 dk'lık JWT access token, veritabanında hash'i saklanan tek kullanımlık refresh
token ve yeniden kullanımda tüm token ailesinin iptali.

**Karar.** Seçenek 3. Parolalar bcrypt ile özetlenir. Bilinmeyen e-postada da bcrypt çalıştırılır, böylece yanıt
süresinden kullanıcının var olup olmadığı anlaşılamaz. Rol kontrolü router seviyesinde her uç noktaya ayrı yazılı
(`internal/api/router.go`, tek dosyada okunabilir). Sahip rolü için satır seviyesi kontrol (yalnız kendi hayvanı ve
kendisine kesilen fatura) handler içinde yapılır.

**Sonuç.** Access token, çıkıştan sonra da süresi dolana kadar (en fazla 15 dk) geçerli kalır. Bu bilinen ve kabul
edilen bir ödünleşim. Kanıtlar: `TestAuthenticationAndRoleChecks` (refresh yeniden kullanımı ailenin tamamını iptal
eder), `TestOwnerSeesOnlyOwnAnimals` ve `make verify` AUTH-1..3.

---

## ADR-006 — Deterministik seed: veritabanından bağımsız üretici, özetle kanıtlanan aynılık

**Bağlam.** 15.000 hayvan ve 5 yıllık geçmiş üretilmeli. Aynı tohum aynı veriyi üretmeli. Soğuk makinede sistem 5 dakika
içinde hazır olmalı.

**Seçenekler.** (1) SQL `generate_series` + `random()`: tohum kontrolü zayıf, karmaşık kuralları (aşı protokolü) ifade
etmek zor. (2) Simülatörün kayıtları API üzerinden tek tek oluşturması: gerçekçi, ama 15.000 hayvan ve yüz binlerce
alt kayıt için dakikalar sürer. (3) Saf Go üretici (`Generate(seed, referans tarih)`) ve `COPY` ile toplu yükleme.

**Karar.** Seçenek 3 (`internal/seed`). Üretici tüm kimlikleri kendisi atar. PRNG olarak tohumlu PCG kullanılır.
Geçmiş aşı dozları, T-1 motoru adım adım çalıştırılıp gerçekçi gecikmeler ve ihmaller eklenerek üretilir. Bu sayede
seed ile API arasında protokol tutarsızlığı olmaz. Canlı simülasyon ise API üzerinden yapılır, böylece canlı olaylar
yetki, çakışma, idempotans ve denetim kurallarından geçer.

**Sonuç.** Yaklaşık 20 sn'de ~400.000 satır yüklenir. `TestGenerateIsDeterministic`, aynı tohumla iki üretimin SHA-256
özetinin aynı olduğunu doğrular. Özet, yüklemede denetim kaydına da yazılır. Veri referans tarihe göre ("bugün")
göreceli üretilir: aynı tohum ve aynı tarih bit düzeyinde aynı veriyi verir. Tarih `SIM_ANCHOR_DATE` ile
sabitlenebilir.
