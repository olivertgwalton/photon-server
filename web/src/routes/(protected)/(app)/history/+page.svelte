<script lang="ts">
import TitleRow from "#lib/components/TitleRow.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import { playMethods, timecode } from "#lib/format.js";

let { data } = $props();

const day = new Intl.DateTimeFormat(undefined, { dateStyle: "full" });
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "short" });

// Plays by the day they started on, the newest first.
const days = $derived.by(() => {
	const out: { day: string; plays: typeof data.history.items }[] = [];
	for (const play of data.history.items) {
		const name = day.format(new Date(play.started_at));
		if (out.at(-1)?.day !== name) out.push({ day: name, plays: [] });
		out.at(-1)?.plays.push(play);
	}
	return out;
});
const { offset, total } = $derived(data.history);
</script>

<svelte:head><title>History · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<h1 class="title">History</h1>
	{#each days as group (group.day)}
		<section aria-labelledby="day-{group.day}">
			<h2 id="day-{group.day}" class="label mb-2">{group.day}</h2>
			<ul class="divide-line divide-y">
				{#each group.plays as play (play.id)}
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
	{#if offset > 0 || offset + data.history.items.length < total}
		<nav aria-label="Pages" class="flex gap-2">
			{#if offset > 0}
				<Button
					href="?offset={Math.max(0, offset - data.pageSize)}"
					variant="outline"
				>
					Newer
				</Button>
			{/if}
			{#if offset + data.history.items.length < total}
				<Button href="?offset={offset + data.pageSize}" variant="outline">
					Older
				</Button>
			{/if}
		</nav>
	{/if}
</div>
