import { expect, test } from "bun:test";
import { forget, forgetting, remembering } from "./answers.ts";

const json = (body: string, status = 200) =>
	new Response(body, {
		status,
		headers: { "content-type": "application/json" },
	});

// A fetch answering what it is told to, counting its asks.
function server(answer: () => Response) {
	const asked: string[] = [];
	const fetch = async (input: RequestInfo | URL) => {
		asked.push(new Request(input).url);
		return answer();
	};
	return { asked, fetch };
}

const settle = () => new Promise((r) => setTimeout(r, 10));

test("a page comes back to its last answer, and is drawn again once it changed", async () => {
	forget();
	let body = '{"n":1}';
	const { asked, fetch } = server(() => json(body));
	const swapped: string[] = [];
	const get = remembering(fetch, (url) => swapped.push(url));
	expect(await (await get("http://web.test/api/v1/home")).json()).toEqual({
		n: 1,
	});
	await new Promise((r) => setTimeout(r, 150));
	body = '{"n":2}';
	// The old answer at once, the new one behind.
	expect(await (await get("http://web.test/api/v1/home")).json()).toEqual({
		n: 1,
	});
	await settle();
	expect(swapped).toEqual(["http://web.test/api/v1/home"]);
	expect(await (await get("http://web.test/api/v1/home")).json()).toEqual({
		n: 2,
	});
	await settle();
	// The answer just swapped in is not asked for again.
	expect(asked).toHaveLength(2);
	expect(swapped).toHaveLength(1);
});

test("an answer the server refuses again is forgotten, so the page sees the refusal", async () => {
	forget();
	let status = 200;
	const { fetch } = server(() => json("{}", status));
	const swapped: string[] = [];
	const get = remembering(fetch, (url) => swapped.push(url));
	await get("http://web.test/api/v1/me");
	await new Promise((r) => setTimeout(r, 150));
	status = 401;
	expect((await get("http://web.test/api/v1/me")).ok).toBe(true);
	await settle();
	expect(swapped).toEqual(["http://web.test/api/v1/me"]);
	expect((await get("http://web.test/api/v1/me")).status).toBe(401);
});

test("a write forgets every answer, so the pages load again from the server", async () => {
	forget();
	let n = 0;
	const { asked, fetch } = server(() => json(String(n++)));
	const get = remembering(fetch, () => {});
	await get("http://web.test/api/v1/home");
	await forgetting(fetch)("http://web.test/api/v1/watchlist", {
		method: "PUT",
	});
	expect(await (await get("http://web.test/api/v1/home")).json()).toBe(2);
	expect(asked).toHaveLength(3);
});

test("only a GET's JSON is remembered", async () => {
	forget();
	const { asked, fetch } = server(() => json("{}"));
	const get = remembering(fetch, () => {});
	await get("http://web.test/api/v1/home", { method: "POST" });
	await get("http://web.test/api/v1/home", { method: "POST" });
	expect(asked).toHaveLength(2);
	const page = server(() => new Response("x"));
	const text = remembering(page.fetch, () => {});
	await text("http://web.test/readyz");
	expect(await (await text("http://web.test/readyz")).text()).toBe("x");
	expect(page.asked).toHaveLength(2);
});
