import { fail } from "@sveltejs/kit";
import { problemMessage } from "#lib/api/problem.js";
import type { Actions } from "./$types";

export const actions: Actions = {
	default: async ({ request, locals }) => {
		const code = String((await request.formData()).get("code") ?? "");
		const { data, error } = await locals.api.POST(
			"/api/v1/auth/device/approve",
			{
				body: { user_code: code },
			},
		);
		if (error)
			return fail(error.status, { code, message: problemMessage(error) });
		return { linked: data };
	},
};
