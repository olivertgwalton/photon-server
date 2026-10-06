import { goto } from "$app/navigation";
import { page } from "$app/state";

// A paged list for one choice, from its first page.
export function narrow(key: string, value: string) {
	const query = new URLSearchParams(page.url.search);
	query.delete("offset");
	if (value === "all") query.delete(key);
	else query.set(key, value);
	goto(`?${query}`, { replace: true, reset: false });
}
