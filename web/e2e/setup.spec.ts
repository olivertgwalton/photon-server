import { expect, type Page, test } from "@playwright/test";
import { expectAccessible } from "./helpers";

// A new server, as the server answers it to a reader in state.
async function newServer(page: Page, state: "open" | "local_only") {
	await page.route("**/api/v1/setup", (route) =>
		route.request().method() === "GET"
			? route.fulfill({ json: { state } })
			: route.fallback(),
	);
}

test("a new server is set up step by step: its admin, its name, its libraries, and remote access", async ({
	page,
}) => {
	await newServer(page, "open");
	let asked: unknown;
	await page.route("**/api/v1/setup", async (route) => {
		if (route.request().method() !== "POST") return route.fallback();
		asked = route.request().postDataJSON();
		// The real server sets the session cookie; here the mock's sign-in does.
		await page.request.post("/api/v1/auth/login", {
			data: {
				method: "password",
				name: "Ada",
				password: "correct horse",
				device: "d",
				client: "c",
				keep: "cookie",
			},
		});
		await route.fulfill({
			json: { profile: { id: "p-ada", name: "Ada", role: "admin" } },
		});
	});

	await page.goto("/");
	await expect(page).toHaveURL("/setup");
	await expect(page.getByRole("heading", { name: "Set up Den" })).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Name").fill("Ada");
	await page.getByLabel("Password", { exact: true }).fill("correct horse");
	await page.getByLabel("Confirm password").fill("correct hose");
	await page.getByRole("button", { name: "Create admin" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"The passwords don't match.",
	);
	expect(asked).toBeUndefined();

	await page.getByLabel("Confirm password").fill("correct horse");
	await page.getByRole("button", { name: "Create admin" }).click();
	await expect(page).toHaveURL("/setup/server");
	expect(asked).toMatchObject({
		name: "Ada",
		password: "correct horse",
		keep: "cookie",
	});

	// Signed in as the admin, the rest is the admin's own settings.
	await expect(
		page.getByRole("heading", { name: "Name the server" }),
	).toBeVisible();
	await expect(page.getByLabel("Name")).toHaveValue("Den");
	await expectAccessible(page);
	const named = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/api/v1/admin/server"),
	);
	await page.getByRole("button", { name: "Next" }).click();
	expect((await named).postDataJSON()).toEqual({
		name: "Den",
		metadata_language: "en-GB",
		certification_country: "GB",
	});

	await expect(page).toHaveURL("/setup/libraries");
	await expect(
		page.getByRole("list", { name: "Libraries added" }),
	).toContainText("Films");
	await expectAccessible(page);
	await page.getByRole("link", { name: "Next" }).click();

	await expect(page).toHaveURL("/setup/remote");
	await expectAccessible(page);
	const before = await (await page.request.get("/api/v1/admin/network")).json();
	await page.getByLabel("Public address").fill("https://photon.example.com");
	await page.getByLabel("Remote streaming limit (Mbps)").fill("8");
	const reached = page.waitForRequest(
		(r) => r.method() === "PUT" && r.url().endsWith("/api/v1/admin/network"),
	);
	await page.getByRole("button", { name: "Next" }).click();
	// The rest of the network is left as it was.
	expect((await reached).postDataJSON()).toEqual({
		...before,
		public_url: "https://photon.example.com",
		remote_max_bitrate_kbps: 8000,
	});
	await page.request.put("/api/v1/admin/network", { data: before });

	await expect(page).toHaveURL("/setup/done");
	await expect(
		page.getByRole("heading", { name: "Den is set up" }),
	).toBeVisible();
	await page.getByRole("link", { name: "Start watching" }).click();
	await expect(page).not.toHaveURL(/setup/);
});

test("a new server reached from elsewhere says to set it up from home", async ({
	page,
}) => {
	await newServer(page, "local_only");
	await page.goto("/auth/login");
	await expect(page).toHaveURL("/setup");
	await expect(page.getByText("set up from its own network")).toBeVisible();
	await expect(page.getByRole("button", { name: "Create admin" })).toHaveCount(
		0,
	);
	await expectAccessible(page);
});

test("a server set up sends the setup page to log in", async ({ page }) => {
	await page.goto("/setup");
	await expect(page).toHaveURL("/auth/login");
});
