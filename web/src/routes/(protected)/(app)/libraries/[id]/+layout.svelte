<script lang="ts">
import { page } from "$app/state";
import { Progress } from "#lib/components/ui/progress/index.js";
import { live } from "#lib/live.svelte.js";

let { data, children } = $props();

const base = $derived(`/libraries/${data.library.id}`);
const tabs = $derived(
	[
		{ href: base, label: "Titles" },
		{ href: `${base}/collections`, label: "Collections" },
		data.facets.genres.length && { href: `${base}/genres`, label: "Genres" },
		data.facets.studios.length && {
			href: `${base}/studios`,
			label: data.library.kind === "shows" ? "Networks" : "Studios",
		},
	].filter((t) => !!t),
);
const scan = $derived(live.scans[data.library.id]);
</script>

<div class="grid gap-4">
	<h1 class="title">{data.library.name}</h1>
	<nav aria-label="{data.library.name} views" class="border-line border-b">
		<ul class="-mb-px flex gap-1 overflow-x-auto">
			{#each tabs as tab (tab.href)}
				{@const here = page.url.pathname === tab.href}
				<li>
					<a
						href={tab.href}
						aria-current={here ? "page" : undefined}
						class={[
							"block border-b-2 px-3 py-2 text-sm font-semibold whitespace-nowrap",
							here
								? "border-ink text-ink"
								: "text-ink-3 hover:text-ink border-transparent",
						]}
					>
						{tab.label}
					</a>
				</li>
			{/each}
		</ul>
	</nav>
	{#if scan}
		<div role="status" class="bg-raise grid gap-2 rounded-lg px-4 py-3">
			<p class="text-ink text-sm font-semibold">
				Being scanned{scan.known
					? `: ${scan.done} of ${scan.known} files`
					: "…"}
			</p>
			{#if scan.known}
				<Progress
					value={scan.done}
					max={scan.known}
					aria-label="Scan progress"
				/>
			{/if}
		</div>
	{/if}
	{@render children()}
</div>
