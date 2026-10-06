<script lang="ts">
import { ticking } from "#lib/admin/clock.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { jobKinds, relative, tasks } from "#lib/admin/words.js";
import ActivityList from "#lib/components/admin/ActivityList.svelte";
import NowPlayingCard from "#lib/components/admin/NowPlayingCard.svelte";
import ScanProgress from "#lib/components/admin/ScanProgress.svelte";
import ServerInfo from "#lib/components/admin/ServerInfo.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";
import { act } from "#lib/admin/act.js";
import { client } from "#lib/api/client.js";

let { data } = $props();

const live = liveStream();
const clock = ticking();

// What the page loaded until the stream's snapshot arrives, then the stream.
const playbacks = $derived(
	live.state.ready ? live.state.playbacks : data.playbacks.items,
);
const transcodes = $derived(data.playbacks.transcodes);

const libraries = $derived(
	new Map(data.libraries.map((l) => [l.id, l.name] as const)),
);
const profiles = $derived(
	new Map(data.profiles.map((p) => [p.id, p.name] as const)),
);
const activity = $derived(
	[
		...live.state.arrived.filter(
			(e) => !data.activity.some((old) => old.id === e.id),
		),
		...data.activity,
	].slice(0, 10),
);

// Running jobs by kind: a scan can run hundreds at once.
const jobs = $derived(
	Object.entries(Object.groupBy(live.state.jobs, (j) => j.kind)).map(
		([kind, list]) =>
			[kind as keyof typeof jobKinds, list?.length ?? 0] as const,
	),
);
</script>

<svelte:head><title>Dashboard · Photon</title></svelte:head>

<div class="flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="title">{data.server.name}</h1>
		<p class="text-ink-3 mt-1 text-sm">
			{data.server.version}
			· up {relative(data.server.started_at, clock.now).replace(/ ago$/, "")}
			{#if !live.connected}
				· <span role="status">reconnecting to live updates…</span>
			{/if}
		</p>
	</div>
	<Button
		variant="outline"
		onclick={() =>
			act(
				client().POST("/api/v1/admin/tasks/{key}/run", {
					params: { path: { key: "scan_libraries" } },
				}),
				"Every library is being scanned.",
			)}
	>
		Scan all libraries
	</Button>
</div>

<section aria-labelledby="playing" class="grid gap-4">
	<div class="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
		<h2 id="playing" class="heading">Now playing</h2>
		<div class="flex min-w-56 items-center gap-3 text-sm">
			<span class="text-ink-2 whitespace-nowrap">
				{transcodes.active}
				{transcodes.limit ? `of ${transcodes.limit}` : ""}
				transcoding
				{#if transcodes.conversions}
					· {transcodes.conversions} for downloads
				{/if}
			</span>
			{#if transcodes.limit}
				<Progress
					value={transcodes.active}
					max={transcodes.limit}
					aria-label="Transcode slots in use"
					class="w-24"
				/>
			{/if}
		</div>
	</div>
	{#if playbacks.length}
		<div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
			{#each playbacks as playback (playback.id)}
				<NowPlayingCard
					{playback}
					now={clock.now}
					nodes={data.server.nodes.length}
				/>
			{/each}
		</div>
	{:else}
		<p class="text-ink-3 text-sm">Nobody is watching anything.</p>
	{/if}
</section>

<div class="grid items-start gap-6 lg:grid-cols-2">
	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Running</h2></Card.Title>
		</Card.Header>
		<Card.Content class="grid gap-4">
			{#each live.state.scans as scan, i (`${scan.library_id}-${i}`)}
				<div class="grid gap-1">
					<p class="text-ink font-semibold">
						Scanning {libraries.get(scan.library_id) ?? "a library"}
					</p>
					<ScanProgress
						{scan}
						name={libraries.get(scan.library_id) ?? "a library"}
					/>
				</div>
			{/each}
			{#if live.state.tasks.length}
				<ul class="grid gap-1 text-sm">
					{#each live.state.tasks as task (task.key)}
						<li class="flex justify-between gap-4">
							<span class="text-ink">{tasks[task.key].name}</span>
							<span class="text-ink-3">
								started {relative(task.started_at, clock.now)}
							</span>
						</li>
					{/each}
				</ul>
			{/if}
			{#if jobs.length}
				<ul class="grid gap-1 text-sm">
					{#each jobs as [kind, count] (kind)}
						<li class="flex justify-between gap-4">
							<span class="text-ink">{jobKinds[kind]}</span>
							<span class="text-ink-3">{count} running</span>
						</li>
					{/each}
				</ul>
			{/if}
			{#if !live.state.scans.length && !live.state.tasks.length && !jobs.length}
				<p class="text-ink-3 text-sm">
					{live.state.ready
						? "The server is idle."
						: "Waiting for live updates…"}
				</p>
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title><h2 class="heading">Recent activity</h2></Card.Title>
			<Card.Action>
				<Button href="/settings/server/activity" variant="ghost" size="sm"
					>All activity</Button
				>
			</Card.Action>
		</Card.Header>
		<Card.Content>
			<ActivityList events={activity} {profiles} {libraries} now={clock.now} />
		</Card.Content>
	</Card.Root>
</div>

<Card.Root>
	<Card.Header>
		<Card.Title><h2 class="heading">Server</h2></Card.Title>
		<Card.Description>
			Set by the server's environment; see the README to change it. The
			<a href="/api/v1/openapi.json" class="underline">API</a>
			is described in full.
		</Card.Description>
	</Card.Header>
	<Card.Content>
		<ServerInfo server={data.server} now={clock.now} />
	</Card.Content>
</Card.Root>
