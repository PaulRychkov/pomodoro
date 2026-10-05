import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.UITEST_PORT ?? 18090);
const baseURL = `http://127.0.0.1:${port}`;
// Каталог собранного фронтенда (по умолчанию frontend/dist).
const web = process.env.UITEST_WEB ?? "../frontend/dist";

// Часовой пояс браузера совпадает с -tz сервера: время сессий в списке рисуется
// через toLocaleTimeString, и тесты сверяют его с часами сервера.
const timezoneId = "Asia/Qyzylorda";

export default defineConfig({
  testDir: "./tests",
  // Один сервер с общим состоянием (БД, часы, фейковый planner) — тесты идут строго по очереди.
  workers: 1,
  fullyParallel: false,
  timeout: 45_000,
  expect: { timeout: 7_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : [["list"]],
  use: {
    baseURL,
    timezoneId,
    locale: "ru-RU",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // Chromium берётся из PLAYWRIGHT_BROWSERS_PATH; если нужен конкретный бинарник — UITEST_CHROMIUM.
    launchOptions: process.env.UITEST_CHROMIUM ? { executablePath: process.env.UITEST_CHROMIUM } : {},
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 900 } } },
    {
      name: "mobile",
      use: {
        browserName: "chromium",
        viewport: { width: 390, height: 844 },
        deviceScaleFactor: 3,
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
  webServer: {
    // Фронтенд собирается заранее: npm --prefix ../frontend run build.
    command: `go run ../cmd/uitest-server -addr 127.0.0.1:${port} -web ${web}`,
    url: `${baseURL}/__test/clock`,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
