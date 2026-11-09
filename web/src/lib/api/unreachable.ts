import type { Middleware } from "openapi-fetch";

// A server that never answered is answered as a refusal, so every caller's one
// way of saying a call failed covers it. One the caller aborted still rejects.
export const unreachable: Middleware = {
	onError({ error }) {
		if (error instanceof DOMException && error.name === "AbortError") return;
		return Response.json(
			{ code: "internal", status: 503, title: "The server isn't answering." },
			{
				status: 503,
				headers: { "Content-Type": "application/problem+json" },
			},
		);
	},
};
