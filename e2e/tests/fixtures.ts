import { test as base, expect, type Page } from "@playwright/test";

export type Role = "Veteriner" | "Teknisyen" | "Resepsiyon" | "Eczane / depo" | "Hayvan sahibi" | "Ortak sahip";

// Her testte tarayıcı konsol hataları ve API'den gelen 5xx yanıtlar toplanır; test sonunda hiçbiri olmamalı.
export const test = base.extend<{ guard: { errors: string[] } }>({
  guard: [
    async ({ page }, use) => {
      const errors: string[] = [];
      page.on("console", (m) => {
        // Testler bilerek iş kuralı ihlalleri (401/403/409/422) üretir; tarayıcı bu 4xx yanıtları kendiliğinden
        // "Failed to load resource" olarak konsola yazar. Bunlar beklenen davranıştır; 5xx ayrıca yakalanır.
        if (m.type() === "error" && !/status of 4\d\d/.test(m.text())) errors.push(`console: ${m.text()}`);
      });
      page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
      page.on("response", (r) => {
        if (r.status() >= 500) errors.push(`${r.status()} ${r.request().method()} ${r.url()}`);
      });
      await use({ errors });
      expect(errors, "konsol hatası ya da 5xx yanıt olmamalı").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };

export async function loginAs(page: Page, role: Role) {
  await page.goto("/");
  await page.getByRole("button", { name: role, exact: true }).click();
  // Mobilde çıkış düğmesi kapalı menünün içindedir; girişin tamamlandığını sayfa başlığından anlarız.
  await expect(page.locator("main.main h1")).toBeVisible();
}

export async function logout(page: Page) {
  if (!(await page.getByRole("button", { name: "Çıkış yap" }).isVisible())) await page.getByRole("button", { name: "Menü" }).click();
  await page.getByRole("button", { name: "Çıkış yap" }).click();
  await expect(page.getByRole("button", { name: "Giriş yap" })).toBeVisible();
}

// Sayfadaki tüm sorgular bitene kadar bekler ve hata kutusu olmadığını doğrular.
export async function expectLoadedWithoutErrors(page: Page) {
  await page.waitForLoadState("networkidle");
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0);
  const errorBoxes = page.locator(".state-error");
  if (await errorBoxes.count()) {
    throw new Error("Sayfada hata kutusu var: " + (await errorBoxes.allInnerTexts()).join(" | "));
  }
}
