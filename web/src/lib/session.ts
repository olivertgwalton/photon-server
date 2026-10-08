// Where the login page returns to: a path on this site, never another.
export function returnPath(url: {
	searchParams: { get(name: string): string | null };
}): string {
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

export const SETUP = "/setup";
