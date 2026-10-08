// The last answer to each GET a page loads, so a page the reader comes back
// to is drawn from it at once while it is asked for again behind, and drawn
// again where the answer changed, as Plex's web app does. The server's ETags
// make the second ask cheap.
type Answer = { body: string; type: string; at: number };

export type Fetch = (
	input: RequestInfo | URL,
	init?: RequestInit,
) => Promise<Response>;

const answers = new Map<string, Answer>();
// Keys being asked for again now.
const asking = new Set<string>();
// How many answers are kept: a few dozen pages' worth.
const kept = 200;
// An answer this fresh is not asked for again: the page is being drawn again
// with the answer just swapped in.
const justNow = 100;

// Wraps fetch so a GET answered before is answered from memory, and asked of
// fetch again behind; swap is told the address of each answer that changed,
// or could not be had again, to load the page again.
export function remembering(fetch: Fetch, swap: (url: string) => void): Fetch {
	return async (input, init) => {
		const request = new Request(input, init);
		if (request.method !== "GET") return written(fetch, request);
		const url = request.url;
		const last = answers.get(url);
		if (!last) return keep(url, await fetch(request));
		if (Date.now() - last.at >= justNow && !asking.has(url)) {
			asking.add(url);
			refresh(fetch, request, last, swap).finally(() => asking.delete(url));
		}
		return new Response(last.body, { headers: { "content-type": last.type } });
	};
}

async function refresh(
	fetch: Fetch,
	request: Request,
	last: Answer,
	swap: (url: string) => void,
) {
	let fresh: Answer | undefined;
	try {
		await keep(request.url, await fetch(request));
		fresh = answers.get(request.url);
	} catch {
		// Forgotten below: the page asks the server itself and shows what it says.
	}
	if (!fresh) answers.delete(request.url);
	if (fresh?.body !== last.body) swap(request.url);
}

// Keeps a JSON answer the server gave, and answers response as it was.
async function keep(url: string, response: Response): Promise<Response> {
	const type = response.headers.get("content-type") ?? "";
	if (!response.ok || !type.includes("json")) {
		answers.delete(url);
		return response;
	}
	const body = await response.clone().text();
	answers.delete(url);
	answers.set(url, { body, type, at: Date.now() });
	for (const key of answers.keys()) {
		if (answers.size <= kept) break;
		answers.delete(key);
	}
	return response;
}

// Wraps fetch so a write forgets every answer once it is done: what it changed
// is unknown, so the pages load again from the server.
export function forgetting(fetch: Fetch): Fetch {
	return (input, init) => {
		const request = new Request(input, init);
		return request.method === "GET" ? fetch(request) : written(fetch, request);
	};
}

async function written(fetch: Fetch, request: Request): Promise<Response> {
	try {
		return await fetch(request);
	} finally {
		forget();
	}
}

// Forgets every answer: they were another profile's, or a session's that ended.
export function forget() {
	answers.clear();
}
