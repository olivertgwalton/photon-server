import { fail, redirect } from "@sveltejs/kit";
import { problemMessage } from "#lib/api/problem.js";
import { apiClient } from "#lib/server/api.js";
import { servedOverHTTPS } from "#lib/server/origin.js";
import { CLIENT, deviceName } from "#lib/server/device.js";
import {
	forgetSession,
	keepSession,
	LOGIN,
	returnPath,
} from "#lib/server/session.js";
import type { Actions, PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals, url }) => {
	if (locals.session) redirect(303, returnPath(url));
	const server = await locals.api.GET("/api/v1/server").catch(() => null);
	return { server: server?.data ?? null };
};

export const actions: Actions = {
	login: async ({ request, locals, cookies, url, getClientAddress }) => {
		const form = await request.formData();
		const name = String(form.get("name") ?? "");
		const password = String(form.get("password") ?? "");
		if (!name)
			return fail(400, { name, message: "Enter your profile's name." });

		const { data, error } = await locals.api
			.POST("/api/v1/auth/login", {
				body: {
					name,
					password,
					device: deviceName(request.headers.get("user-agent")),
					client: CLIENT,
				},
			})
			.catch(() => ({ data: undefined, error: undefined }));
		if (!data) {
			return fail(error ? error.status : 502, {
				name,
				message: error ? problemMessage(error) : "The server isn't answering.",
			});
		}
		keepSession(cookies, data.token, servedOverHTTPS(request));

		// A household of several is asked who is watching, as Plex and Netflix
		// ask; a household of one goes straight on.
		const to = returnPath(url);
		const profiles = await apiClient(data.token, getClientAddress()).GET(
			"/api/v1/profiles",
		);
		if ((profiles.data?.items.length ?? 0) > 1) {
			redirect(303, `/profiles?to=${encodeURIComponent(to)}`);
		}
		redirect(303, to);
	},

	// Signs this browser out, on the server too, so it leaves the devices list.
	logout: async ({ locals, cookies }) => {
		if (locals.session) {
			await locals.api.POST("/api/v1/auth/logout").catch(() => {});
		}
		forgetSession(cookies);
		redirect(303, LOGIN);
	},
};
