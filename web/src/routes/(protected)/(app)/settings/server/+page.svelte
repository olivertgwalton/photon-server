<script lang="ts">
import { ticking } from "#lib/admin/clock.svelte.js";
import { runTask } from "#lib/actions.svelte.js";
import { liveStream } from "#lib/admin/stream.svelte.js";
import { byName, elapsed } from "#lib/admin/words.js";
import ActivityList from "#lib/components/admin/ActivityList.svelte";
import NowPlayingCard from "#lib/components/admin/NowPlayingCard.svelte";
import RunningNow from "#lib/components/admin/RunningNow.svelte";
import ServerInfo from "#lib/components/admin/ServerInfo.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Card from "#lib/components/ui/card/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";
import { count } from "#lib/format.js";

let { data } = $props();

const live = liveStream();
const clock = ticking();

// What the page loaded until the stream's snapshot arrives, then the stream.
const playbacks = $derived(
	live.state.ready ? live.state.playbacks : data.playbacks.items,
);
const transcodes = $derived(data.playbacks.transcodes);

const libraries = $derived(byName(data.libraries));
const profiles = $derived(byName(data.profiles));
const activity = $derived(
	[
		...live.state.arrived.filter(
			(e) => !data.activity.some((old) => old.id === e.id),
		),
		...data.activity,
	].slice(0, 10),
);

// What an admin should look at, as Plex's and Jellyfin's dashboards put
// warnings first: each with where to put it right.
const attention = $derived(
	[
		!data.libraries.length && {
			said: "There are no libraries yet: add a folder of films or shows.",
			href: "/settings/server/libraries/new",
			go: "Add a library",
		},
		data.dead > 0 && {
			said: `${count(data.dead, "job")} failed every attempt and ${data.dead === 1 ? "waits" : "wait"} to be tried again.`,
			href: "/settings/server/jobs",
			go: "See jobs",
		},
		...data.providers
			.filter((p) => !p.ready)
			.map((p) => ({
				p,
				asking: data.libraries.filter((l) =>
					l.sources.some((k) =>
						[...k.metadata, ...k.images].some(
							(r) => r.enabled && r.source === p.id,
						),
					),
				),
			}))
			.filter(({ asking }) => asking.length)
			.map(({ p, asking }) => ({
				said: `${p.name} needs its settings, and ${asking.map((l) => l.name).join(", ")} ${asking.length === 1 ? "asks" : "ask"} it for metadata.`,
				href: "/settings/server/providers",
				go: "Set it up",
			})),
	].filter((a) => !!a),
);
</script>

<svelte:head><title>Dashboard · Settings · Photon</title></svelte:head>

<div class="flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="title">{data.server.name}</h1>
		<p class="text-ink-3 mt-1 text-sm">
			{data.server.version}
			· up {elapsed(clock.now - Date.parse(data.server.started_at))}
			{#if !live.connected}
				· <span role="status">reconnecting to live updates…</span>
			{/if}
		</p>
	</div>
	<Button
		variant="outline"
		onclick={() => runTask("scan_libraries", "Every library is being scanned.")}
	>
		Scan all libraries
	</Button>
</div>

{#if attention.length}
	<section
		aria-labelledby="attention"
		class="border-destructive/40 grid gap-3 rounded-xl border p-4"
	>
		<h2 id="attention" class="heading">Needs attention</h2>
		<ul class="grid gap-2">
			{#each attention as a (a.said)}
				<li class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
					<p class="text-ink text-sm">{a.said}</p>
					<Button href={a.href} variant="outline" size="sm">{a.go}</Button>
				</li>
			{/each}
		</ul>
	</section>
{/if}

<!-- Activity is a column of its own beside the rest: two cards side by side
	leave a hole under whichever is shorter. -->
<div class="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_24rem]">
	<div class="grid min-w-0 gap-6">
		<section aria-labelledby="playing" class="grid gap-4">
			<div
				class="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2"
			>
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
				<div class="grid gap-4 sm:grid-cols-2">
					{#each playbacks as playback (playback.id)}
						<NowPlayingCard
							{playback}
							now={clock.now}
							nodes={data.nodes.filter((n) => n.online).length}
						/>
					{/each}
				</div>
			{:else}
				<p class="text-ink-3 text-sm">Nobody is watching anything.</p>
			{/if}
		</section>

		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">Running</h2></Card.Title>
			</Card.Header>
			<Card.Content class="grid gap-4">
				<RunningNow live={live.state} {libraries} />
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Title><h2 class="heading">Server</h2></Card.Title>
				<Card.Description>
					Set by the server's environment, but for what each node does; see the
					README to change it. The
					<a href="/api/v1/openapi.json" class="underline">API</a>
					is described in full.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<ServerInfo server={data.server} nodes={data.nodes} now={clock.now} />
			</Card.Content>
		</Card.Root>
	</div>

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
