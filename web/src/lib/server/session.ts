import type { Cookies } from "@sveltejs/kit";

export const SESSION_COOKIE = "photon_session";

// A device token does not lapse on its own, so the cookie keeps it as long as
// a browser keeps any cookie.
const maxAge = 400 * 24 * 60 * 60;

// Secure wherever the page is served over HTTPS. A server on a home network is
// often reached over plain HTTP by its address, where a Secure cookie would
// never be stored and no one could log in.
export function keepSession(cookies: Cookies, token: string, secure: boolean) {
	cookies.set(SESSION_COOKIE, token, {
		path: "/",
		httpOnly: true,
		sameSite: "lax",
		secure,
		maxAge,
	});
}

export function forgetSession(cookies: Cookies) {
	cookies.delete(SESSION_COOKIE, { path: "/" });
}

// Where the login page returns to: a path on this site, never another.
export function returnPath(url: URL): string {
	const base = "http://here.invalid";
	const to = new URL(url.searchParams.get("to") ?? "/", base);
	return to.origin === base ? to.pathname + to.search + to.hash : "/";
}

export const LOGIN = "/auth/login";

// The login page, saying where to come back to.
export function loginPath(from: URL): string {
	const to = from.pathname + from.search;
	return to === "/" ? LOGIN : `${LOGIN}?to=${encodeURIComponent(to)}`;
}
