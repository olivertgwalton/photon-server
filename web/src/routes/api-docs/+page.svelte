<script lang="ts">
import { createApiReference } from "@scalar/api-reference";
import "@scalar/api-reference/style.css";

// Scalar's default fonts, telemetry and AI agent are on its own hosts, which the app's CSP refuses.
const draw = (node: HTMLElement) => {
	const reference = createApiReference(node, {
		url: "/api/v1/openapi.json",
		withDefaultFonts: false,
		telemetry: false,
		agent: { disabled: true },
		showDeveloperTools: "never",
	});
	return () => reference.destroy();
};
</script>

<svelte:head><title>API · Photon</title></svelte:head>

<div class="min-h-svh" {@attach draw}></div>
