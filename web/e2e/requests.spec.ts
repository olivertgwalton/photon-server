import { expect, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

test("a title the server lacks is asked for from a search, and listed as asked", async ({
	page,
}) => {
	await logIn(page, "/search?q=arrival");
	const lacking = page.getByRole("region", { name: "Not on Den yet" });
	await lacking.getByRole("button", { name: /Arrival/ }).click();
	const dialog = page.getByRole("dialog", { name: "Arrival" });
	await expect(dialog).toContainText(
		"A linguist is asked to talk to visitors.",
	);
	await expectAccessible(page);
	await dialog.getByRole("button", { name: "Ask for it" }).click();
	await expect(page.getByText("Asked for Arrival.")).toBeVisible();

	// Asked for, the profile has requests to look back at; an admin's is
	// approved as it is asked.
	await page.getByRole("link", { name: "Requests" }).first().click();
	await expect(page).toHaveURL("/requests");
	await expect(
		page.getByRole("main").getByRole("listitem").filter({ hasText: "Arrival" }),
	).toContainText("Approved");

	// Asked for again, it says so.
	await page.goto("/search?q=arrival");
	await page.getByRole("button", { name: /Arrival/ }).click();
	await page.getByRole("button", { name: "Ask for it" }).click();
	await expect(
		page.getByText("the title has been asked for already"),
	).toBeVisible();
});
