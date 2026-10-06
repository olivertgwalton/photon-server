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
			compilerOptions: { runes: true },
		}),
	],
	server: { proxy: { "/api": api, "/readyz": api } },
});
