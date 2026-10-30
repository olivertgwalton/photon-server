import { withQuery } from "#lib/address.ts";
import { goto } from "$app/navigation";
import { page } from "$app/state";

// A paged list for one choice, from its first page.
export function narrow(key: string, value: string) {
	const to = withQuery(page.url, {
		offset: undefined,
		after: undefined,
		[key]: value === "all" ? undefined : value,
	});
	goto(to, { replace: true, reset: false });
}
