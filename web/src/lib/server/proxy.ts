// What crosses the proxy, by name. Anything else, the browser's cookie above
// all, stays on its side.
const requestHeaders = [
	"accept",
	"accept-language",
	"content-type",
	"if-modified-since",
	"if-none-match",
	"if-range",
	"last-event-id",
	"range",
	"user-agent",
];

const responseHeaders = [
	"accept-ranges",
	"cache-control",
	"content-disposition",
	"content-length",
	"content-range",
	"content-security-policy",
	"content-type",
	"etag",
	"expires",
	"last-modified",
	"location",
	"retry-after",
	"vary",
	"x-content-type-options",
];

function pick(from: Headers, names: string[]): Headers {
	const out = new Headers();
	for (const name of names) {
		const value = from.get(name);
		if (value !== null) out.set(name, value);
	}
	return out;
}

export function upstreamHeaders(
	incoming: Headers,
	token: string | undefined,
	client: string,
): Headers {
	const out = pick(incoming, requestHeaders);
	if (token) out.set("authorization", `Bearer ${token}`);
	out.set("x-forwarded-for", client);
	return out;
}

export function downstreamHeaders(upstream: Headers): Headers {
	return pick(upstream, responseHeaders);
}

// Forwards a request to the API and streams both bodies, so a segment, a
// download or an event stream passes through as it arrives. A browser that
// goes away cancels the upstream request with it.
export async function proxy(
	request: Request,
	target: URL,
	token: string | undefined,
	client: string,
): Promise<Response> {
	const hasBody = request.method !== "GET" && request.method !== "HEAD";
	let upstream: Response;
	try {
		upstream = await fetch(target, {
			method: request.method,
			headers: upstreamHeaders(request.headers, token, client),
			body: hasBody ? request.body : null,
			redirect: "manual",
			signal: request.signal,
		});
	} catch {
		const problem = {
			title: "Bad Gateway",
			status: 502,
			code: "internal",
			detail: "The server isn't answering.",
		};
		return new Response(JSON.stringify(problem), {
			status: 502,
			headers: { "content-type": "application/problem+json" },
		});
	}
	return new Response(upstream.body, {
		status: upstream.status,
		statusText: upstream.statusText,
		headers: downstreamHeaders(upstream.headers),
	});
}
