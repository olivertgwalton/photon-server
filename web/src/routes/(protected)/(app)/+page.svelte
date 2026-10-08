<script lang="ts">
import Rail from "#lib/components/Rail.svelte";
import { Skeleton } from "#lib/components/ui/skeleton/index.js";
import { homeRows, rail } from "#lib/rows.js";
import { settled } from "#lib/settled.svelte.js";

let { data } = $props();
const home = settled(() => data.home);
// Rails of blank cards, as wide as Rail draws its posters, until the first answer.
const blanks = Array.from({ length: 8 }, (_, n) => n);
</script>

<svelte:head><title>Home · Photon</title></svelte:head>

<h1 class="sr-only">Home</h1>
{#if home.failed}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">{home.failed}</p>
	</div>
{:else if !home.value}
	<div class="grid gap-8" aria-hidden="true">
		{#each blanks.slice(0, 3) as row (row)}
			<div>
				<Skeleton class="mb-3 h-5 w-40" />
				<ul class="-mx-3 flex gap-3 overflow-hidden px-3 py-2 sm:-mx-6 sm:px-6">
					{#each blanks as card (card)}
						<li class="w-32 shrink-0 sm:w-36 lg:w-40 2xl:w-48">
							<Skeleton class="aspect-[2/3] rounded-xl" />
						</li>
					{/each}
				</ul>
			</div>
		{/each}
	</div>
{:else if home.value.rows.length}
	<div class="grid gap-8">
		{#each home.value.rows.map((row) => ({
			row,
			...rail(row),
		})) as { row, key, href } (key)}
			<Rail
				title={row.title}
				cards={row.items}
				shape={homeRows[row.kind].shape}
				{href}
			/>
		{/each}
	</div>
{:else}
	<div class="grid min-h-[50svh] place-content-center gap-2 text-center">
		<p class="heading">Nothing to watch yet</p>
		<p class="text-sm">Titles appear here once a library has been scanned.</p>
	</div>
{/if}
