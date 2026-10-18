<script lang="ts">
import type { Bucket } from "#lib/admin/statistics.js";
import { count, runtime } from "#lib/format.js";

// How long was watched in each column's day or week, as columns from one
// baseline: one series, so the card's title names it and no legend repeats it.
let { columns }: { columns: Bucket[] } = $props();

const most = $derived(Math.max(...columns.map((c) => c.ms), 0));
const dated = new Intl.DateTimeFormat(undefined, {
	weekday: "short",
	day: "numeric",
	month: "short",
});

function said(c: Bucket): string {
	const when =
		c.days === 1 ? dated.format(c.from) : `Week of ${dated.format(c.from)}`;
	return c.plays
		? `${when}: ${count(c.plays, "play")}, ${runtime(c.ms)}`
		: `${when}: nothing watched`;
}
</script>

<div class="grid gap-2">
	<div class="text-ink-3 flex justify-between text-xs" aria-hidden="true">
		<span>{most ? runtime(most) : ""}</span>
	</div>
	<!-- Each column is as tall as its share of the busiest, rounded at its top
	     and square on the baseline; a column with something in it is never
	     thinner than a hairline, so a short day still shows. -->
	<div
		class="border-line flex h-40 items-end gap-0.5 border-b"
		role="img"
		aria-label="Time watched, {columns.length === 1
			? "one day"
			: `${columns.length} ${columns[0]?.days === 1 ? "days" : "weeks"}`}"
	>
		{#each columns as c (c.from.getTime())}
			<div class="flex h-full max-w-6 flex-1 items-end" title={said(c)}>
				<div
					class="bg-ink-2 w-full rounded-t-[4px]"
					style:height={most && c.ms
						? `max(2px, ${(c.ms / most) * 100}%)`
						: "0"}
				></div>
			</div>
		{/each}
	</div>
	<div class="text-ink-3 flex justify-between text-xs" aria-hidden="true">
		{#if columns.length}
			<span>{dated.format(columns[0].from)}</span>
			<span>{dated.format(columns.at(-1)?.from ?? columns[0].from)}</span>
		{/if}
	</div>
	<!-- The chart's values, for a reader that cannot see it. -->
	<table class="sr-only">
		<caption>
			Time watched by {columns[0]?.days === 1 ? "day" : "week"}
		</caption>
		<tbody>
			{#each columns as c (c.from.getTime())}
				<tr>
					<td>{said(c)}</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
