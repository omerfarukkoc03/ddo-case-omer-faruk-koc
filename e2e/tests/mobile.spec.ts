import { test, expect, loginAs, expectLoadedWithoutErrors } from "./fixtures";

// Mobil genişlikte (Pixel 7) sayfalar yatay taşma yapmamalı ve menü düğmeyle açılmalı.
const pages = ["/", "/animals", "/animals/1", "/appointments", "/hospital", "/billing", "/merge"];

test("mobil: resepsiyon sayfaları yatay taşmasız ve menü çalışır", async ({ page }) => {
  await loginAs(page, "Resepsiyon");
  for (const p of pages) {
    await page.goto(p);
    await expectLoadedWithoutErrors(page);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, `${p} yatay taşma (px)`).toBeLessThanOrEqual(1);
    await page.screenshot({ path: `screenshots/mobile${p.replace(/\//g, "_") || "_home"}.png`, fullPage: true });
  }
  await page.getByRole("button", { name: "Menü" }).click();
  await expect(page.locator("aside.sidebar")).toBeVisible();
  await page.locator("aside.sidebar").getByRole("link", { name: "Randevular" }).click();
  await expect(page).toHaveURL(/appointments/);
  await expect(page.locator("aside.sidebar")).toBeHidden();
});

test("mobil: sahip sağlık karnesi", async ({ page }) => {
  await loginAs(page, "Hayvan sahibi");
  await expectLoadedWithoutErrors(page);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "screenshots/mobile_owner.png", fullPage: true });
});
