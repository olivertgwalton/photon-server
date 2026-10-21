// The lists a Pager pages through ask the server for this many at a time.
export const pageSize = 50;

// Where in a paged list an address asks to start.
export function offsetOf(url: URL): number {
	return Math.max(Number(url.searchParams.get("offset")) || 0, 0);
}

// Where in a list paged on from the page before an address asks to start: the
// page before's next, or the first page for none.
export function afterOf(url: URL): string | undefined {
	return url.searchParams.get("after") || undefined;
}
