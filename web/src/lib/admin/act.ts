import { toast } from "svelte-sonner";
import { goto, invalidateAll } from "$app/navigation";
import { problemMessage } from "#lib/api/problem.js";

// A change asked of the API from a page: a refusal says why in a toast, and
// one that worked says so and has the page load again, or goes `to` another
// where what it changed is gone. Answers whether it worked.
export async function act(
	call: Promise<{ error?: unknown }>,
	said: string,
	to?: string,
): Promise<boolean> {
	const { error } = await call;
	if (error) {
		toast.error(problemMessage(error));
		return false;
	}
	await (to ? goto(to, { invalidateAll: true }) : invalidateAll());
	toast.success(said);
	return true;
}
