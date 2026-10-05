import { apiURL } from "#lib/server/api.js";
import { proxy } from "#lib/server/proxy.js";
import { SESSION_COOKIE } from "#lib/server/session.js";
import type { RequestHandler } from "./$types";

// Every browser call to the API, any method, on to the Go server with the
// session's token in place of the cookie.
export const fallback: RequestHandler = ({
	request,
	url,
	cookies,
	getClientAddress,
}) =>
	proxy(
		request,
		new URL(url.pathname + url.search, apiURL()),
		cookies.get(SESSION_COOKIE),
		getClientAddress(),
	);
