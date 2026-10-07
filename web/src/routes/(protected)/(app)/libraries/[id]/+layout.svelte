<script lang="ts">
import { page } from "$app/state";

let { data, children } = $props();

const base = $derived(`/libraries/${data.library.id}`);
const tabs = $derived(
	[
		{ href: base, label: "Titles" },
		data.collections && {
			href: `${base}/collections`,
			label: "Collections",
		},
		data.facets.genres.length && { href: `${base}/genres`, label: "Genres" },
		data.facets.studios.length && {
			href: `${base}/studios`,
			label: data.library.kind === "shows" ? "Networks" : "Studios",
		},
	].filter((t) => !!t),
);
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
	{@render children()}
</div>
