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

		switch (route) {
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
