import { need } from "#lib/server/api.js";
import type { PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ locals }) => ({
	home: await need(locals.api.GET("/api/v1/home")),
});
