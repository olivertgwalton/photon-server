import { defineEnvVars } from "@sveltejs/kit/env";

export const variables = defineEnvVars({
	PHOTON_API_URL: {
		description: "Where the Go server is, as this process reaches it.",
		// Optional here because the build reads this file too; the server
		// refuses to start without it (see init in hooks.server.ts).
		schema: (value) => (value ? new URL(value).href : undefined),
	},
});
