import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("an admin's settings list the devices and sign one out", async ({
	page,
}) => {
	await logIn(page, "/settings");
	await expect(
		page.getByText("An admin's profile is always opened"),
	).toBeVisible();
	await expect(page.getByText("This browser")).toBeVisible();
	await expectAccessible(page);

	await page.getByRole("button", { name: "Sign out Living Room" }).click();
	await expect(page.getByText("That device is signed out.")).toBeVisible();
});

test("a member sets a PIN and takes it off again", async ({ page }) => {
	await logIn(page);
	await page.goto("/profiles?to=%2Fsettings");
	await page.getByRole("button", { name: "Kids" }).click();
	await expect(page).toHaveURL("/settings");

	await page.getByLabel("PIN", { exact: true }).fill("2468");
	await page.getByRole("button", { name: "Set PIN" }).click();
	await expect(page.getByText("PIN set.")).toBeVisible();
	await expect(
		page.getByText("Switching to this profile asks for its PIN."),
	).toBeVisible();

	await page.getByRole("button", { name: "Remove PIN" }).click();
	await expect(page.getByText("PIN removed.")).toBeVisible();
	await expect(page.getByRole("button", { name: "Set PIN" })).toBeVisible();
});

test("a TV is linked by the code it shows", async ({ page }) => {
	await logIn(page, "/link");
	await expectAccessible(page);
	const code = page.getByLabel("Code");
	await code.fill("ZZZZ-ZZZZ");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("alert")).toHaveText(
		/No device is showing that code/,
	);

	await code.fill("bcdf-ghjk");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("status")).toHaveText(
		"Living Room (Photon for tvOS) is signed in.",
	);
});

test("a TV's link fills in the code it shows", async ({ page }) => {
	await logIn(page, "/link?code=BCDF-GHJK");
	await expect(page.getByLabel("Code")).toHaveValue("BCDF-GHJK");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("status")).toHaveText(
		"Living Room (Photon for tvOS) is signed in.",
	);
});
