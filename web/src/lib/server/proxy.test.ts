import { afterAll, expect, test } from "bun:test";
import { downstreamHeaders, proxy, upstreamHeaders } from "./proxy.ts";

test("the cookie stays here and the token goes in its place", () => {
	const out = upstreamHeaders(
		new Headers({
			cookie: "photon_session=secret",
			authorization: "Bearer forged",
			range: "bytes=0-99",
			"if-none-match": '"abc"',
			"x-forwarded-for": "6.6.6.6",
			host: "photon.example",
		}),
		"token",
		"192.0.2.7",
	);
	expect(out.get("cookie")).toBeNull();
	expect(out.get("host")).toBeNull();
	expect(out.get("authorization")).toBe("Bearer token");
	expect(out.get("x-forwarded-for")).toBe("192.0.2.7");
	expect(out.get("range")).toBe("bytes=0-99");
	expect(out.get("if-none-match")).toBe('"abc"');
});

test("a browser signed out sends no token at all", () => {
	const out = upstreamHeaders(
		new Headers({ authorization: "Bearer forged" }),
		undefined,
		"192.0.2.7",
	);
	expect(out.get("authorization")).toBeNull();
});

test("what a player needs comes back, and nothing that sets state here", () => {
	const out = downstreamHeaders(
		new Headers({
			"content-type": "video/mp4",
			"content-range": "bytes 0-99/1000",
			"accept-ranges": "bytes",
			"cache-control": "public, max-age=31536000, immutable",
			"set-cookie": "a=b",
			connection: "keep-alive",
		}),
	);
	expect(out.get("content-type")).toBe("video/mp4");
	expect(out.get("content-range")).toBe("bytes 0-99/1000");
	expect(out.get("accept-ranges")).toBe("bytes");
	expect(out.get("cache-control")).toBe("public, max-age=31536000, immutable");
	expect(out.get("set-cookie")).toBeNull();
	expect(out.get("connection")).toBeNull();
});

const upstream = Bun.serve({
	port: 0,
	async fetch(request) {
		return new Response(
			JSON.stringify({
				method: request.method,
				path: new URL(request.url).pathname,
				authorization: request.headers.get("authorization"),
				body: await request.text(),
			}),
			{ status: 201, headers: { "content-type": "application/json" } },
		);
	},
});
afterAll(() => upstream.stop());

test("a request goes through with its body, status and type", async () => {
	const response = await proxy(
		new Request("http://web.test/api/v1/playlists", {
			method: "POST",
			body: '{"name":"Sunday"}',
			headers: { "content-type": "application/json", cookie: "x=y" },
		}),
		new URL("/api/v1/playlists", upstream.url),
		"token",
		"192.0.2.7",
	);
	expect(response.status).toBe(201);
	expect(response.headers.get("content-type")).toBe("application/json");
	expect(await response.json()).toEqual({
		method: "POST",
		path: "/api/v1/playlists",
		authorization: "Bearer token",
		body: '{"name":"Sunday"}',
	});
});

test("a server that isn't there is a problem the page can show", async () => {
	const response = await proxy(
		new Request("http://web.test/api/v1/me"),
		new URL("http://127.0.0.1:1/api/v1/me"),
		"token",
		"192.0.2.7",
	);
	expect(response.status).toBe(502);
	expect(response.headers.get("content-type")).toBe("application/problem+json");
	expect((await response.json()).detail).toBe("The server isn't answering.");
});
