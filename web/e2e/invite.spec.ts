import { expect, test } from "@playwright/test";
import { expectAccessible } from "./helpers";

test("an invite's link chooses a profile's password and logs it in", async ({
	page,
}) => {
	await page.goto("/auth/invite#someone-elses");
	await expect(
		page.getByRole("heading", { name: "Choose your password" }),
	).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Password", { exact: true }).fill("battery staple");
	await page.getByLabel("Confirm password").fill("battery stable");
	await page.getByRole("button", { name: "Set password" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"The passwords don't match.",
	);

	await page.getByLabel("Confirm password").fill("battery staple");
	await page.getByRole("button", { name: "Set password" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		"no invite is waiting for that code",
	);

	await page.goto("/auth/invite#an-invite");
	await page.getByLabel("Password", { exact: true }).fill("battery staple");
	await page.getByLabel("Confirm password").fill("battery staple");
	const accepted = page.waitForRequest("**/api/v1/auth/invites/redemptions");
	await page.getByRole("button", { name: "Set password" }).click();
	expect((await accepted).postDataJSON()).toMatchObject({
		code: "an-invite",
		password: "battery staple",
		keep: "cookie",
	});
	await expect(page).toHaveURL("/");
});

test("a link without an invite says so", async ({ page }) => {
	await page.goto("/auth/invite");
	await expect(page.getByText("This link has no invite in it.")).toBeVisible();
	await expect(page.getByRole("button", { name: "Set password" })).toHaveCount(
		0,
	);
});
