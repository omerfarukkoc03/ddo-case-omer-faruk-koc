import { defineConfig, devices } from "@playwright/test";

// Arayüz uçtan uca testleri: çalışan sisteme (make up) gerçek tarayıcıyla bağlanır.
export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "report" }]],
  use: {
    baseURL: process.env.BASE_URL ?? "http://localhost:3000",
    locale: "tr-TR",
    timezoneId: "Europe/Istanbul",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    // Yerelde kurulu Google Chrome kullanılabilir (PW_CHANNEL=chrome); CI Playwright'ın kendi Chromium'unu kullanır.
    channel: process.env.PW_CHANNEL || undefined,
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } }, testIgnore: /mobile\.spec\.ts/ },
    { name: "mobile", use: { ...devices["Pixel 7"] }, testMatch: /mobile\.spec\.ts/ },
  ],
});
