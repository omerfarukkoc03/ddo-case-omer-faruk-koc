# Yük testi sonuçları (`make load`)

**Ortam:** Windows 11 dizüstü, Docker Desktop (WSL2), tüm servisler tek makinede. Ölçüm sırasında simülatör de canlı
olay üretiyordu. Veritabanında seed verisi vardı: 15.000 hayvan, ~47.000 muayene, ~120.000 aşı dozu. Mutlak sayılar
donanıma bağlıdır; burada önemli olan önce/sonra farkı ve doğruluk eşikleri.

**Senaryolar (eşzamanlı, 30 sn):**

| Senaryo | Yük | Neyi ölçer |
|---|---|---|
| `booking` (T-3) | 20 sanal kullanıcı, 12 slota sürekli rezervasyon | Yoğun çakışmada gecikme; her slotta en fazla 1 randevu |
| `dose` (T-2) | 50 istek/sn | Adım adım doz hesabının gecikmesi |
| `vaccPlan` (T-1) | 30 istek/sn, rastgele hayvan | Aşı motorunun her istekte planı yeniden hesaplama gecikmesi |

**Eşikler:** `double_bookings == 0`, beklenmeyen durum kodu < %1, doz ve plan için p95 < 300 ms, rezervasyon için p95 < 500 ms.

## Bulgu: eşzamanlı EXCLUDE eklemeleri kilitleniyordu

İlk çalıştırmada doğruluk tamdı: 2.420 denemeden tam 12 rezervasyon kabul edildi, çifte rezervasyon 0. Ancak gecikme
eşikleri aşıldı. Yavaş rezervasyonlar saniyede bir tamamlanıyordu, bu da PostgreSQL'in `deadlock_timeout`
(1 sn) değerine denk geliyordu.

Neden: Aynı slota eşzamanlı iki `INSERT`, EXCLUDE kontrolünde birbirinin commit edilmemiş satırını görüp birbirini
bekliyordu. Bu bir deadlock'tu; PostgreSQL bir saniye sonra tespit edip birini iptal ediyordu. Bekleyen işlemler
bağlantı tuttuğu için havuz tıkanıyor, doz ve plan istekleri de yavaşlıyordu.

Düzeltme: Rezervasyon artık veteriner ve oda satırlarını kimlik sırasıyla `FOR NO KEY UPDATE` ile kilitliyor
(`internal/api/appointments.go`). Aynı kaynağa gelen istekler kısa süre sıraya giriyor, ikincisi commit edilmiş
satırı görüp hemen 409 alıyor. Kilitlenme olmuyor.

| Ölçüm | Önce | Sonra |
|---|---:|---:|
| Rezervasyon denemesi (30 sn) | 2.420 | 4.678 |
| Rezervasyon p95 / p99 / en kötü | 163 ms / 12,6 sn / 15,2 sn (zaman aşımı) | 277 ms / 425 ms / 1,1 sn |
| Doz hesabı p95 | 847 ms | 54 ms |
| Aşı planı p95 | 691 ms | 91 ms |
| Beklenmeyen durum kodu | 18 (504 zaman aşımı) | 0 |
| Atılan iterasyon (havuz doygunluğu) | 407 | 5 |
| Kabul edilen rezervasyon / slot | 12 / 12 | 12 / 12 |
| Çifte rezervasyon | 0 | 0 |

`http_req_failed` oranının yüksek görünmesi (~%66) beklenen bir durum. k6 409 yanıtlarını başarısız sayıyor, oysa bunlar
dolu slota yapılan ve kısıtın doğru biçimde reddettiği rezervasyonlar.

Son çalıştırmanın ham özeti `load/results/summary.json` dosyasına yazılır (depoya eklenmez).
