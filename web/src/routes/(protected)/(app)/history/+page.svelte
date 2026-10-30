<script lang="ts">
import CursorPager from "#lib/components/CursorPager.svelte";
import TitleRow from "#lib/components/TitleRow.svelte";
import { Button } from "#lib/components/ui/button/index.ts";
import { timecode } from "#lib/format.ts";
import { vocabulary } from "#lib/vocabulary.ts";

let { data } = $props();
const words = vocabulary();

const day = new Intl.DateTimeFormat(undefined, { dateStyle: "full" });
const time = new Intl.DateTimeFormat(undefined, { timeStyle: "short" });

// Plays by the day they started on, the newest first.
const days = $derived(
	Map.groupBy(data.history.items, (play) =>
		day.format(new Date(play.started_at)),
	),
);
</script>

<svelte:head><title>History · Photon</title></svelte:head>

<div class="grid max-w-4xl gap-8">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<h1 class="title">History</h1>
		<!-- The session's cookie goes with a plain link, as a backup's download does. -->
		<Button href="/api/v1/history/file" download variant="outline" size="sm"
			>Download history</Button
		>
	</header>
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
							)} · stopped at {timecode(play.position_ms)} · {words
								.play_methods[play.method]}"
						/>
					</li>
				{/each}
			</ul>
		</section>
	{:else}
		<p class="text-ink-3">Nothing played yet.</p>
	{/each}
	<CursorPager next={data.history.next} />
</div>
