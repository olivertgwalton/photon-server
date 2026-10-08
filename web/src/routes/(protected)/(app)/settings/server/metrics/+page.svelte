<script lang="ts">
import { untrack } from "svelte";
import { vocabulary } from "#lib/vocabulary.js";
import { ticking } from "#lib/admin/clock.svelte.js";
import {
	keepFor,
	type Point,
	pollEvery,
	quantile,
	series,
	total,
} from "#lib/admin/metrics.js";
import { relative } from "#lib/admin/words.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import PageHeader from "#lib/components/PageHeader.svelte";
import JobsTable from "#lib/components/admin/JobsTable.svelte";
import Sparkline from "#lib/components/admin/Sparkline.svelte";
import { Badge } from "#lib/components/ui/badge/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import * as Table from "#lib/components/ui/table/index.js";
import { bitrate, bytes, count } from "#lib/format.js";

type Schemas = components["schemas"];

let { data } = $props();
const words = vocabulary();

const api = client();
const clock = ticking(pollEvery);

// Every answer of the last few minutes, by when it came, for the sparklines
// and for rates between the last two; the page's load is the first.
let answers = $state.raw(
	untrack(() => [{ at: Date.now(), metrics: data.metrics }]),
);

// Asked again every few seconds while the page is in view, as Plex's and
// Jellyfin's dashboards are, and not at all while the tab is hidden.
$effect(() => {
	let timer: ReturnType<typeof setInterval> | undefined;
	async function ask() {
		const { data: metrics } = await api.GET("/api/v1/admin/metrics");
		if (!metrics) return;
		const now = Date.now();
		answers = [
			...answers.filter((a) => a.at > now - keepFor),
			{ at: now, metrics },
		];
	}
	function follow() {
		clearInterval(timer);
		timer = undefined;
		if (document.hidden) return;
		timer = setInterval(ask, pollEvery);
	}
	function shown() {
		if (!document.hidden) void ask();
		follow();
	}
	follow();
	document.addEventListener("visibilitychange", shown);
	return () => {
		clearInterval(timer);
		document.removeEventListener("visibilitychange", shown);
	};
});

const latest = $derived(answers[answers.length - 1].metrics);
const previous = $derived(answers.at(-2)?.metrics);
const lines = $derived(series(answers.map((a) => a.metrics)));
const answered = $derived(latest.nodes.filter((n) => n.metrics));
const cluster = $derived(latest.cluster);

function last(id: string): Point | undefined {
	return lines.get(id)?.at(-1);
}

function line(id: string, value: (p: Point) => number | undefined) {
	return (lines.get(id) ?? []).flatMap((p) => {
		const v = value(p);
		return v === undefined ? [] : [{ at: p.at, value: v }];
	});
}

const methods = Object.keys(words.play_methods) as Schemas["PlayMethod"][];
const streams = $derived(
	methods.map(
		(m) =>
			[
				m,
				answered.reduce((s, n) => s + (n.metrics?.playbacks[m] ?? 0), 0),
			] as const,
	),
);
const transcoding = $derived(
	answered.reduce((s, n) => s + total(n.metrics?.transcodes), 0),
);
// A node with no limit leaves the cluster with none.
const slots = $derived(
	answered.every((n) => n.metrics?.transcode_slots)
		? answered.reduce((s, n) => s + (n.metrics?.transcode_slots ?? 0), 0)
		: undefined,
);
const bandwidth = $derived(
	answered.reduce((s, n) => s + (last(n.id)?.bandwidth ?? 0), 0),
);
const refused = $derived(
	answered.reduce((s, n) => {
		const before = previous?.nodes.find((p) => p.id === n.id)?.metrics;
		if (!before || !n.metrics) return s;
		const d =
			total(n.metrics.transcode_refusals) - total(before.transcode_refusals);
		return d > 0 ? s + d : s;
	}, 0),
);
const unreachable = $derived(latest.nodes.length - answered.length);

const kinds = $derived(
	(Object.keys(words.jobs) as Schemas["JobKind"][]).filter(
		(k) =>
			cluster?.jobs.some((j) => j.kind === k && j.count) ||
			cluster?.oldest_due_seconds[k] !== undefined,
	),
);
const finished = $derived(
	[...(cluster?.tasks ?? [])].sort((a, b) =>
		b.finished_at.localeCompare(a.finished_at),
	),
);

function speed(bytesPerSecond: number | undefined) {
	return bytesPerSecond === undefined
		? "—"
		: bitrate(Math.round((bytesPerSecond * 8) / 1000));
}

function wait(seconds: number | undefined) {
	if (seconds === undefined) return "—";
	return seconds < 1
		? `${Math.round(seconds * 1000)} ms`
		: `${seconds.toFixed(1)} s`;
}

function ago(seconds: number, now: number) {
	return relative(now - seconds * 1000, now);
}
</script>

<PageHeader
	title="Metrics"
	description="What every node is doing now, read again every few seconds while this page is open: its streams, transcodes, bandwidth and load, and the work queued behind them."
/>

<section aria-labelledby="cluster" class="grid gap-3">
	<h2 id="cluster" class="heading">
		{latest.nodes.length === 1 ? "Server" : "Across the nodes"}
	</h2>
	<dl class="grid grid-cols-2 gap-3 lg:grid-cols-3">
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Streams</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{streams.reduce((s, [, n]) => s + n, 0)}
			</dd>
			<dd class="text-ink-3 text-xs">
				{streams
					.map(([m, n]) => `${n} ${words.play_methods[m].toLowerCase()}`)
					.join(" · ")}
			</dd>
		</div>
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Transcodes</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{transcoding}<span class="text-ink-3 text-base font-semibold">
					{slots ? ` of ${slots}` : ", no limit"}</span
				>
			</dd>
			<dd class="text-ink-3 text-xs">
				{refused
					? `${count(refused, "play")} refused in the last ${pollEvery / 1000} seconds`
					: "None refused lately"}
			</dd>
		</div>
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Bandwidth</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{previous ? speed(bandwidth) : "—"}
			</dd>
			<dd class="text-ink-3 text-xs">Sent to players</dd>
		</div>
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Nodes</dt>
			<dd class="text-ink font-heading text-2xl font-bold">
				{latest.nodes.length}
			</dd>
			<dd class="text-ink-3 text-xs">
				{[
					cluster && `${cluster.nodes.active ?? 0} active`,
					cluster?.nodes.draining && `${cluster.nodes.draining} draining`,
					unreachable && `${unreachable} not answering`,
				]
					.filter(Boolean)
					.join(" · ")}
			</dd>
		</div>
		<div class="bg-raise grid content-start gap-1 rounded-xl p-4">
			<dt class="label">Library</dt>
			{#if cluster}
				<dd class="text-ink font-heading text-2xl font-bold">
					{count(cluster.library_items.movie ?? 0, "film")}
					<span class="text-ink-3 font-sans text-sm font-normal">
						{bytes(cluster.library_bytes.movie ?? 0)}
					</span>
				</dd>
				<dd class="text-ink-3 text-xs">
					{count(cluster.library_items.episode ?? 0, "episode")}
					· {bytes(cluster.library_bytes.episode ?? 0)}
				</dd>
			{:else}
				<dd class="text-ink-3 text-sm">Not known yet</dd>
			{/if}
		</div>
	</dl>
</section>

<section aria-labelledby="nodes" class="grid gap-3">
	<h2 id="nodes" class="heading">Nodes</h2>
	<div class="grid gap-4 md:grid-cols-2">
		{#each latest.nodes as n (n.id)}
			{@const m = n.metrics}
			{@const p = last(n.id)}
			<Card.Root>
				<Card.Header>
					<Card.Title
						><h3 class="text-ink font-semibold">{n.name}</h3></Card.Title
					>
					<Card.Description>
						{words.node_roles[n.role].name}
					</Card.Description>
					<Card.Action>
						{#if !m}
							<Badge variant="destructive">Not answering</Badge>
						{:else if n.availability === "draining"}
							<Badge variant="outline">Draining</Badge>
						{:else}
							<Badge variant="secondary">Active</Badge>
						{/if}
					</Card.Action>
				</Card.Header>
				<Card.Content>
					{#if m}
						<dl class="grid grid-cols-2 gap-x-6 gap-y-4 text-sm">
							<div class="grid gap-1">
								<dt class="label">Streams</dt>
								<dd class="text-ink font-mono">{total(m.playbacks)}</dd>
								<dd>
									<Sparkline
										points={line(n.id, (q) => q.streams)}
										label="{n.name}'s streams over the last ten minutes"
									/>
								</dd>
							</div>
							<div class="grid gap-1">
								<dt class="label">Bandwidth</dt>
								<dd class="text-ink font-mono">{speed(p?.bandwidth)}</dd>
								<dd>
									<Sparkline
										points={line(n.id, (q) => q.bandwidth)}
										label="{n.name}'s bandwidth over the last ten minutes"
									/>
								</dd>
							</div>
							<div class="grid gap-1">
								<dt class="label">CPU</dt>
								<dd class="text-ink font-mono">
									{p?.cpu === undefined ? "—" : `${Math.round(p.cpu)}%`}
									<span class="text-ink-3 font-sans text-xs">of a core</span>
								</dd>
								<dd>
									<Sparkline
										points={line(n.id, (q) => q.cpu)}
										label="{n.name}'s CPU over the last ten minutes"
									/>
								</dd>
							</div>
							<div class="grid content-start gap-1">
								<dt class="label">Memory</dt>
								<dd class="text-ink font-mono">
									{bytes(m.resident_memory_bytes)}
								</dd>
							</div>
							<div class="grid content-start gap-1">
								<dt class="label">Transcodes</dt>
								<dd class="text-ink font-mono">
									{total(m.transcodes)}{m.transcode_slots
										? ` of ${m.transcode_slots}`
										: ""}
								</dd>
								{#if m.transcodes.conversion}
									<dd class="text-ink-3 text-xs">
										{m.transcodes.conversion}
										for downloads
									</dd>
								{/if}
							</div>
							<div class="grid content-start gap-1">
								<dt class="label">Segment wait</dt>
								<dd class="text-ink font-mono">
									{wait(quantile(m.segment_wait, 0.5))}
									<span class="text-ink-3 font-sans text-xs">median</span>
								</dd>
								<dd class="text-ink-3 text-xs">
									{wait(quantile(m.segment_wait, 0.95))}
									at the 95th percentile, since it started
								</dd>
							</div>
						</dl>
					{:else}
						<p class="text-destructive text-sm wrap-break-word">{n.error}</p>
					{/if}
				</Card.Content>
			</Card.Root>
		{/each}
	</div>
</section>

<section aria-labelledby="jobs" class="grid gap-3">
	<h2 id="jobs" class="heading">Jobs</h2>
	{#if !cluster}
		<p class="text-ink-3 text-sm">
			No node holds the scheduler lease just now; it passes to another within
			seconds.
		</p>
	{:else if kinds.length}
		<JobsTable counts={cluster.jobs} {kinds} afterLabel="Oldest due">
			{#snippet after(
				kind: Schemas["JobKind"],
			)}
				{@const due = cluster.oldest_due_seconds[kind]}
				{due === undefined ? "" : ago(due, clock.now)}
			{/snippet}
		</JobsTable>
	{:else}
		<p class="text-ink-3 text-sm">The queue is empty.</p>
	{/if}
</section>

{#if cluster}
	<section aria-labelledby="tasks" class="grid gap-3">
		<h2 id="tasks" class="heading">Scheduled tasks</h2>
		{#if finished.length}
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Task</Table.Head>
						<Table.Head>Last finished</Table.Head>
						<Table.Head class="text-right">Result</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each finished as t (t.task)}
						<Table.Row>
							<Table.Cell class="text-ink font-semibold"
								>{words.tasks[t.task].name}</Table.Cell
							>
							<Table.Cell class="text-ink-2"
								>{relative(t.finished_at, clock.now)}</Table.Cell
							>
							<Table.Cell class="text-right">
								<Badge
									variant={t.result === "failed" ? "destructive" : "secondary"}
									>{t.result}</Badge
								>
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		{:else}
			<p class="text-ink-3 text-sm">No task has finished yet.</p>
		{/if}
	</section>
{/if}

<p class="text-ink-3 text-xs">
	This page keeps the last ten minutes only. History over time comes from
	scraping each node's <code class="font-mono">/metrics</code> with Prometheus,
	as the Metrics section of
	<code class="font-mono">docs/cluster.md</code>
	describes.
</p>
