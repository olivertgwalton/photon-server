import adapter from "@sveltejs/adapter-static";
import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// In development the API is another process; the proxy keeps it on this
// origin, as the server keeps it in production, so the session cookie works.
const api = process.env.PHOTON_API_URL ?? "http://localhost:8640";

export default defineConfig({
	plugins: [
		tailwindcss(),
		// Kit 3 takes its options flat, not nested under `kit`. The server serves
		// the build, with its own Content-Security-Policy.
		sveltekit({
			adapter: adapter({ fallback: "index.html", precompress: true }),
			// `await` in a component, a derived or the markup: a page keeps what
			// it shows while the next answer is on its way, then changes at once.
			compilerOptions: { runes: true, experimental: { async: true } },
		}),
	],
	// The ui components merge their variants' classes through cn(), so
	// tailwind-variants need not bring its own copy of tailwind-merge.
	resolve: {
		alias: [
			{ find: /^tailwind-variants$/, replacement: "tailwind-variants/lite" },
		],
	},
	server: { proxy: { "/api": { target: api, ws: true }, "/health": api } },
});
