import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("an admin's settings are one place: their account, then the server", async ({
	page,
}) => {
	await logIn(page, "/settings");
	await expect(page.getByRole("heading", { name: "Ada" })).toBeVisible();
	const nav = page.getByRole("navigation", { name: "Settings" });
	for (const name of ["Profile", "Playback", "Devices", "Link a device"]) {
		await expect(nav.getByRole("link", { name, exact: true })).toBeVisible();
	}
	await expect(nav.getByRole("link", { name: "Libraries" })).toBeVisible();
	await expect(
		page.getByText("An admin's profile is always opened"),
	).toBeVisible();
	await expectAccessible(page);

	await page.getByLabel("Current password").fill("correct horse");
	await page.getByLabel("New password", { exact: true }).fill("battery staple");
	await page.getByLabel("Repeat the new password").fill("battery stapler");
	await page.getByRole("button", { name: "Change password" }).click();
	await expect(
		page.getByText("The new password and its repeat differ."),
	).toBeVisible();
	await page.getByLabel("Repeat the new password").fill("battery staple");
	await page.getByRole("button", { name: "Change password" }).click();
	await expect(page.getByText(/Password changed/)).toBeVisible();

	await nav.getByRole("link", { name: "Devices" }).click();
	await expect(page).toHaveURL("/settings/devices");
	await expect(page.getByText("This browser")).toBeVisible();
	await expectAccessible(page);
	await page.getByRole("button", { name: "Sign out Living Room" }).click();
	await expect(page.getByText("That device is signed out.")).toBeVisible();
});

test("the dashboard's old addresses lead to its new ones", async ({ page }) => {
	await logIn(page, "/admin/libraries");
	await expect(page).toHaveURL("/settings/server/libraries");
	await expect(page.getByRole("heading", { name: "Libraries" })).toBeVisible();
	await page.goto("/admin");
	await expect(page).toHaveURL("/settings/server");
});

test("a member sets a PIN and takes it off again, and sees no server", async ({
	page,
}) => {
	await logIn(page);
	await page.goto("/profiles?to=%2Fsettings");
	await page.getByRole("button", { name: "Kids" }).click();
	await expect(page).toHaveURL("/settings");
	await expect(
		page.getByRole("navigation", { name: "Settings" }).getByRole("link", {
			name: "Libraries",
		}),
	).toHaveCount(0);

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

test("playback settings are the profile's, kept by the server", async ({
	page,
}) => {
	await logIn(page, "/settings/playback");
	await expectAccessible(page);
	await page.getByLabel("Subtitles", { exact: true }).click();
	await page.getByRole("option", { name: "Always" }).click();
	await expect(page.getByText("Saved for every device.")).toBeVisible();
	await page.getByRole("switch", { name: "Play the next episode" }).click();
	await page.reload();
	await expect(page.getByLabel("Subtitles", { exact: true })).toHaveText(
		"Always",
	);
	await expect(
		page.getByRole("switch", { name: "Play the next episode" }),
	).toHaveAttribute("aria-checked", "false");
});

test("a TV is linked by the code it shows", async ({ page }) => {
	await logIn(page, "/settings/link");
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

test("the address a TV shows fills in its code", async ({ page }) => {
	await logIn(page, "/link?code=BCDF-GHJK");
	await expect(page).toHaveURL("/settings/link?code=BCDF-GHJK");
	await expect(page.getByLabel("Code")).toHaveValue("BCDF-GHJK");
	await page.getByRole("button", { name: "Link" }).click();
	await expect(page.getByRole("status")).toHaveText(
		"Living Room (Photon for tvOS) is signed in.",
	);
});
