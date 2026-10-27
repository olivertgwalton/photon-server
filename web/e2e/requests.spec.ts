import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("a title the server lacks is asked for from a search, and listed as asked", async ({
	page,
}) => {
	await logIn(page, "/search?q=arrival");
	const lacking = page.getByRole("region", { name: "Not in library" });
	await lacking.getByRole("link", { name: /Arrival/ }).click();
	// It opens as a title's page does, as its provider describes it, with Ask
	// for it in Play's place and nothing else a held title has.
	await expect(page).toHaveURL("/titles/d-arrival");
	await expect(
		page.getByRole("heading", { level: 1, name: "Arrival" }),
	).toBeVisible();
	await expect(page.getByText("Why are they here?")).toBeVisible();
	await expect(page.getByText("It isn't in the library yet.")).toBeVisible();
	const cast = page.getByRole("region", { name: "Cast & crew" });
	await expect(cast.getByRole("link", { name: /Ada Lane/ })).toBeVisible();
	await expect(cast.getByText("Jeremy Renner")).toBeVisible();
	await expect(cast.getByRole("link", { name: /Jeremy Renner/ })).toHaveCount(
		0,
	);
	await expect(page.getByRole("button", { name: "Favourite" })).toHaveCount(0);
	await expectAccessible(page);
	await page.getByRole("button", { name: "Ask for it" }).click();
	await expect(page.getByText("Asked for Arrival.")).toBeVisible();
	await expect(page.getByRole("button", { name: "Asked for" })).toBeDisabled();

	// Asked for, the profile has requests to look back at; an admin's is
	// approved as it is asked.
	await page.getByRole("link", { name: "Requests" }).first().click();
	await expect(page).toHaveURL("/requests");
	await expect(
		page.getByRole("main").getByRole("listitem").filter({ hasText: "Arrival" }),
	).toContainText("Approved");

	// Asked for again, it says so.
	await page.goto("/titles/d-arrival");
	await page.getByRole("button", { name: "Ask for it" }).click();
	await expect(
		page.getByText("the title has been asked for already"),
	).toBeVisible();
});

test("an admin approves what another profile asked for", async ({ page }) => {
	await logIn(page, "/settings/requests");
	// How many wait is drawn beside Requests, and goes once none do.
	const settings = page.getByRole("navigation", { name: "Settings" });
	await expect(
		settings.getByRole("link", { name: "Requests: 1 waiting" }),
	).toBeVisible();
	const dune = page
		.getByRole("main")
		.getByRole("listitem")
		.filter({ hasText: "Dune" });
	await expect(dune).toContainText("asked by Kids");
	await expectAccessible(page);
	await dune.getByRole("button", { name: "Approve Dune" }).click();
	await expect(page.getByText("Dune is approved.")).toBeVisible();
	await expect(
		page.getByRole("listitem").filter({ hasText: "Dune" }),
	).toHaveCount(0);
	await expect(
		settings.getByRole("link", { name: "Requests", exact: true }),
	).toBeVisible();
});
