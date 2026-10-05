import { defineConfig, devices } from "@playwright/test";

// The suite drives the built app, served as in production, against a mock of
// the Go server's API (e2e/mock-api.ts): everything the browser and the Bun
// server do is real, and CI needs no Postgres, Valkey, FFmpeg or Go.
const api = 4180;
const web = 4173;

export default defineConfig({
	testDir: "./e2e",
	testMatch: /\.spec\.ts$/,
	// One worker: the mock keeps one household, and a test may change it.
	workers: 1,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: process.env.CI ? "github" : "list",
	use: { baseURL: `http://localhost:${web}`, trace: "on-first-retry" },
	projects: [
		{ name: "chromium", use: { ...devices["Desktop Chrome"] } },
		{
			name: "phone",
			use: { ...devices["Pixel 7"] },
			testMatch: /shell\.spec\.ts$/,
		},
	],
	webServer: [
		{
			command: `MOCK_API_PORT=${api} bun e2e/mock-api.ts`,
			url: `http://localhost:${api}/api/v1/server`,
			reuseExistingServer: !process.env.CI,
		},
		{
			command: `bun run build && PORT=${web} PHOTON_API_URL=http://localhost:${api} bun ./build/index.js`,
			url: `http://localhost:${web}/auth/login`,
			reuseExistingServer: !process.env.CI,
			timeout: 120_000,
		},
	],
});
