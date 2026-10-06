import { defineConfig, devices } from "@playwright/test";

// The suite drives the built app, served as in production beside a mock of
// the Go server's API (e2e/mock-api.ts): everything the browser does is real,
// and CI needs no Postgres, Valkey, FFmpeg or Go.
// E2E_PORT lets suites run side by side, each with its own mock.
const port = Number(process.env.E2E_PORT ?? 4173);

export default defineConfig({
	testDir: "./e2e",
	testMatch: /\.spec\.ts$/,
	// One worker: the mock keeps one household, and a test may change it.
	workers: 1,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: process.env.CI ? "github" : "list",
	use: { baseURL: `http://localhost:${port}`, trace: "on-first-retry" },
	projects: [
		{ name: "chromium", use: { ...devices["Desktop Chrome"] } },
		{
			name: "phone",
			use: { ...devices["Pixel 7"] },
			testMatch: /shell\.spec\.ts$/,
		},
	],
	webServer: {
		command: `bun run build && PORT=${port} bun e2e/mock-api.ts`,
		url: `http://localhost:${port}/api/v1/server`,
		reuseExistingServer: !process.env.CI,
		timeout: 120_000,
	},
});
