import { PHOTON_API_URL } from "$app/env/private";
import { error, redirect } from "@sveltejs/kit";
import createClient from "openapi-fetch";
import { getRequestEvent } from "$app/server";
import { problemMessage } from "#lib/api/problem.js";
import type { paths } from "#lib/api/schema.js";
import { forgetSession, loginPath } from "./session.js";

// Where the Go server is.
export function apiURL(): string {
	if (!PHOTON_API_URL) throw new Error("PHOTON_API_URL is not set");
	return PHOTON_API_URL;
}

// The API as a reader: their token, and their address so the server's sign-in
// limits and activity log see them rather than this process.
export function apiClient(token: string | undefined, client: string) {
	const headers: Record<string, string> = { "X-Forwarded-For": client };
	if (token) headers.Authorization = `Bearer ${token}`;
	return createClient<paths>({ baseUrl: apiURL(), headers });
}

type Answer<T> = { data?: T; error?: unknown; response: Response };

// The data of a call a page cannot do without. A lapsed session goes to the login
// page and comes back here after; any other refusal is the page's error.
export async function need<T>(call: Promise<Answer<T>>): Promise<T> {
	const { data, error: problem, response } = await call;
	if (response.ok) return data as T;
	if (response.status === 401) {
		const { cookies, url } = getRequestEvent();
		forgetSession(cookies);
		redirect(303, loginPath(url));
	}
	error(response.status, problemMessage(problem));
}
