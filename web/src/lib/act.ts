import { toast } from "svelte-sonner";
import { goto, refreshAll } from "$app/navigation";
import { problemMessage } from "#lib/api/problem.js";

// A change asked of the API from a page: a refusal says why in a toast, and
// one that worked redraws the page, or goes `to` another where what it changed
// is gone, and says so if there is something to say. Answers whether it worked.
export async function act(
	call: Promise<{ error?: unknown }>,
	said?: string,
	to?: string,
): Promise<boolean> {
	const { error } = await call;
	if (error) {
		toast.error(problemMessage(error));
		return false;
	}
	await (to ? goto(to, { refreshAll: true }) : refreshAll());
	if (said) toast.success(said);
	return true;
}
