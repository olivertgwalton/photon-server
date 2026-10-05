import { fail, redirect } from "@sveltejs/kit";
import { problemMessage } from "#lib/api/problem.js";
import { need } from "#lib/server/api.js";
import { returnPath } from "#lib/server/session.js";
import type { Actions, PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals, url }) => {
	const { items } = await need(locals.api.GET("/api/v1/profiles"));
	return {
		profiles: items,
		current: locals.session?.profile.id,
		// A locked profile, once chosen, asks for its PIN or password.
		chosen: items.find((p) => p.id === url.searchParams.get("profile")),
		to: returnPath(url),
	};
};

export const actions: Actions = {
	default: async ({ request, locals, url }) => {
		const form = await request.formData();
		const profileID = String(form.get("profile_id") ?? "");
		const secret = String(form.get("secret") ?? "");
		const { error } = await locals.api.PUT("/api/v1/session/profile", {
			body: secret
				? { profile_id: profileID, secret }
				: { profile_id: profileID },
		});
		if (error) return fail(error.status, { message: problemMessage(error) });
		redirect(303, returnPath(url));
	},
};
