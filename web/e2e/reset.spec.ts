import { expect, test } from "@playwright/test";
import { expectAccessible } from "./helpers";

test("a forgotten password is reset by the code in the server's log", async ({
	page,
}) => {
	await page.route("**/api/v1/auth/password-resets", (route) =>
		route.fulfill({ json: { node: "den", expires_in_ms: 30 * 60_000 } }),
	);
	let redeemed: unknown;
	await page.route("**/api/v1/auth/password-resets/redemptions", (route) => {
		redeemed = route.request().postDataJSON();
		return redeemed && (redeemed as { code: string }).code === "BCDF-GHJK"
			? route.fulfill({ status: 204 })
			: route.fulfill({
					status: 404,
					json: {
						title: "Not Found",
						status: 404,
						code: "not_found",
						detail: "no password reset is waiting for that code",
					},
				});
	});

	await page.goto("/auth/login");
	await page.getByRole("link", { name: "Forgot your password?" }).click();
	await expect(
		page.getByRole("heading", { name: "Reset your password" }),
	).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Name").fill("Ada");
	await page.getByRole("button", { name: "Get a code" }).click();
	await expect(page.getByText("in the log of den")).toBeVisible();
	await expect(page.getByText("30 minutes")).toBeVisible();

	await page.getByLabel("Code").fill("ZZZZ-ZZZZ");
	await page.getByLabel("New password").fill("battery staple");
	await page.getByLabel("Confirm password").fill("battery staple");
	await page.getByRole("button", { name: "Reset password" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"no password reset is waiting for that code",
	);

	await page.getByLabel("Code").fill("BCDF-GHJK");
	await page.getByRole("button", { name: "Reset password" }).click();
	await expect(page).toHaveURL("/auth/login");
	await expect(page.getByText("Your password is reset.")).toBeVisible();
	expect(redeemed).toEqual({ code: "BCDF-GHJK", password: "battery staple" });
});

test("a reader handed a code goes straight to entering it", async ({
	page,
}) => {
	await page.goto("/auth/reset");
	await page.getByRole("button", { name: "Have a code already?" }).click();
	await expect(page.getByLabel("Code")).toBeVisible();
	await page.getByLabel("Code").fill("BCDF-GHJK");
	await page.getByLabel("New password").fill("battery staple");
	await page.getByLabel("Confirm password").fill("battery stapler");
	await page.getByRole("button", { name: "Reset password" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"The passwords don't match.",
	);
	await expectAccessible(page);
});
