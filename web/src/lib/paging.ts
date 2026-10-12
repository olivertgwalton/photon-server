// The lists a Pager pages through ask the server for this many at a time.
export const pageSize = 50;

// Where in a paged list an address asks to start.
export function offsetOf(url: URL): number {
	return Math.max(Number(url.searchParams.get("offset")) || 0, 0);
}
