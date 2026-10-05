<script lang="ts">
import { page } from "$app/state";
import { Button } from "#lib/components/ui/button/index.js";
</script>

<svelte:head><title>{page.status} · Photon</title></svelte:head>

<main class="grid min-h-svh place-content-center gap-4 p-6 text-center">
	<p class="label">{page.status}</p>
	<h1 class="title">{page.error?.message}</h1>
	<p><Button href="/" variant="outline">Go home</Button></p>
</main>
