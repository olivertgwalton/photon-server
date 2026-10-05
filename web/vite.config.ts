import adapter from "@sveltejs/adapter-bun";
import { sveltekit } from "@sveltejs/kit/vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { cspDirectives } from "./src/lib/csp.ts";

export default defineConfig({
	plugins: [
		tailwindcss(),
		// Kit 3 takes its options flat, not nested under `kit`.
		sveltekit({
			adapter: adapter(),
			csp: { mode: "auto", directives: cspDirectives },
			// Kit's own check compares the scheme too, which adapter-bun can only
			// guess; hooks.server.ts makes the same check by host instead.
			csrf: { trustedOrigins: ["*"] },
			compilerOptions: { runes: true },
		}),
	],
});
