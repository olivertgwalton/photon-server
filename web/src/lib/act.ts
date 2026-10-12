import { toast } from "svelte-sonner";
import { problemMessage } from "#lib/api/problem.js";
import { goto, refreshAll } from "$app/navigation";

// A change asked of the API from a page: a refusal says why in a toast, and
// one that worked redraws the page, or goes `to` another where what it changed
// is gone, and says so if there is something to say. Answers what the API
// answered when it worked, boxed since a change that answers nothing worked too.
export async function act<T>(
	call: Promise<{ data?: T; error?: unknown }>,
	said?: string,
	to?: string | ((data: T) => string),
): Promise<{ data: T } | undefined> {
	const { data, error } = await call;
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	await (to
		? goto(typeof to === "string" ? to : to(data as T), { refreshAll: true })
		: refreshAll());
	if (said) toast.success(said);
	return { data: data as T };
}
