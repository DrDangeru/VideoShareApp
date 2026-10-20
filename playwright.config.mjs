import { defineConfig, devices } from '@playwright/test';

const baseURL = 'http://127.0.0.1:18765';
const { defaultBrowserType: _defaultBrowserType, ...pixel7ContextOptions } = devices['Pixel 7'];

export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.mjs',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report' }],
  ],
  outputDir: 'test-results/browser-tests',
  use: {
    baseURL,
    contextOptions: { serviceWorkers: 'block' },
    screenshot: 'off',
    trace: 'off',
    video: 'off',
  },
  projects: [
    {
      name: 'desktop-chromium',
      use: {
        browserName: 'chromium',
        viewport: { width: 1280, height: 800 },
        contextOptions: {
          viewport: { width: 1280, height: 800 },
          serviceWorkers: 'block',
        },
      },
    },
    {
      name: 'mobile-chromium',
      use: {
        browserName: 'chromium',
        ...pixel7ContextOptions,
        viewport: { width: 390, height: 844 },
        contextOptions: {
          ...pixel7ContextOptions,
          viewport: { width: 390, height: 844 },
          serviceWorkers: 'block',
        },
      },
    },
  ],
  webServer: {
    command: 'node e2e/start-server.mjs',
    url: `${baseURL}/__e2e/ready`,
    timeout: 120_000,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
    gracefulShutdown: { signal: 'SIGTERM', timeout: 15_000 },
  },
});
