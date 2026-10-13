<script lang="ts">
import { SwaggerUIBundle } from "swagger-ui-dist";
import "swagger-ui-dist/swagger-ui.css";

// Swagger UI draws itself light, whatever the app's theme.
const draw = (node: HTMLElement) => {
	SwaggerUIBundle({ domNode: node, url: "/api/v1/openapi.json" });
};
</script>

<svelte:head><title>API · Photon</title></svelte:head>

<div class="min-h-svh bg-white" {@attach draw}></div>
