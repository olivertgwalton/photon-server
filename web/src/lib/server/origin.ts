// adapter-bun assumes TLS ends at a proxy in front of it, so every request's
// URL says https, even on a home network reached over plain HTTP. What the
// page was really served over is in the Origin a browser sends with every
// POST, so both the forgery check and the cookie's Secure flag read that.

// A browser's write from another site. Only the host is compared, as the
// scheme here is the adapter's guess.
export function crossSite(request: Request, url: URL): boolean {
	if (["GET", "HEAD", "OPTIONS"].includes(request.method)) return false;
	const origin = request.headers.get("origin");
	if (origin === null) return false;
	return !URL.canParse(origin) || new URL(origin).host !== url.host;
}

export function servedOverHTTPS(request: Request): boolean {
	return request.headers.get("origin")?.startsWith("https://") ?? false;
}
