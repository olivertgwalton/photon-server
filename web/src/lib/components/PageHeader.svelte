<script lang="ts">
import type { Snippet } from "svelte";

// A settings page's name, what it is for in a line, and what can be done from
// it, laid out the same on every page.
let {
	title,
	head = title,
	description,
	actions,
}: {
	title: string;
	// The tab's name, where it says more than the heading.
	head?: string;
	description?: string;
	actions?: Snippet;
} = $props();
</script>

<svelte:head><title>{head} · Settings · Photon</title></svelte:head>

<header class="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
	<div class="grid max-w-2xl gap-1">
		<h1 class="title">{title}</h1>
		{#if description}
			<p class="text-ink-2 text-sm [overflow-wrap:anywhere]">{description}</p>
		{/if}
	</div>
	{#if actions}
		<div class="flex flex-wrap gap-2">{@render actions()}</div>
	{/if}
</header>
