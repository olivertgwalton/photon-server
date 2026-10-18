<script lang="ts">
import type { Bucket } from "#lib/admin/statistics.js";
import * as Tooltip from "#lib/components/ui/tooltip/index.js";
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

// The chart is one stop for Tab, as a toolbar is, its columns walked with the
// arrow keys: a year's 53 columns each in the tab order would be a wall to
// tab through. It starts on the latest, and comes back to the one last read.
let current = $state(-1);
const stop = $derived(
	current >= 0 && current < columns.length ? current : columns.length - 1,
);

function walk(event: KeyboardEvent, from: number) {
	const to = {
		ArrowLeft: from - 1,
		ArrowRight: from + 1,
		Home: 0,
		End: columns.length - 1,
	}[event.key];
	if (to === undefined || to < 0 || to >= columns.length) return;
	event.preventDefault();
	current = to;
	const column = (event.currentTarget as HTMLElement).parentElement?.children[
		to
	];
	(column as HTMLElement | undefined)?.focus();
}
</script>

<div class="grid gap-2">
	<div class="text-ink-3 flex justify-between text-xs" aria-hidden="true">
		<span>{most ? runtime(most) : ""}</span>
	</div>
	<!-- Each column is as tall as its share of the busiest, rounded at its top
	     and square on the baseline; a column with something in it is never
	     thinner than a hairline, so a short day still shows. The whole height
	     of its slot is what the pointer finds, not only the bar. -->
	<div
		class="border-line flex h-40 items-end gap-0.5 border-b"
		role="toolbar"
		aria-label="Time watched, {columns[0]?.days === 1
			? "a column a day"
			: "a column a week"}; arrow keys move between them"
	>
		{#each columns as c, i (c.from.getTime())}
			<Tooltip.Root>
				<Tooltip.Trigger
					class="group focus-visible:ring-ring/50 flex h-full max-w-6 flex-1 cursor-default items-end rounded-t-[4px] outline-none focus-visible:ring-3"
					aria-label={said(c)}
					tabindex={i === stop ? 0 : -1}
					onkeydown={(e: KeyboardEvent) => walk(e, i)}
					onfocus={() => (current = i)}
				>
					<span
						class="bg-ink-2 group-hover:bg-ink group-focus-visible:bg-ink w-full rounded-t-[4px] transition-colors"
						style:height={most && c.ms
							? `max(2px, ${(c.ms / most) * 100}%)`
							: "0"}
					></span>
				</Tooltip.Trigger>
				<Tooltip.Content>{said(c)}</Tooltip.Content>
			</Tooltip.Root>
		{/each}
	</div>
	<div class="text-ink-3 flex justify-between text-xs" aria-hidden="true">
		{#if columns.length}
			<span>{dated.format(columns[0].from)}</span>
			<span>{dated.format(columns.at(-1)?.from ?? columns[0].from)}</span>
		{/if}
	</div>
</div>
