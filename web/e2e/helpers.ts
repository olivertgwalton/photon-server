import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

export async function logIn(page: Page, to = "/") {
	await page.goto(to);
	await page.getByLabel("Name").fill("Ada");
	await page.getByLabel("Password").fill("correct horse");
	await page.getByRole("button", { name: "Log in" }).click();
	// Two profiles in the household, so the picker asks who is watching.
	await page.getByRole("button", { name: "Ada" }).click();
}

export async function expectAccessible(page: Page) {
	const results = await new AxeBuilder({ page })
		.withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
		.analyze();
	expect(
		results.violations,
		JSON.stringify(results.violations, null, 2),
	).toEqual([]);
}
