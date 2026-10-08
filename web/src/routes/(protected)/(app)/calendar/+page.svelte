<script lang="ts">
import Artwork from "#lib/components/Artwork.svelte";
import { withQuery } from "#lib/address.js";
import CheckIcon from "@lucide/svelte/icons/check";
import ChevronLeftIcon from "@lucide/svelte/icons/chevron-left";
import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
import FilmIcon from "@lucide/svelte/icons/film";
import TvIcon from "@lucide/svelte/icons/tv";
import { page } from "$app/state";
import type { components } from "#lib/api/schema.js";
import { blurStyle } from "#lib/blurhash.js";
import { dayLabel, monthKey, shift, today } from "#lib/calendar.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { episodeLabel } from "#lib/format.js";

type Entry = components["schemas"]["CalendarEntry"];
type Filter = components["schemas"]["CalendarFilter"];

let { data } = $props();

// A month's day shows this many, and the rest behind "+n more".
const shown = 3;

const filterNames: Record<Filter, string> = {
	mine: "My titles",
	watchlist: "Watchlist",
	favourites: "Favourites",
	all: "Everything",
};
const milestoneNames: Record<components["schemas"]["Milestone"], string> = {
	series_premiere: "Series premiere",
	season_premiere: "Season premiere",
	season_finale: "Finale",
};

const utc = { timeZone: "UTC" } as const;
const monthName = new Intl.DateTimeFormat(undefined, {
	...utc,
	month: "long",
	year: "numeric",
});
const weekday = new Intl.DateTimeFormat(undefined, {
	...utc,
	weekday: "short",
});
const at = (day: string) => new Date(`${day}T00:00:00Z`);

const now = today(new Date());
const thisMonth = $derived(monthKey(data.month));
const byDay = $derived(
	new Map(data.calendar.days.map((d) => [d.date, d.entries])),
);
const days = $derived(
	data.days.map((date) => ({
		date,
		entries: byDay.get(date) ?? [],
		inMonth: data.view === "upcoming" || date.startsWith(thisMonth),
	})),
);
const busyDays = $derived(days.filter((d) => d.inMonth && d.entries.length));

// This page with some of what it asks changed.
const to = (changes: Record<string, string | undefined>) =>
	withQuery(page.url, changes);

// An announced episode leads to its show, which has its place in it.
const href = (e: Entry) =>
	`/titles/${e.availability === "announced" ? e.show?.id : e.id}`;
const heading = (e: Entry) => e.show?.title ?? e.title;
const place = (e: Entry) =>
	episodeLabel(e.season_number, e.episode_number, e.episode_end);
// As Jellyfin's apps say of an episode with no file: unaired until its day
// is past, then missing.
const absence = (e: Entry, day: string) =>
	e.availability !== "announced" ? "" : day > now ? "Unaired" : "Missing";
</script>

<svelte:head><title>Calendar · Photon</title></svelte:head>

{#snippet badges(
	e: Entry,
	day: string,
)}
	{#each e.milestones ?? [] as m (m)}
		<Badge class={m === "season_finale" ? "bg-ink text-ground" : ""}>
			{milestoneNames[m]}
		</Badge>
	{/each}
	{#if absence(e, day)}
		<Badge variant="outline">{absence(e, day)}</Badge>
	{/if}
{/snippet}

{#snippet card(
	e: Entry,
	day: string,
)}
	<a
		href={href(e)}
		class={[
			"group hover:bg-raise focus-visible:outline-signal flex min-w-0 items-center gap-3 rounded-lg p-2",
			e.watched_at && "opacity-60",
		]}
	>
		<span
			class="bg-raise group-hover:ring-line-strong block aspect-[2/3] w-12 shrink-0 overflow-hidden rounded-md ring-2 ring-transparent"
		>
			{#if e.poster}
				<Artwork
					id={e.poster}
					shape="poster"
					sizes="3rem"
					class={[
						"size-full object-cover",
						e.availability === "announced" && "grayscale",
					]}
					style={blurStyle(e.blurhashes?.[e.poster])}
				/>
			{/if}
		</span>
		<span class="grid min-w-0 flex-1 gap-1">
			<span class="text-ink block truncate font-semibold group-hover:underline">
				{heading(e)}
			</span>
			<span class="text-ink-3 block truncate text-sm">
				{e.kind === "episode"
					? [place(e), e.title].filter(Boolean).join(" · ")
					: "Film"}
			</span>
			<span class="flex flex-wrap gap-1 empty:hidden"
				>{@render badges(e, day)}</span
			>
		</span>
		{#if e.watched_at}
			<CheckIcon class="text-ink-3 size-4 shrink-0" aria-hidden="true" />
			<span class="sr-only">Watched</span>
		{/if}
	</a>
{/snippet}

{#snippet chip(
	e: Entry,
)}
	{@const Icon = e.kind === "movie" ? FilmIcon : TvIcon}
	<a
		href={href(e)}
		title="{heading(e)} {place(e)} · {e.title}"
		class={[
			"hover:bg-raise focus-visible:outline-signal flex min-w-0 items-center gap-1.5 rounded-md border px-1.5 py-1 text-xs",
			e.availability === "announced"
				? "border-line text-ink-3 border-dashed"
				: "border-line-strong bg-raise/50 text-ink",
			e.watched_at && "opacity-60",
		]}
	>
		<Icon
			class={[
				"size-3 shrink-0",
				e.kind === "episode" ? "text-signal" : "text-ink-2",
			]}
			aria-hidden="true"
		/>
		<span class={["truncate font-semibold", e.watched_at && "line-through"]}>
			{heading(e)}
			{#if e.kind === "episode"}
				&nbsp;{place(e)}
			{/if}
		</span>
	</a>
{/snippet}

{#snippet more(
	date: string,
	entries: Entry[],
)}
	<Dialog.Root>
		<Dialog.Trigger
			class="text-ink-3 hover:text-ink hover:bg-raise w-full rounded-md px-1.5 py-1 text-left text-xs font-medium"
		>
			+{entries.length - shown}
			more
		</Dialog.Trigger>
		<Dialog.Content class="max-w-md">
			<Dialog.Header>
				<Dialog.Title>{dayLabel(date, now)}</Dialog.Title>
				<Dialog.Description>{entries.length} out that day</Dialog.Description>
			</Dialog.Header>
			<ul class="grid max-h-96 overflow-y-auto">
				{#each entries as e, i (i)}
					<li>{@render card(e, date)}</li>
				{/each}
			</ul>
		</Dialog.Content>
	</Dialog.Root>
{/snippet}

<div class="grid gap-6">
	<header class="flex flex-wrap items-end justify-between gap-4">
		<h1 class="title">
			{data.view === "month"
				? monthName.format(at(`${thisMonth}-01`))
				: "Upcoming"}
		</h1>
		<nav aria-label="Calendar" class="flex flex-wrap items-center gap-2">
			<div class="flex flex-wrap gap-1">
				{#each data.filters as f (f)}
					<Button
						href={to({ filter: f === "mine" ? undefined : f })}
						variant={data.filter === f ? "secondary" : "ghost"}
						size="sm"
						aria-current={data.filter === f ? "page" : undefined}
					>
						{filterNames[f]}
					</Button>
				{/each}
			</div>
			<div class="flex gap-1">
				<Button
					href={to({ view: undefined, month: undefined })}
					variant={data.view === "upcoming" ? "secondary" : "outline"}
					size="sm"
				>
					Upcoming
				</Button>
				<Button
					href={to({ view: "month" })}
					variant={data.view === "month" ? "secondary" : "outline"}
					size="sm"
				>
					Month
				</Button>
			</div>
			{#if data.view === "month"}
				<div class="flex gap-1">
					<Button
						href={to({ month: monthKey(shift(data.month, -1)) })}
						variant="outline"
						size="icon-sm"
						aria-label="Previous month"
					>
						<ChevronLeftIcon />
					</Button>
					<Button href={to({ month: undefined })} variant="outline" size="sm">
						Today
					</Button>
					<Button
						href={to({ month: monthKey(shift(data.month, 1)) })}
						variant="outline"
						size="icon-sm"
						aria-label="Next month"
					>
						<ChevronRightIcon />
					</Button>
				</div>
			{/if}
		</nav>
	</header>

	{#snippet agenda()}
		{#each busyDays as day (day.date)}
			<section aria-labelledby="day-{day.date}" class="grid gap-1">
				<h2
					id="day-{day.date}"
					class={[
						"heading bg-ground/90 sticky top-0 z-10 py-2 backdrop-blur",
						day.date === now && "text-signal",
					]}
				>
					{dayLabel(day.date, now)}
				</h2>
				<ul class="grid gap-1 sm:grid-cols-2 xl:grid-cols-3">
					{#each day.entries as e, i (i)}
						<li>{@render card(e, day.date)}</li>
					{/each}
				</ul>
			</section>
		{:else}
			<div
				class="border-line grid justify-items-start gap-3 rounded-lg border border-dashed p-6"
			>
				<p class="text-ink-2">
					{data.filter === "all"
						? "Nothing out in this time."
						: `Nothing out from ${filterNames[data.filter].toLowerCase()} in this time.`}
				</p>
				{#if data.filter !== "all"}
					<Button href={to({ filter: "all" })} variant="outline" size="sm">
						Show everything
					</Button>
				{/if}
			</div>
		{/each}
	{/snippet}

	{#if data.view === "upcoming"}
		<div class="grid gap-4">{@render agenda()}</div>
	{:else}
		<!-- A phone lists the month's days with anything on them. -->
		<div class="grid gap-4 md:hidden">{@render agenda()}</div>
		<div
			class="border-line bg-line hidden grid-cols-7 gap-px overflow-hidden rounded-lg border md:grid"
		>
			{#each days.slice(0, 7) as day (day.date)}
				<div class="bg-ground label px-2 py-2 text-center">
					{weekday.format(at(day.date))}
				</div>
			{/each}
			{#each days as day (day.date)}
				<section
					aria-label={dayLabel(day.date, now)}
					class={[
						"bg-ground grid min-h-32 content-start gap-1 p-1.5",
						!day.inMonth && "text-ink-3 bg-ground/60",
					]}
				>
					<span
						class={[
							"grid size-7 place-items-center rounded-md text-sm font-semibold",
							day.date === now && "bg-signal text-signal-ink",
						]}
					>
						{at(day.date).getUTCDate()}
					</span>
					{#each day.entries.slice(0, shown) as e, i (i)}
						{@render chip(e)}
					{/each}
					{#if day.entries.length > shown}
						{@render more(day.date, day.entries)}
					{/if}
				</section>
			{/each}
		</div>
	{/if}
</div>
