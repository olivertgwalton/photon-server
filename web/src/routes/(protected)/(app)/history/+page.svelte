<script lang="ts">
import Pager from "#lib/components/Pager.svelte";
import TitleRow from "#lib/components/TitleRow.svelte";
import { playMethods, timecode } from "#lib/format.js";

let { data } = $props();

const day = new Intl.DateTimeFormat(undefined, { dateStyle: "full" });
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "short" });

// Plays by the day they started on, the newest first.
const days = $derived(
	Map.groupBy(data.history.items, (play) =>
		day.format(new Date(play.started_at)),
	),
);
const { offset, total } = $derived(data.history);
</script>

<svelte:head><title>History · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<h1 class="title">History</h1>
	{#each days as [name, plays] (name)}
		<section aria-labelledby="day-{name}">
			<h2 id="day-{name}" class="label mb-2">{name}</h2>
			<ul class="divide-line divide-y">
				{#each plays as play (play.id)}
					<li class="py-2">
						<TitleRow
							card={play.title}
							detail="{time.format(
								new Date(play.started_at),
							)} · stopped at {timecode(play.position_ms)} · {playMethods[
								play.method
							]}"
						/>
					</li>
				{/each}
			</ul>
		</section>
	{:else}
		<p class="text-ink-3">Nothing played yet.</p>
	{/each}
	<Pager {offset} limit={data.pageSize} {total} />
</div>
