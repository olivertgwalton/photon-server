import { fail } from "@sveltejs/kit";
import { problemMessage } from "#lib/api/problem.js";
import { need } from "#lib/server/api.js";
import type { Actions, PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals }) => {
	const [devices, profiles] = await Promise.all([
		need(locals.api.GET("/api/v1/auth/devices")),
		need(locals.api.GET("/api/v1/profiles")),
	]);
	const me = profiles.items.find((p) => p.id === locals.session?.profile.id);
	return { devices: devices.items, lock: me?.lock ?? "none" };
};

export const actions: Actions = {
	setPIN: async ({ request, locals }) => {
		const pin = String((await request.formData()).get("pin") ?? "");
		const { error } = await locals.api.PUT("/api/v1/me/pin", { body: { pin } });
		if (error) return fail(error.status, { pin: problemMessage(error) });
		return { said: "PIN set." };
	},
	clearPIN: async ({ locals }) => {
		const { error } = await locals.api.DELETE("/api/v1/me/pin");
		if (error) return fail(error.status, { pin: problemMessage(error) });
		return { said: "PIN removed." };
	},
	signOutDevice: async ({ request, locals }) => {
		const id = String((await request.formData()).get("id") ?? "");
		const { error } = await locals.api.DELETE("/api/v1/auth/devices/{id}", {
			params: { path: { id } },
		});
		if (error) return fail(error.status, { devices: problemMessage(error) });
		return { said: "That device is signed out." };
	},
};
