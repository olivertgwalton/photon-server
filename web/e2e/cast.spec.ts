import { expect, type Page, test } from "@playwright/test";
import { logIn } from "./helpers";

const time = (page: Page) =>
	page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime);

// Google's sender, stood in for: one Chromecast on the network, which the
// reader picks when asked, and whose session the test ends as the TV would.
const fakeSender = `
	const listeners = new Map();
	const fire = (type, data) => (listeners.get(type) ?? []).forEach((f) => f(data));
	const loaded = [];
	const media = { media: { tracks: [] }, addUpdateListener() {}, editTracksInfo() {}, stop() {} };
	const session = {
		getCastDevice: () => ({ friendlyName: "Living Room TV" }),
		loadMedia: (request) => { loaded.push(request); return Promise.resolve(); },
		getMediaSession: () => media,
	};
	let current = null;
	const context = {
		setOptions() {},
		getCastState: () => "NOT_CONNECTED",
		getCurrentSession: () => current,
		requestSession: () => { window.fakeCast.start(); return Promise.resolve(); },
		addEventListener: (type, f) => listeners.set(type, [...(listeners.get(type) ?? []), f]),
		removeEventListener: (type, f) => listeners.set(type, (listeners.get(type) ?? []).filter((g) => g !== f)),
	};
	class Event { addEventListener() {} removeEventListener() {} playOrPause() {} seek() {} }
	window.cast = { framework: {
		CastContext: { getInstance: () => context },
		CastContextEventType: { CAST_STATE_CHANGED: "caststatechanged", SESSION_STATE_CHANGED: "sessionstatechanged" },
		CastState: { NO_DEVICES_AVAILABLE: "NO_DEVICES_AVAILABLE" },
		SessionState: { SESSION_STARTED: "SESSION_STARTED", SESSION_RESUMED: "SESSION_RESUMED", SESSION_ENDED: "SESSION_ENDED" },
		RemotePlayerEventType: { CURRENT_TIME_CHANGED: "currentTimeChanged", PLAYER_STATE_CHANGED: "playerStateChanged" },
		RemotePlayer: class {},
		RemotePlayerController: Event,
	} };
	window.chrome ??= {};
	chrome.cast = {
		AutoJoinPolicy: { ORIGIN_SCOPED: "origin_scoped" },
		Image: class { constructor(url) { this.url = url; } },
		media: {
			DEFAULT_MEDIA_RECEIVER_APP_ID: "CC1AD845",
			StreamType: { BUFFERED: "BUFFERED" },
			TrackType: { TEXT: "TEXT" },
			HlsSegmentFormat: { FMP4: "fmp4" },
			HlsVideoSegmentFormat: { FMP4: "fmp4" },
			PlayerState: {}, IdleReason: {},
			MediaInfo: class { constructor(contentId, contentType) { Object.assign(this, { contentId, contentType }); } },
			GenericMediaMetadata: class {},
			LoadRequest: class { constructor(media) { this.media = media; } },
			EditTracksInfoRequest: class {},
			StopRequest: class {},
		},
	};
	window.fakeCast = {
		loaded,
		start() { current = session; fire("sessionstatechanged", { session, sessionState: "SESSION_STARTED" }); },
		end() { current = null; fire("sessionstatechanged", { session, sessionState: "SESSION_ENDED" }); },
	};
	window.__onGCastApiAvailable(true);
`;

// A Chromecast reaches the server at a name on the network, not at localhost,
// and the sender runs only in a secure context.
const castHost = `http://photon.test:${process.env.E2E_PORT ?? 4173}`;

test.use({
	// The headless shell takes no insecure origin as secure; Chromium does.
	launchOptions: {
		channel: "chromium",
		args: [
			"--host-resolver-rules=MAP photon.test 127.0.0.1",
			`--unsafely-treat-insecure-origin-as-secure=${castHost}`,
		],
	},
});

test("a Chromecast plays the title from where it was, and the browser takes it back", async ({
	page,
}) => {
	await page.route("https://www.gstatic.com/cv/js/sender/**", (route) =>
		route.fulfill({ contentType: "text/javascript", body: fakeSender }),
	);
	// The server carries a profile that opens no file as it is in HLS.
	const profiles: { containers: string[]; video: { codec: string }[] }[] = [];
	await page.route("**/api/v1/titles/p-film/play", async (route) => {
		const body = route.request().postDataJSON();
		profiles.push(body.profile);
		// Playwright fetches from outside the browser, which alone knows the name.
		const url = route.request().url().replace("photon.test", "localhost");
		const json = await (await route.fetch({ url })).json();
		if (body.profile.containers.length === 0) {
			json.playlist = "/api/v1/hls/pb/1/sig/main.m3u8";
			json.parts = undefined;
		}
		await route.fulfill({ json });
	});
	await logIn(page, `${castHost}/play/p-film`);
	await expect.poll(() => time(page)).toBeGreaterThan(1);

	// The title is opened for the Chromecast where it was, and handed to it
	// at an address it fetches.
	await page.getByRole("button", { name: "Cast" }).click();
	await expect(page.getByText("Playing on Living Room TV")).toBeVisible();
	const loads = () =>
		page.evaluate(
			() =>
				(
					window as unknown as {
						fakeCast: {
							loaded: {
								currentTime: number;
								media: {
									contentId: string;
									contentType: string;
									hlsSegmentFormat: string;
									hlsVideoSegmentFormat: string;
								};
							}[];
						};
					}
				).fakeCast.loaded,
		);
	await expect.poll(async () => (await loads()).length).toBe(1);
	const loaded = await loads();
	expect(profiles.at(-1)?.containers).toEqual([]);
	expect(profiles.at(-1)?.video.map((v) => v.codec)).toEqual(["h264"]);
	expect(loaded[0]?.media.contentId).toBe(
		`${castHost}/api/v1/hls/pb/1/sig/main.m3u8`,
	);
	expect(loaded[0]?.media.contentType).toBe("application/vnd.apple.mpegurl");
	expect(loaded[0]?.media).toMatchObject({
		hlsSegmentFormat: "fmp4",
		hlsVideoSegmentFormat: "fmp4",
	});
	expect(loaded[0]?.currentTime).toBeGreaterThanOrEqual(1);
	expect(await page.locator("video").getAttribute("src")).toBeNull();
	await expect(page.getByRole("button", { name: "Mute" })).toBeHidden();

	// Once the session ends the browser plays it again.
	await page.evaluate(() =>
		(window as unknown as { fakeCast: { end(): void } }).fakeCast.end(),
	);
	await expect(page.getByText("Playing on Living Room TV")).toBeHidden();
	await expect.poll(() => profiles.at(-1)?.containers).toContain("mp4");
	await expect.poll(() => time(page)).toBeGreaterThan(1);
});
