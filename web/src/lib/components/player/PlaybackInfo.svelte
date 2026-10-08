<script lang="ts">
import type { components } from "#lib/api/schema.js";
import * as Sheet from "#lib/components/ui/sheet/index.js";
import { bitrate, playMethods, rangeName } from "#lib/format.js";
import { channels, reasons } from "#lib/player/words.js";

type Schemas = components["schemas"];

// How the title is reaching this browser and why, as the server decided it:
// Jellyfin's Playback Info, Plex's "stats for nerds".
let {
	open = $bindable(false),
	playback,
	version,
	quality,
	portal,
}: {
	open?: boolean;
	playback: Schemas["Playback"];
	version: Schemas["VersionPage"] | undefined;
	quality: VideoPlaybackQuality | undefined;
	portal: HTMLElement | undefined;
} = $props();

const stream = (index: number | undefined) =>
	version?.streams.find((s) => s.index === index);
const source = $derived(stream(playback.video?.stream));
const sound = $derived(stream(playback.audio?.stream));

const picture = (s: Schemas["StreamPage"] | undefined) =>
	s
		? [
				s.codec.toUpperCase(),
				s.profile,
				s.width && s.height ? `${s.width}×${s.height}` : "",
				s.range && s.range !== "sdr" ? rangeName(s.range) : "",
				s.bitrate_kbps ? bitrate(s.bitrate_kbps) : "",
			]
				.filter(Boolean)
				.join(" · ")
		: "";

const videoTarget = $derived.by(() => {
	const v = playback.video;
	if (!v) return "";
	if (v.decision === "copy") {
		return v.dolby_vision === "strip"
			? "Copied, Dolby Vision removed"
			: "Copied";
	}
	return [
		`Converted to ${v.codec?.toUpperCase()}`,
		v.width && v.height ? `${v.width}×${v.height}` : "",
		v.bitrate_kbps ? bitrate(v.bitrate_kbps) : "",
		v.tone_mapped ? "tone mapped to SDR" : "",
		v.burned_subtitle != null ? "subtitles drawn in" : "",
	]
		.filter(Boolean)
		.join(" · ");
});

const audioTarget = $derived.by(() => {
	const a = playback.audio;
	if (!a) return "";
	if (a.decision === "copy") return "Copied";
	return [
		`Converted to ${a.codec?.toUpperCase()}`,
		channels(a.channels),
		a.bitrate_kbps ? bitrate(a.bitrate_kbps) : "",
	]
		.filter(Boolean)
		.join(" · ");
});
</script>

<Sheet.Root bind:open>
	<Sheet.Content portalProps={{ to: portal }} class="overflow-y-auto">
		<Sheet.Header>
			<Sheet.Title>Playback info</Sheet.Title>
			<Sheet.Description>{playMethods[playback.method]}</Sheet.Description>
		</Sheet.Header>
		<dl class="grid gap-4 px-4 pb-6 text-sm">
			{#if playback.reasons?.length}
				<div>
					<dt class="label">Why</dt>
					<dd>
						<ul class="text-ink mt-1 list-disc pl-4">
							{#each playback.reasons as reason (reason)}
								<li>{reasons[reason]}</li>
							{/each}
						</ul>
					</dd>
				</div>
			{/if}
			{#if version}
				<div>
					<dt class="label">File</dt>
					<dd class="text-ink mt-1">
						{version.container}{version.bitrate_kbps
							? ` · ${bitrate(version.bitrate_kbps)}`
							: ""}
					</dd>
				</div>
			{/if}
			{#if playback.video}
				<div>
					<dt class="label">Video</dt>
					<dd class="text-ink mt-1">{picture(source)}</dd>
					<dd class="mt-0.5">{videoTarget}</dd>
				</div>
			{/if}
			{#if playback.audio}
				<div>
					<dt class="label">Audio</dt>
					<dd class="text-ink mt-1">{sound?.display_title ?? ""}</dd>
					<dd class="mt-0.5">{audioTarget}</dd>
				</div>
			{/if}
			{#if quality}
				<div>
					<dt class="label">Frames</dt>
					<dd class="text-ink mt-1 tabular-nums">
						{quality.droppedVideoFrames}
						dropped of {quality.totalVideoFrames}
					</dd>
				</div>
			{/if}
		</dl>
	</Sheet.Content>
</Sheet.Root>
