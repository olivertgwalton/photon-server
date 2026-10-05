import type { Handle, ServerInit } from "@sveltejs/kit/hooks";
import { apiClient, apiURL } from "#lib/server/api.js";
import { crossSite } from "#lib/server/origin.js";
import { forgetSession, SESSION_COOKIE } from "#lib/server/session.js";

// A server with nowhere to send the API's calls stops here, not on its first page.
export const init: ServerInit = () => {
	apiURL();
};

// Resolves the cookie into a session for the request. Who may see what is the
// (protected) layout's to decide, and the API's.
export const handle: Handle = async ({ event, resolve }) => {
	const { cookies, url, request } = event;
	if (crossSite(request, url)) {
		return new Response("Cross-site writes are refused", { status: 403 });
	}
	const token = cookies.get(SESSION_COOKIE);
	event.locals.api = apiClient(token, event.getClientAddress());
	// The proxy adds the token itself and leaves the rest to the server.
	if (!token || url.pathname.startsWith("/api/")) return resolve(event);

	// A server that isn't answering leaves the cookie alone: the reader is sent
	// to the login page, which says so, and comes back once it answers.
	const me = await event.locals.api.GET("/api/v1/me").catch(() => null);
	if (me?.data) event.locals.session = { token, profile: me.data };
	else if (me?.response.status === 401) forgetSession(cookies);
	return resolve(event);
};
