// The Go server as the web sees it, for the e2e suite: the built app's files
// and the API on one origin, the answers a real server gave, typed by the same
// schema the app is, so a change to the API that the app would feel fails
// `bun run check` here too.
import type { components } from "../src/lib/api/schema.d.ts";

type Schemas = components["schemas"];

const ada: Schemas["Profile"] = { id: "p-ada", name: "Ada", role: "admin" };
const kids: Schemas["Profile"] = { id: "p-kids", name: "Kids", role: "member" };

const server: Schemas["Info"] = { id: "s-1", name: "Den", version: "v1.0.0" };

const libraries: Schemas["LibraryList"] = {
	items: [
		{ id: "l-films", name: "Films", kind: "movies" },
		{ id: "l-shows", name: "Shows", kind: "shows" },
	],
};

const home: Schemas["Home"] = {
	rows: [
		{
			kind: "continue_watching",
			items: [
				{
					id: "t-ep",
					kind: "episode",
					title: "Pilot",
					added_at: "2026-10-01T20:00:00Z",
					duration_ms: 1_800_000,
					state: { position_ms: 600_000 },
					show: { id: "t-show", title: "Small Show" },
					season_number: 1,
					episode_number: 1,
				},
			],
		},
		{
			kind: "recently_added_films",
			items: [
				{
					id: "t-film",
					kind: "movie",
					title: "Quiet Hours",
					year: 2018,
					added_at: "2026-10-01T20:00:00Z",
				},
			],
		},
	],
};

// A film and two episodes to play, each six seconds of e2e/fixtures/film.mp4.
const version = (id: string): Schemas["VersionPage"] => ({
	id,
	container: "mov,mp4,m4a,3gp,3g2,mj2",
	duration_ms: 6_000,
	size_bytes: 152_341,
	bitrate_kbps: 200,
	parts: 1,
	streams: [
		{
			index: 0,
			kind: "video",
			codec: "h264",
			profile: "High",
			width: 320,
			height: 180,
		},
		{ index: 1, kind: "audio", codec: "aac", language: "en", channels: 2 },
		{ index: 2, kind: "audio", codec: "aac", language: "fr", channels: 2 },
	],
	subtitles: [{ codec: "subrip", language: "en" }],
	chapters: [
		{ start_ms: 0, end_ms: 3_000, title: "Opening" },
		{ start_ms: 3_000, end_ms: 6_000, title: "The Rest" },
	],
	markers: [
		{ kind: "intro", start_ms: 500, end_ms: 3_000, source: "chapter" },
		{ kind: "credits", start_ms: 4_000, end_ms: 6_000, source: "chapter" },
	],
	trickplay: [
		{
			part_id: "part-1",
			offset_ms: 0,
			width: 160,
			height: 90,
			interval_ms: 1_000,
			columns: 3,
			rows: 2,
			thumbnails: 6,
			sheets: 1,
		},
	],
});

const titles: Record<string, Schemas["TitlePage"]> = {
	"t-film": {
		id: "t-film",
		kind: "movie",
		title: "Quiet Hours",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-film")],
		state: { position_ms: 1_000 },
	},
	"t-ep": {
		id: "t-ep",
		kind: "episode",
		title: "Pilot",
		added_at: "2026-10-01T20:00:00Z",
		show: { id: "t-show", title: "Small Show" },
		season_number: 1,
		episode_number: 1,
		versions: [version("v-ep")],
	},
	"t-ep2": {
		id: "t-ep2",
		kind: "episode",
		title: "Second",
		added_at: "2026-10-01T20:00:00Z",
		show: { id: "t-show", title: "Small Show" },
		season_number: 1,
		episode_number: 2,
		versions: [version("v-ep2")],
	},
	"t-busy": {
		id: "t-busy",
		kind: "movie",
		title: "Busy Night",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-busy")],
	},
	"t-odd": {
		id: "t-odd",
		kind: "movie",
		title: "Odd Format",
		added_at: "2026-10-01T20:00:00Z",
		versions: [version("v-odd")],
	},
};

// Where each playback is of which title, and where the reader stopped.
const playing = new Map<string, string>();
const stopped = new Map<string, number>();

// The server's answer to a play: the file as it is, or, under a quality
// limit, HLS of it.
function play(id: string, body: Schemas["Play"]): Response {
	if (id === "t-busy") {
		return Response.json(
			{
				title: "Service Unavailable",
				status: 503,
				code: "transcode_limit",
				detail:
					"the server is already transcoding as many videos at once as it may: 1",
			} satisfies Schemas["Problem"],
			{ status: 503, headers: { "content-type": "application/problem+json" } },
		);
	}
	if (id === "t-odd") {
		return Response.json(
			{
				title: "Unprocessable Entity",
				status: 422,
				code: "no_compatible_stream",
				reasons: ["video_codec_not_supported", "audio_codec_not_supported"],
			} satisfies Schemas["Refusal"],
			{ status: 422, headers: { "content-type": "application/problem+json" } },
		);
	}
	const limited = (body.profile?.max_bitrate_kbps ?? 0) > 0;
	const playback: Schemas["Playback"] = {
		playback_id: crypto.randomUUID(),
		method: limited ? "transcode" : "direct",
		version_id: titles[id]?.versions?.[0]?.id ?? "",
		video: limited
			? {
					stream: 0,
					decision: "transcode",
					codec: "h264",
					width: 320,
					height: 180,
					bitrate_kbps: 300,
				}
			: { stream: 0, decision: "copy" },
		audio: { stream: body.audio_stream ?? 1, decision: "copy" },
		reasons: limited ? ["bitrate_exceeds_limit"] : undefined,
		expires_at: "2026-10-07T00:00:00Z",
	};
	playing.set(playback.playback_id, id);
	if (limited) playback.playlist = "/api/v1/hls/pb/1/sig/main.m3u8";
	else {
		playback.parts = [
			{
				id: "part-1",
				url: "/api/v1/parts/part-1/stream?exp=1&sig=s",
				offset_ms: 0,
				duration_ms: 6_000,
			},
		];
		playback.subtitles = [
			{
				id: "sub-1",
				codec: "subrip",
				language: "en",
				url: "/api/v1/subtitles/sub-1/file?exp=1&sig=s",
			},
		];
	}
	return Response.json(playback);
}

const fixture = (name: string) =>
	new Response(Bun.file(new URL(`fixtures/${name}`, import.meta.url)));

const problem = (status: number, code: Schemas["ProblemCode"], title: string) =>
	Response.json({ title, status, code } satisfies Schemas["Problem"], {
		status,
		headers: { "content-type": "application/problem+json" },
	});

// token → who it is watching as, and whether the profile has a PIN.
const sessions = new Map<string, Schemas["Profile"]>();
let kidsPIN = "";

const cookie = "photon_session";

// The app's page for every path that is not a file, as the server answers it.
async function app(url: URL) {
	const file = Bun.file(`build${url.pathname}`);
	if (url.pathname !== "/" && (await file.exists())) return new Response(file);
	return new Response(Bun.file("build/index.html"));
}

const server_ = Bun.serve({
	port: Number(process.env.PORT ?? 4173),
	async fetch(request) {
		const url = new URL(request.url);
		if (!url.pathname.startsWith("/api/")) return app(url);
		const route = `${request.method} ${url.pathname}`;
		const token =
			request.headers.get("authorization")?.replace("Bearer ", "") ??
			new Bun.CookieMap(request.headers.get("cookie") ?? "").get(cookie) ??
			undefined;
		const me = token ? sessions.get(token) : undefined;

		if (route === "GET /api/v1/server") return Response.json(server);
		if (route === "POST /api/v1/auth/login") {
			const body = (await request.json()) as Schemas["LoginRequest"];
			if (body.name !== "Ada" || body.password !== "correct horse") {
				return problem(401, "invalid_credentials", "Unauthorized");
			}
			const issued = crypto.randomUUID();
			sessions.set(issued, ada);
			if (body.keep !== "cookie") {
				return Response.json({
					token: issued,
					profile: ada,
				} satisfies Schemas["LoginResponse"]);
			}
			return Response.json(
				{ profile: ada } satisfies Schemas["LoginResponse"],
				{
					headers: {
						"set-cookie": `${cookie}=${issued}; Path=/; HttpOnly; SameSite=Lax`,
					},
				},
			);
		}
		if (!me) return problem(401, "unauthenticated", "Unauthorized");

		const title = url.pathname.match(
			/^\/api\/v1\/titles\/([^/]+)(\/play|\/next)?$/,
		);
		if (title?.[1] && titles[title[1]]) {
			const [, id, action] = title;
			if (!action && request.method === "GET") {
				const at = stopped.get(id);
				return Response.json(
					at === undefined
						? titles[id]
						: { ...titles[id], state: { position_ms: at } },
				);
			}
			if (action === "/play" && request.method === "POST") {
				return play(id, (await request.json()) as Schemas["Play"]);
			}
			if (action === "/next" && id === "t-ep") {
				const { versions: _, ...card } = titles["t-ep2"];
				return Response.json(card satisfies Schemas["Card"]);
			}
			if (action === "/next") return problem(404, "not_found", "Not Found");
		}
		const report = route.match(
			/^POST \/api\/v1\/playback\/([^/]+)\/(progress|stop)$/,
		);
		if (report?.[1]) {
			const { position_ms } = (await request.json()) as Schemas["Position"];
			const id = playing.get(report[1]);
			if (id && report[2] === "stop") stopped.set(id, position_ms);
			return Response.json({ reach: "resumable" } satisfies Schemas["Reached"]);
		}
		const hls = url.pathname.match(/^\/api\/v1\/hls\/pb\/1\/sig\/([\w.]+)$/);
		if (hls) return fixture(`hls/${hls[1]}`);
		switch (route) {
			case "GET /api/v1/parts/part-1/stream":
				return fixture("film.mp4");
			case "GET /api/v1/subtitles/sub-1/file":
				return fixture("film.srt");
			case "GET /api/v1/parts/part-1/trickplay/0":
				return fixture("sheet.jpg");
			case "GET /api/v1/me":
				return Response.json(me);
			case "POST /api/v1/auth/logout":
				sessions.delete(token as string);
				return new Response(null, {
					status: 204,
					headers: { "set-cookie": `${cookie}=; Path=/; Max-Age=0` },
				});
			case "GET /api/v1/profiles":
				return Response.json({
					items: [
						{ ...ada, lock: "password" },
						{ ...kids, lock: kidsPIN ? "pin" : "none" },
					],
				} satisfies Schemas["ProfileListingList"]);
			case "PUT /api/v1/session/profile": {
				const body = (await request.json()) as Schemas["Switch"];
				const target = [ada, kids].find((p) => p.id === body.profile_id);
				if (!target) return problem(404, "not_found", "Not Found");
				const locked =
					target === kids ? kidsPIN : target === ada ? "correct horse" : "";
				if (target !== me && locked && body.secret !== locked) {
					return problem(403, "wrong_secret", "Forbidden");
				}
				sessions.set(token as string, target);
				return Response.json(target);
			}
			case "PUT /api/v1/me/pin": {
				const body = (await request.json()) as Schemas["Pin"];
				if (!/^\d{4,6}$/.test(body.pin)) {
					return Response.json(
						{
							title: "Bad Request",
							status: 400,
							code: "invalid_body",
							detail: "a PIN is 4 to 6 digits",
						} satisfies Schemas["Problem"],
						{
							status: 400,
							headers: { "content-type": "application/problem+json" },
						},
					);
				}
				kidsPIN = body.pin;
				return new Response(null, { status: 204 });
			}
			case "DELETE /api/v1/me/pin":
				kidsPIN = "";
				return new Response(null, { status: 204 });
			case "GET /api/v1/libraries":
				return Response.json(libraries);
			case "GET /api/v1/home":
				return Response.json(home);
			case "GET /api/v1/auth/devices":
				return Response.json({
					items: [
						{
							id: "d-this",
							device: "Chrome on macOS",
							client: "Photon Web",
							profile: me.name,
							signed_in_at: "2026-10-01T20:00:00Z",
							last_seen_at: "2026-10-05T20:00:00Z",
							this_device: true,
						},
						{
							id: "d-tv",
							device: "Living Room",
							client: "Photon for tvOS",
							profile: "Kids",
							signed_in_at: "2026-09-01T20:00:00Z",
							last_seen_at: "2026-10-04T20:00:00Z",
							this_device: false,
						},
					],
				} satisfies Schemas["DeviceListingList"]);
			case "DELETE /api/v1/auth/devices/d-tv":
				return new Response(null, { status: 204 });
			case "POST /api/v1/auth/device/approve": {
				const body = (await request.json()) as Schemas["Approval"];
				if (body.user_code.replace(/[- ]/g, "").toUpperCase() !== "BCDFGHJK") {
					return problem(404, "pairing_not_found", "Not Found");
				}
				return Response.json({
					device: "Living Room",
					client: "Photon for tvOS",
				} satisfies Schemas["Device"]);
			}
		}
		return problem(404, "not_found", "Not Found");
	},
});

console.log(`mock server on ${server_.url}`);
