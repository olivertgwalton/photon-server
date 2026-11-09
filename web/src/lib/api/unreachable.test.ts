import { expect, test } from "bun:test";
import createClient from "openapi-fetch";
import { problemMessage } from "./problem.ts";
import { unreachable } from "./unreachable.ts";

type Paths = {
	"/thing": {
		get: { responses: { 200: { content: { "application/json": string } } } };
	};
};

function api(fails: unknown) {
	const c = createClient<Paths>({
		baseUrl: "http://photon.test",
		fetch: () => Promise.reject(fails),
	});
	c.use(unreachable);
	return c;
}

test("a server that never answered is a refusal saying so", async () => {
	const { data, error, response } = await api(
		new TypeError("Failed to fetch"),
	).GET("/thing");
	expect(data).toBeUndefined();
	expect(response.status).toBe(503);
	expect(problemMessage(error)).toBe("The server isn't answering.");
});

test("a call the caller aborted still rejects", async () => {
	const aborted = new DOMException("gone", "AbortError");
	await expect(api(aborted).GET("/thing")).rejects.toBe(aborted);
});
