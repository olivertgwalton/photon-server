import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("an account a profile links at its provider logs in as that profile", async ({
	page,
}) => {
	await page.goto("/settings");
	await expect(page).toHaveURL("/auth/login?to=%2Fsettings");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Continue with Pocket ID" }).click();
	await expect(page).toHaveURL("/auth/login?refused=not_linked&to=%2Fsettings");
	await expect(page.getByRole("alert")).toHaveText(
		/That account isn't linked to a profile here/,
	);

	await logIn(page, "/settings/sign-in");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Link Pocket ID" }).click();
	await expect(
		page.getByText("Your Pocket ID account is linked."),
	).toBeVisible();
	await expect(page).toHaveURL("/settings/sign-in");
	await expect(page.getByText(/Linked as\s+ada/)).toBeVisible();

	await page.getByRole("button", { name: "Ada's profile" }).click();
	await page.getByRole("menuitem", { name: "Log out" }).click();
	await expect(page).toHaveURL("/auth/login");
	await page.getByRole("button", { name: "Continue with Pocket ID" }).click();
	await expect(page).toHaveURL("/");
	await expect(
		page.getByRole("button", { name: "Ada's profile" }),
	).toBeVisible();

	await page.goto("/settings/sign-in");
	await page.getByRole("button", { name: "Unlink Pocket ID" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Unlink" })
		.click();
	await expect(
		page.getByText("Your Pocket ID account is unlinked."),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Link Pocket ID" }),
	).toBeVisible();
});

test("an admin adds a sign-in provider once the server has a public address", async ({
	page,
}) => {
	await logIn(page, "/settings/server/network");
	await page.getByLabel("Public address").fill("https://photon.example.com");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByText(/^Saved/)).toBeVisible();

	await page.goto("/settings/server/sign-in");
	await expectAccessible(page);
	await page.getByRole("button", { name: "Add a provider" }).click();
	const dialog = page.getByRole("dialog");
	await dialog.getByLabel("Slug").fill("authelia");
	await expect(dialog.getByLabel("Redirect URI")).toHaveValue(
		"https://photon.example.com/api/v1/auth/sign-in-providers/authelia/callback",
	);
	await dialog.getByLabel("Name").fill("Authelia");
	await dialog.getByLabel("Issuer").fill("http://auth.example.com");
	await dialog.getByLabel("Client id").fill("photon");
	await dialog.getByLabel("Client secret").fill("secret");
	await dialog.getByLabel("Who signs in").click();
	await page
		.getByRole("option", { name: "Anyone it signs in, each given a profile" })
		.click();
	await dialog.getByLabel("Required group").fill("photon");
	await dialog.getByLabel("Check back").click();
	await page.getByRole("option", { name: "Only as they sign in" }).click();
	await dialog.getByLabel("Every library, including ones added later").click();
	await dialog.getByLabel("Films").check();
	await expectAccessible(page);
	await dialog.getByRole("button", { name: "Add provider" }).click();
	await expect(dialog.getByRole("alert")).toHaveText(/reached over https/);

	await dialog.getByLabel("Issuer").fill("https://auth.example.com");
	await dialog.getByRole("button", { name: "Add provider" }).click();
	await expect(page.getByText("Authelia was saved.")).toBeVisible();
	await expect(dialog).toBeHidden();
	await expect(page.getByRole("heading", { name: "Authelia" })).toBeVisible();
	await expect(page.getByText("Makes profiles")).toBeVisible();
	await expect(page.getByText("Profiles it makes see")).toBeVisible();
	await expect(page.getByText("Only at sign-in")).toBeVisible();
	await expect(
		page.getByRole("definition").filter({ hasText: /^Films$/ }),
	).toBeVisible();

	await page.getByRole("button", { name: "Remove Authelia" }).click();
	await page
		.getByRole("alertdialog")
		.getByRole("button", { name: "Remove provider" })
		.click();
	await expect(page.getByText("Authelia was removed.")).toBeVisible();
	await expect(page.getByText("No sign-in providers yet.")).toBeVisible();
});
