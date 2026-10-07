import { error, redirect } from "@sveltejs/kit";
import createClient from "openapi-fetch";
import { navigating } from "$app/state";
import { loginPath } from "#lib/session.js";
import { problemMessage } from "./problem.js";
import type { paths } from "./schema.js";

// The API, on this origin, as the browser's session: the server keeps it in a
// cookie the page's script never sees. A load passes its own `fetch`.
export function client(fetch: typeof globalThis.fetch = globalThis.fetch) {
	return createClient<paths>({ fetch });
}

type Answer<T> = { data?: T; error?: unknown; response: Response };

// The data of a call a page cannot do without. A lapsed session goes to the
// login page and comes back here after; any other refusal is the page's error.
export async function need<T>(call: Promise<Answer<T>>): Promise<T> {
	const { data, error: problem, response } = await call;
	if (response.ok) return data as T;
	if (response.status === 401) {
		// The page being loaded: the one navigated to, or on first load this one.
		redirect(303, loginPath(navigating.to?.url ?? new URL(location.href)));
	}
	error(response.status, problemMessage(problem));
}
