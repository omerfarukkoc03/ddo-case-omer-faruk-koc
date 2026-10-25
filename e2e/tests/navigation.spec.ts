import { test, expect, loginAs, logout, expectLoadedWithoutErrors, type Role } from "./fixtures";

// Her rol için menüdeki her sayfa açılır; hiçbir sayfada hata kutusu, konsol hatası ya da 5xx olmamalı.
// Ekran görüntüleri screenshots/ klasörüne yazılır (görsel inceleme için).
const roles: { role: Role; slug: string; menu: string[]; notInMenu: string[] }[] = [
  { role: "Veteriner", slug: "vet", menu: ["Pano", "Hasta kayıtları", "Randevular", "Reçete & doz", "Eczane & stok", "Yatan hasta", "Hatırlatmalar"], notInMenu: ["Mükerrer kayıtlar"] },
  { role: "Teknisyen", slug: "tech", menu: ["Pano", "Hasta kayıtları", "Randevular", "Yatan hasta"], notInMenu: ["Reçete & doz", "Fatura & tahsilat"] },
  { role: "Resepsiyon", slug: "reception", menu: ["Pano", "Hasta kayıtları", "Randevular", "Yatan hasta", "Fatura & tahsilat", "Mükerrer kayıtlar", "Hatırlatmalar"], notInMenu: ["Reçete & doz"] },
  { role: "Eczane / depo", slug: "pharmacy", menu: ["Pano", "Eczane & stok"], notInMenu: ["Hasta kayıtları", "Fatura & tahsilat"] },
  { role: "Hayvan sahibi", slug: "owner", menu: ["Hayvanlarım", "Randevular", "Fatura & tahsilat", "Hatırlatmalar"], notInMenu: ["Pano", "Eczane & stok"] },
];

for (const r of roles) {
  test(`${r.role}: menüdeki tüm sayfalar hatasız açılır`, async ({ page }) => {
    await loginAs(page, r.role);
    const nav = page.locator("nav.nav");
    for (const item of r.notInMenu) {
      await expect(nav.getByRole("link", { name: item })).toHaveCount(0);
    }
    for (const item of r.menu) {
      await nav.getByRole("link", { name: item }).click();
      await expectLoadedWithoutErrors(page);
      await expect(page.locator("h1")).toBeVisible();
      await page.screenshot({ path: `screenshots/${r.slug}-${item.replace(/[^a-zA-Zçğıöşü]+/gi, "_")}.png`, fullPage: true });
    }
  });
}

test("hasta detayı: aşı planı, kilo grafiği ve sahipler görünür", async ({ page }) => {
  await loginAs(page, "Veteriner");
  await page.goto("/animals/1");
  await expectLoadedWithoutErrors(page);
  await expect(page.getByRole("heading", { name: /Karamel/ })).toBeVisible();
  await expect(page.getByText("Fatura sorumlusu", { exact: true })).toBeVisible();
  await expect(page.locator("svg.chart")).toBeVisible();
  await expect(page.getByRole("cell", { name: /DHPP/ }).first()).toBeVisible();
  await page.screenshot({ path: "screenshots/vet-animal-detail.png", fullPage: true });
});

test("yetkisiz sayfa: sahip /pharmacy adresine girerse kendi ana sayfasına yönlenir", async ({ page }) => {
  await loginAs(page, "Hayvan sahibi");
  await page.goto("/pharmacy");
  await expect(page).toHaveURL(/\/my$/);
  await expectLoadedWithoutErrors(page);
  // Başkasının hayvanının detayı: sunucu reddeder, arayüz Türkçe ve tekrar düğmesiz hata gösterir.
  await page.goto("/animals/5");
  await expect(page.locator(".state-error")).toContainText("yetkiniz yok");
  await expect(page.getByRole("button", { name: "Yeniden dene" })).toHaveCount(0);
});

test("giriş: yanlış parola Türkçe hata verir", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("E-posta").fill("vet@patiklinik.test");
  await page.getByLabel("Parola").fill("yanlis-parola");
  await page.getByRole("button", { name: "Giriş yap" }).click();
  await expect(page.locator(".state-error")).toContainText("E-posta ya da parola hatalı");
  await page.screenshot({ path: "screenshots/login-error.png" });
});

test("çıkış: oturum kapanır ve giriş ekranına dönülür", async ({ page }) => {
  await loginAs(page, "Resepsiyon");
  await logout(page);
  await page.goto("/billing");
  await expect(page.getByRole("button", { name: "Giriş yap" })).toBeVisible();
});
