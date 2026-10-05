import type { Config } from "@sveltejs/kit/vite";

// Everything the browser fetches comes from this origin: the API through its
// proxy, artwork included. hls.js plays from blob: media sources and runs its
// worker from a blob:.
export const cspDirectives: NonNullable<Config["csp"]>["directives"] = {
	"default-src": ["self"],
	"script-src": ["self"],
	"worker-src": ["self", "blob:"],
	"style-src": ["self", "unsafe-inline"],
	"img-src": ["self", "data:"],
	"media-src": ["self", "blob:"],
	"font-src": ["self"],
	"connect-src": ["self"],
	"object-src": ["none"],
	"base-uri": ["self"],
	"form-action": ["self"],
	"frame-ancestors": ["none"],
};
