// A page's address with some of what it asks changed: each name set to its
// value, or left out where the value is undefined or empty.
export function withQuery(
	url: { href: string },
	changes: Record<string, string | undefined>,
): string {
	const to = new URL(url.href);
	for (const [name, value] of Object.entries(changes)) {
		if (value) to.searchParams.set(name, value);
		else to.searchParams.delete(name);
	}
	return to.pathname + to.search;
}
