import { expect, type Page, test } from "@playwright/test";
import { expectAccessible, logIn } from "./helpers";

const video = (page: Page) => page.locator("video");
const time = (page: Page) =>
	video(page).evaluate((v: HTMLVideoElement) => v.currentTime);

async function settings(page: Page, ...path: string[]) {
	await page.getByRole("button", { name: "Settings" }).click();
	for (const name of path.slice(0, -1)) {
		await page.getByRole("menuitem", { name }).click();
	}
	const last = path.at(-1) as string;
	await page
		.getByRole("menuitemradio", { name: last })
		.or(page.getByRole("menuitem", { name: last }))
		.click();
}

test("a film plays as it is from where it was left, and stops where it was", async ({
	page,
}) => {
	const asked = page.waitForRequest("**/api/v1/titles/t-film/play");
	await logIn(page, "/play/t-film");
	const body = (await asked).postDataJSON();
	expect(body.profile.video.map((v: { codec: string }) => v.codec)).toContain(
		"h264",
	);
	expect(body.profile.containers).toContain("mp4");

	await page.waitForRequest("**/api/v1/playback/*/progress");
	await expect.poll(() => time(page)).toBeGreaterThan(2);
	await expect(
		page.getByRole("heading", { name: "Quiet Hours" }),
	).toBeVisible();
	await expectAccessible(page);

	// The stop goes out as the page unloads, where Playwright sees no request,
	// so it is read back as where the title now resumes.
	await page.getByRole("link", { name: "Back to Quiet Hours" }).click();
	await expect
		.poll(async () => {
			const title = await page.request.get("/api/v1/titles/t-film");
			return (await title.json()).state.position_ms;
		})
		.toBeGreaterThan(2_000);
});

test("Skip Intro jumps past the intro", async ({ page }) => {
	await logIn(page, "/play/t-film?t=0.6");
	await page.getByRole("button", { name: "Skip Intro" }).click();
	await expect.poll(() => time(page)).toBeGreaterThanOrEqual(3);
});

test("the keys play, pause, seek and mute as YouTube's do", async ({
	page,
}) => {
	await logIn(page, "/play/t-film?t=0");
	await page.waitForRequest("**/api/v1/playback/*/progress");
	await page.keyboard.press("k");
	await expect(
		page.getByRole("button", { name: "Play", exact: true }),
	).toBeVisible();
	await page.keyboard.press("5");
	await expect.poll(() => time(page)).toBeCloseTo(3, 0);
	await page.keyboard.press("m");
	await expect(page.getByRole("button", { name: "Unmute" })).toBeVisible();
	await page.keyboard.press(" ");
	await expect(page.getByRole("button", { name: "Pause" })).toBeVisible();
});

test("subtitles beside the file show as a track", async ({ page }) => {
	await logIn(page, "/play/t-film?t=1");
	await page.getByRole("button", { name: "Subtitles" }).click();
	await expect
		.poll(() =>
			video(page).evaluate(
				(v: HTMLVideoElement) =>
					(v.textTracks[0]?.activeCues?.[0] as VTTCue | undefined)?.text,
			),
		)
		.toBe("A line to read");
});

test("a lower quality stops the playback and plays the server's HLS", async ({
	page,
}) => {
	await logIn(page, "/play/t-film?t=0");
	await page.waitForRequest("**/api/v1/playback/*/progress");
	const stop = page.waitForRequest("**/api/v1/playback/*/stop");
	const asked = page.waitForRequest("**/api/v1/titles/t-film/play");
	await settings(page, "Quality", "420 kbps");
	await stop;
	expect((await asked).postDataJSON().profile.max_bitrate_kbps).toBe(420);
	await page.waitForRequest("**/api/v1/hls/pb/1/sig/*.m4s");
	await expect.poll(() => time(page)).toBeGreaterThan(0.5);

	await settings(page, "Playback info");
	const info = page.getByRole("dialog", { name: "Playback info" });
	await expect(info).toContainText("Transcode");
	await expect(info).toContainText("The file is above the quality chosen.");
	await expectAccessible(page);
	await page.keyboard.press("Escape");
	await settings(page, "Quality", "Original");
});

test("a busy server and an unplayable file are said plainly", async ({
	page,
}) => {
	await logIn(page, "/play/t-busy");
	await expect(page.getByRole("alert")).toContainText(
		"already transcoding as many videos at once as it may",
	);
	await page.goto("/play/t-odd");
	const alert = page.getByRole("alert");
	await expect(alert).toContainText(
		"This browser doesn't play the video's codec.",
	);
	await expect(alert).toContainText(
		"This browser doesn't play the audio's codec.",
	);
	await expectAccessible(page);
});

test("the next episode is offered in the credits and plays", async ({
	page,
}) => {
	await logIn(page, "/play/t-ep?t=3.5");
	const upNext = page.getByRole("region", { name: "Up next" });
	await expect(upNext).toContainText("S1 E2 · Second");
	await upNext.getByRole("button", { name: "Play now" }).click();
	await expect(page).toHaveURL("/play/t-ep2");
	await expect(
		page.getByRole("heading", { name: "Small Show · S1 E2 · Second" }),
	).toBeVisible();
});

test("scrubbing shows the chapter and the thumbnail under the pointer", async ({
	page,
}) => {
	await logIn(page, "/play/t-film?t=0");
	const seek = page.getByRole("slider", { name: "Seek" });
	const box = await seek.boundingBox();
	if (!box) throw new Error("no seek bar");
	const sheet = page.waitForResponse("**/api/v1/parts/part-1/trickplay/0");
	await page.mouse.move(box.x + box.width * 0.75, box.y + box.height / 2);
	await expect(page.getByText("The Rest")).toBeVisible();
	expect((await sheet).ok()).toBe(true);
});
