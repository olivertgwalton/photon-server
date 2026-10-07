<script lang="ts">
import { act } from "#lib/act.js";
import { confirmFirst } from "#lib/actions.svelte.js";
import { client } from "#lib/api/client.js";
import type { components } from "#lib/api/schema.js";
import { artworkSrc, artworkSrcset } from "#lib/artwork.js";
import { bitrate, timecode } from "#lib/format.js";
import { positionAt } from "#lib/admin/live.js";
import {
	accelerations,
	methods,
	playedTitle,
	reasons,
} from "#lib/admin/words.js";
import { Badge } from "#lib/components/ui/badge/index.js";
import { Progress } from "#lib/components/ui/progress/index.js";

const api = client();

type NowPlaying = components["schemas"]["NowPlaying"];
type Encode = components["schemas"]["PlaybackEncode"];

// One play as the dashboard shows it: who, on what, how far, and what the
// server is doing to the file to get it there.
let {
	playback: p,
	now,
	nodes,
}: { playback: NowPlaying; now: number; nodes: number } = $props();

const picture = $derived(p.title.thumb ?? p.title.backdrop);
const position = $derived(positionAt(p, now));
const name = $derived(playedTitle(p.title));

function resolution(width?: number, height?: number) {
	return width && height ? `${width}×${height}` : "";
}

function rate(kbps?: number) {
	return kbps ? bitrate(kbps) : "";
}

// What a stream is, and what it becomes where the server encodes it.
function line(
	codec: string,
	parts: string[],
	encode: Encode | null | undefined,
	encoded: string[],
) {
	const from = [codec, ...parts].filter(Boolean).join(" ");
	if (!encode) return from;
	return `${from} → ${[encode.codec, ...encoded].filter(Boolean).join(" ")}`;
}

let stopping = $state(false);

function stop() {
	confirmFirst(
		`Stop ${p.profile.name}'s play?`,
		`${name} stops on ${p.device.name} where it is now, and its place is kept.`,
		"Stop",
		async () => {
			stopping = true;
			await act(
				api.DELETE("/api/v1/admin/playbacks/{id}", {
					params: { path: { id: p.id } },
				}),
				`${p.profile.name}'s play was stopped.`,
			);
			stopping = false;
		},
	);
}
</script>

<article class="bg-raise grid overflow-hidden rounded-xl">
	<div class="bg-ground relative aspect-video">
		{#if picture}
			<img
				src={artworkSrc(picture, "still")}
				srcset={artworkSrcset(picture, "still")}
				sizes="(min-width: 1024px) 24rem, 100vw"
				alt=""
				loading="lazy"
				class="size-full object-cover opacity-70"
			>
		{/if}
		<div
			class="absolute inset-x-0 bottom-0 grid gap-1 bg-linear-to-t from-black/90 to-transparent p-4 pt-10"
		>
			<h3 class="font-heading text-ink text-base leading-tight font-bold">
				<a href="/titles/{p.title.id}" class="hover:underline">{name}</a>
			</h3>
			<p class="text-ink-2 text-sm">
				{p.profile.name}
				· {p.device.name} · {p.device.client}
			</p>
		</div>
	</div>
	<div class="grid gap-3 p-4">
		<div class="grid gap-1.5">
			<Progress
				value={position}
				max={Math.max(p.version.duration_ms, 1)}
				aria-label="How far {p.profile.name} is through {name}"
			/>
			<p class="text-ink-3 flex justify-between font-mono text-xs">
				<span
					>{timecode(position)}{p.state === "paused" ? " · paused" : ""}</span
				>
				<span>{timecode(p.version.duration_ms)}</span>
			</p>
		</div>
		<div class="flex flex-wrap gap-1.5">
			<Badge variant={p.method === "transcode" ? "default" : "secondary"}>
				{methods[p.method]}
			</Badge>
			{#if p.acceleration}
				<Badge variant="outline">
					{p.acceleration === "software"
						? "Software"
						: `Hardware · ${accelerations[p.acceleration]}`}
				</Badge>
			{/if}
			{#if nodes > 1}
				<Badge variant="outline" title={p.node_id}>
					Node {p.node_id.slice(-6)}
				</Badge>
			{/if}
		</div>
		{#if p.reasons?.length}
			<p class="text-ink-2 text-sm">
				Because of the {p.reasons.map((r) => reasons[r]).join(", ")}
			</p>
		{/if}
		<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
			<dt class="label self-center">File</dt>
			<dd class="text-ink-2">
				{[p.version.container, p.version.label, rate(p.version.bitrate_kbps)]
					.filter(Boolean)
					.join(" · ")}
			</dd>
			{#if p.video}
				<dt class="label self-center">Video</dt>
				<dd class="text-ink-2">
					{line(
						p.video.codec,
						[
							resolution(p.video.width, p.video.height),
							p.video.range && p.video.range !== "sdr"
								? p.video.range.toUpperCase()
								: "",
							rate(p.video.bitrate_kbps),
						],
						p.video.encode,
						[
							resolution(p.video.encode?.width, p.video.encode?.height),
							p.video.encode?.range && p.video.encode.range !== "sdr"
								? p.video.encode.range.toUpperCase()
								: "",
							rate(p.video.encode?.bitrate_kbps),
							p.video.encode?.tone_mapped ? "tone mapped" : "",
						],
					)}
				</dd>
			{/if}
			{#if p.audio}
				<dt class="label self-center">Audio</dt>
				<dd class="text-ink-2">
					{line(
						p.audio.codec,
						[
							p.audio.channels ? `${p.audio.channels} ch` : "",
							p.audio.language ?? "",
						],
						p.audio.encode,
						[
							p.audio.encode?.channels ? `${p.audio.encode.channels} ch` : "",
							rate(p.audio.encode?.bitrate_kbps),
						],
					)}
				</dd>
			{/if}
			{#if p.subtitle}
				<dt class="label self-center">Subtitles</dt>
				<dd class="text-ink-2">
					{[
						p.subtitle.codec,
						p.subtitle.language,
						p.subtitle.burned ? "burned in" : "",
					]
						.filter(Boolean)
						.join(" · ")}
				</dd>
			{/if}
		</dl>
		<button
			type="button"
			class="text-ink-2 hover:text-ink justify-self-start text-sm font-semibold underline-offset-4 hover:underline"
			disabled={stopping}
			onclick={stop}
		>
			Stop this play
		</button>
	</div>
</article>
