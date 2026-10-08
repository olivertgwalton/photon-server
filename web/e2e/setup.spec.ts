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

test("a new server sends a reader to set it up, then to add a library", async ({
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
	await expect(page).toHaveURL("/settings/server/libraries/new");
	expect(asked).toMatchObject({
		name: "Ada",
		password: "correct horse",
		keep: "cookie",
	});
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
