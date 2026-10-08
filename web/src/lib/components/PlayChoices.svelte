<script lang="ts">
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { onDisk, trackName, versionName } from "#lib/format.js";

type Version = components["schemas"]["VersionPage"];

// Which copy, which sound and which subtitles a title is played with: only
// what the reader changed is bound. A choice with one answer is not offered. Subtitle files beside the copy are
// the player's to offer: the server hands them over when the title starts.
let {
	versions,
	version = $bindable(),
	audio = $bindable(),
	subtitle = $bindable(),
}: {
	versions: Version[];
	version?: string;
	audio?: number;
	subtitle?: number | "off";
} = $props();

const id = $props.id();
const chosen = $derived(onDisk(versions, version) ?? versions[0]);
const audios = $derived(
	chosen?.streams.filter((s) => s.kind === "audio") ?? [],
);
const subtitles = $derived(
	chosen?.streams.filter((s) => s.kind === "subtitle") ?? [],
);

// What plays when the reader chooses nothing: the copy's default track, and
// its default or forced subtitles. Left unchosen, nothing is sent and the
// server decides as it would for any client.
const audible = $derived(audios.find((s) => s.default) ?? audios[0]);
const captioned = $derived(subtitles.find((s) => s.default || s.forced));
</script>

{#snippet choice(
	name: string,
	key: string,
	value: string,
	options: { value: string; label: string; disabled?: boolean }[],
	set: (value: string) => void,
)}
	<div class="grid max-w-full gap-1">
		<span id="{id}-{key}" class="label">{name}</span>
		<Choice
			aria-labelledby="{id}-{key}"
			class="max-w-80"
			{value}
			{options}
			onchange={set}
		/>
	</div>
{/snippet}

<div class="flex flex-wrap gap-4">
	{#if versions.length > 1}
		{@render choice(
			"Version",
			"version",
			chosen?.id ?? "",
			versions.map((v) => ({
				value: v.id,
				label: versionName(v),
				disabled: !!v.missing_since,
			})),
			(value) => {
				// Tracks are numbered per copy, so a new copy starts unchosen.
				version = value;
				audio = subtitle = undefined;
			},
		)}
	{/if}
	{#if audios.length > 1}
		{@render choice(
			"Audio",
			"audio",
			String(audio ?? audible?.index),
			audios.map((s) => ({ value: String(s.index), label: trackName(s) })),
			(value) => (audio = Number(value)),
		)}
	{/if}
	{#if subtitles.length}
		{@render choice(
			"Subtitles",
			"subtitle",
			String(subtitle ?? captioned?.index ?? "off"),
			[
				{ value: "off", label: "Off" },
				...subtitles.map((s) => ({
					value: String(s.index),
					label: trackName(s),
				})),
			],
			(value) => (subtitle = value === "off" ? "off" : Number(value)),
		)}
	{/if}
</div>
