<script lang="ts">
import { narrow } from "#lib/admin/narrow.js";
import { buckets } from "#lib/admin/statistics.js";
import WatchedChart from "#lib/components/admin/WatchedChart.svelte";
import Choice from "#lib/components/Choice.svelte";
import PageHeader from "#lib/components/PageHeader.svelte";
import * as Card from "#lib/components/ui/card/index.js";
import { Label } from "#lib/components/ui/label/index.js";
import * as Table from "#lib/components/ui/table/index.js";
import { count, fullTitle, runtime } from "#lib/format.js";

let { data } = $props();

const everyone = $derived([
	{ value: "all", label: "Everyone" },
	...data.profiles.map((p) => ({ value: p.id, label: p.name })),
]);
const spanOptions = [
	{ value: "7", label: "Last 7 days" },
	{ value: "30", label: "Last 30 days" },
	{ value: "90", label: "Last 90 days" },
	{ value: "365", label: "Last year" },
];
const columns = $derived(buckets(data.stats.daily));
</script>

<PageHeader
	title="Watch statistics"
	description="What was watched, who watched it, and what was watched most."
>
	{#snippet actions()}
		<div class="grid gap-1.5">
			<Label for="days">When</Label>
			<Choice
				id="days"
				name="days"
				value={String(data.days)}
				options={spanOptions}
				onchange={(v: string) => narrow("days", v)}
				class="w-40"
			/>
		</div>
		<div class="grid gap-1.5">
			<Label for="profile">Who</Label>
			<Choice
				id="profile"
				name="profile"
				value={data.profile ?? "all"}
				options={everyone}
				onchange={(v: string) => narrow("profile", v)}
				class="w-48"
			/>
		</div>
	{/snippet}
</PageHeader>

<div class="grid gap-6">
	<dl class="grid grid-cols-2 gap-3 sm:max-w-md">
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Plays</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{data.stats.plays.toLocaleString()}
			</dd>
		</div>
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Time watched</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{data.stats.watched_ms ? runtime(data.stats.watched_ms) : "None"}
			</dd>
		</div>
	</dl>

	<Card.Root>
		<Card.Header>
			<Card.Title
				><h2 class="text-ink font-semibold">Time watched</h2></Card.Title
			>
		</Card.Header>
		<Card.Content>
			<WatchedChart {columns} />
		</Card.Content>
	</Card.Root>

	{#if data.stats.plays}
		<div class="grid gap-6 lg:grid-cols-2">
			<section class="grid content-start gap-2">
				<h2 class="text-ink font-semibold">Who watched</h2>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Profile</Table.Head>
							<Table.Head class="text-right">Plays</Table.Head>
							<Table.Head class="text-right">Time</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each data.stats.profiles as p (p.profile_id)}
							<Table.Row>
								<Table.Cell>
									<a
										href="/settings/server/history?profile={p.profile_id}"
										class="text-ink font-semibold hover:underline"
										>{p.name}</a
									>
								</Table.Cell>
								<Table.Cell class="text-right font-mono">{p.plays}</Table.Cell>
								<Table.Cell class="text-right font-mono">
									{runtime(p.watched_ms)}
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</section>
			<section class="grid content-start gap-2">
				<h2 class="text-ink font-semibold">Watched most</h2>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Title</Table.Head>
							<Table.Head class="text-right">Plays</Table.Head>
							<Table.Head class="text-right">Time</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each data.stats.titles as t (t.title.id)}
							<Table.Row>
								<Table.Cell class="max-w-xs truncate">
									<a
										href="/titles/{t.title.id}"
										class="text-ink font-semibold hover:underline"
									>
										{fullTitle(t.title, t.title.show?.title)}
									</a>
								</Table.Cell>
								<Table.Cell class="text-right font-mono">{t.plays}</Table.Cell>
								<Table.Cell class="text-right font-mono">
									{runtime(t.watched_ms)}
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</section>
		</div>
	{:else}
		<p class="text-ink-3 text-sm">
			Nothing was watched
			{data.days === 365
				? "in the last year"
				: `in the last ${count(data.days, "day")}`}.
		</p>
	{/if}
</div>
