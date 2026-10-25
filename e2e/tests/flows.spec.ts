import { test, expect, loginAs, logout, expectLoadedWithoutErrors } from "./fixtures";

const rnd = () => Math.random().toString().slice(2, 9);

test("T-2 doz hesabı: kontrendikasyon, sınır aşımı + gerekçe, adım adım hesap ve reçete", async ({ page }) => {
  await loginAs(page, "Veteriner");
  await page.getByRole("link", { name: "Reçete & doz" }).click();
  await page.getByLabel("Hasta no").fill("4"); // Zeytin, Collie
  await expect(page.getByText(/Zeytin/)).toBeVisible();

  await page.getByLabel("İlaç").selectOption({ label: "İvermektin 10 mg/mL enjeksiyon" });
  await expect(page.getByText("Kontrendikasyon").first()).toBeVisible();
  await page.getByRole("button", { name: "Hesapla" }).click();
  await expect(page.locator(".state-error")).toContainText("Kontrendikasyon");

  await page.getByLabel("İlaç").selectOption({ label: "Meloksikam 5 mg/mL enjeksiyon" });
  await page.getByLabel("Doz oranı (mg/kg)").fill("0.3");
  await page.getByRole("button", { name: "Hesapla" }).click();
  await expect(page.locator(".state-error")).toContainText("azami sınırı aşıyor");
  await page.getByLabel("Sınır aşımı gerekçesi").fill("Şiddetli ağrı, uzman konsültasyonu ile onaylandı");
  await page.getByRole("button", { name: "Hesapla" }).click();
  await expect(page.locator("ol.steps li").first()).toBeVisible();
  await page.screenshot({ path: "screenshots/flow-dose.png", fullPage: true });
  await page.getByRole("button", { name: "Reçeteyi kaydet" }).click();
  await expect(page.locator(".state-ok")).toContainText("kaydedildi");
});

test("T-3 acil vaka: kabul edilir ve sonuç açıklaması gösterilir", async ({ page }) => {
  await loginAs(page, "Veteriner");
  await page.getByRole("link", { name: "Randevular" }).click();
  await expectLoadedWithoutErrors(page);
  const card = page.locator(".card", { hasText: "Acil vaka kabulü" });
  await card.getByLabel("Hasta no").fill("30");
  await card.getByLabel("Şikâyet").fill("Uçtan uca test: trafik kazası");
  await card.getByRole("button", { name: "Acil vakayı şimdi başlat" }).click();
  await expect(card.locator(".state-ok")).toContainText("seçildi");
  await page.screenshot({ path: "screenshots/flow-emergency.png", fullPage: true });
});

test("F-5 kafes: taburcu → temizlik → hazır", async ({ page }) => {
  await loginAs(page, "Veteriner");
  await page.getByRole("link", { name: "Yatan hasta" }).click();
  await expectLoadedWithoutErrors(page);
  const admit = page.locator(".card", { hasText: "Hasta yatır" });
  await admit.getByLabel("Neden").fill("Uçtan uca test gözlemi");
  // Rastgele seçilen hasta zaten yatıyor olabilir (sunucu doğru biçimde reddeder); bu durumda başka hasta denenir.
  const result = admit.getByText(/Yatış #\d+, kafes #\d+/);
  for (let attempt = 0; attempt < 5 && !(await result.isVisible()); attempt++) {
    await admit.getByLabel("Hasta no").fill(String(100 + Math.floor(Math.random() * 5000)));
    await admit.getByRole("button", { name: "Yatır" }).click();
    await expect(result.or(admit.locator(".state-error"))).toBeVisible();
  }
  await expect(result).toBeVisible();
  const cageNo = (await admit.getByText(/Yatış #\d+, kafes #\d+/).innerText()).match(/kafes #(\d+)/)![1];
  const cage = page.locator(".cage", { hasText: `K-${cageNo.padStart(2, "0")}` });
  await cage.getByRole("button", { name: "Taburcu et" }).click();
  await expect(cage.getByText("Temizlik")).toBeVisible();
  await logout(page);
  await loginAs(page, "Teknisyen");
  await page.getByRole("link", { name: "Yatan hasta" }).click();
  await page.locator(".cage", { hasText: `K-${cageNo.padStart(2, "0")}` }).getByRole("button", { name: "→ Hazır" }).click();
  await expect(page.locator(".cage", { hasText: `K-${cageNo.padStart(2, "0")}` }).getByText("Hazır", { exact: true })).toBeVisible();
});

test("F-6 fatura: fatura kesilir, kısmi ödeme alınır, fazla ödeme reddedilir", async ({ page }) => {
  await loginAs(page, "Resepsiyon");
  await page.getByRole("link", { name: "Fatura & tahsilat" }).click();
  await page.getByRole("button", { name: /Yeni fatura/ }).click();
  const form = page.locator("form", { hasText: "Fatura kes" });
  await form.getByLabel("Hasta no").fill("1");
  await form.getByLabel("Tutar (TL)").fill("750");
  await form.getByRole("button", { name: "Fatura kes" }).click();
  await expect(form.locator(".state-ok")).toContainText("Ece Aksoy");
  const pay = page.locator(".card", { hasText: /Fatura #\d+/ }).last();
  await pay.getByLabel("Tutar (TL)").fill("9999");
  await pay.getByRole("button", { name: "Tahsil et" }).click();
  await expect(pay.locator(".state-error")).toContainText("kalan tutarını aşıyor");
  await pay.getByLabel("Tutar (TL)").fill("750");
  await pay.getByRole("button", { name: "Tahsil et" }).click();
  await expect(pay.getByText("Ödendi").or(pay.getByText("₺0,00"))).toBeVisible();
  await page.screenshot({ path: "screenshots/flow-billing.png", fullPage: true });
});

test("Sahip portalı: resepsiyon davet kodu üretir, sahip hesabını etkinleştirir ve yalnız kendi hayvanını görür", async ({ page }) => {
  const email = `e2e${rnd()}@example.com`;
  await loginAs(page, "Resepsiyon");
  await page.getByRole("link", { name: "Hasta kayıtları" }).click();
  await page.getByRole("button", { name: /Yeni sahip kaydı/ }).click();
  const ownerForm = page.locator("form", { hasText: "Ad soyad" });
  await ownerForm.getByLabel("Ad soyad").fill("Uçtan Uca Sahip");
  await ownerForm.getByLabel("Telefon").fill("0532" + rnd());
  await ownerForm.getByLabel("E-posta").fill(email);
  await ownerForm.getByRole("button", { name: "Kaydet" }).click();
  const ownerId = (await ownerForm.locator(".state-ok").innerText()).match(/#(\d+)/)![1];

  await page.getByRole("button", { name: /Yeni hayvan kaydı/ }).click();
  const animalForm = page.locator("form", { hasText: "Kaydet ve aşı planını oluştur" });
  await animalForm.getByLabel("Ad", { exact: true }).fill("Portakal");
  await animalForm.getByLabel("Tür").selectOption("cat");
  await animalForm.getByLabel("…ya da tahmini yaş (ay)").fill("3");
  await animalForm.getByLabel("Sahip no", { exact: true }).fill(ownerId);
  await animalForm.getByRole("button", { name: "Kaydet ve aşı planını oluştur" }).click();
  await expect(page.getByRole("heading", { name: /Portakal/ })).toBeVisible();
  await expect(page.getByText("tahmini yaştan")).toBeVisible();

  await page.getByRole("button", { name: "Portal daveti" }).click();
  await page.getByRole("button", { name: "Kod üret" }).click();
  const code = (await page.locator(".mono", { hasText: /^[A-Z0-9]{4}-[A-Z0-9]{4}$/ }).innerText()).trim();
  await page.screenshot({ path: "screenshots/flow-invite.png", fullPage: true });
  await logout(page);

  await page.getByRole("button", { name: "Hesabımı etkinleştir →" }).click();
  await page.getByLabel("E-posta").fill(email);
  await page.getByLabel("Etkinleştirme kodu").fill(code);
  await page.getByLabel("Yeni parola").first().fill("E2eParola123");
  await page.getByLabel("Yeni parola (tekrar)").fill("E2eParola123");
  await page.screenshot({ path: "screenshots/flow-activate.png" });
  await page.getByRole("button", { name: "Hesabımı etkinleştir ve giriş yap" }).click();
  await expect(page.getByRole("heading", { name: "Sağlık karnesi" })).toBeVisible();
  await expectLoadedWithoutErrors(page);
  await expect(page.locator(".page .row button")).toHaveCount(1);
  await expect(page.getByRole("button", { name: /Portakal/ })).toBeVisible();
});

test("Mükerrer kayıt: sahip birleştirilir ve geri alınır", async ({ page }) => {
  await loginAs(page, "Resepsiyon");
  await page.getByRole("link", { name: "Mükerrer kayıtlar" }).click();
  await expectLoadedWithoutErrors(page);
  const owners = page.locator(".card", { hasText: "Olası mükerrer sahipler" });
  await owners.getByRole("button", { name: "Birleştir" }).first().click();
  await expect(page.locator(".state-ok")).toContainText("Birleştirme #");
  const history = page.locator(".card", { hasText: "Birleştirme geçmişi" });
  await history.getByRole("button", { name: "Geri al" }).first().click();
  await expect(history.getByText(/geri alındı/).first()).toBeVisible();
});
