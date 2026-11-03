<script lang="ts">
import type { components } from "#lib/api/schema.d.ts";
import * as Dialog from "#lib/components/ui/dialog/index.ts";
import { bytes } from "#lib/format.ts";
import { type Listen, playingAs } from "#lib/player/listen.ts";

type Schemas = components["schemas"];

// Who made what plays and how it is played, as Apple Music's View Credits
// has it: its artists, its album's, its place on the album, and its file.
let {
	open = $bindable(false),
	item,
	page,
	playback,
}: {
	open: boolean;
	item: Listen;
	page: Schemas["TitlePage"];
	playback: Schemas["Playback"] | undefined;
} = $props();

const version = $derived(
	page.versions?.find((v) => v.id === playback?.version_id) ??
		page.versions?.[0],
);
const sound = $derived(version?.streams.find((s) => s.kind === "audio"));
const rows = $derived(
	[
		["Artists", page.artists?.join(", ")],
		[item.kind === "book" ? "Author" : "Album artist", item.creator?.title],
		["Album", item.from?.title],
		[
			"On the album",
			page.track_number
				? [
						page.disc_number && `Disc ${page.disc_number}`,
						`Track ${page.track_number}`,
					]
						.filter(Boolean)
						.join(" · ")
				: undefined,
		],
		["Series", page.series],
		[
			"Playing",
			playback &&
				playingAs(
					playback,
					sound && {
						codec: sound.codec,
						// A FLAC stream says no bitrate of its own; its file's is the same.
						bitrate_kbps: sound.bitrate_kbps ?? version?.bitrate_kbps,
					},
				),
		],
		[
			"File",
			version &&
				[
					version.container.toUpperCase(),
					sound?.sample_rate && `${sound.sample_rate / 1000} kHz`,
					sound?.bit_depth && `${sound.bit_depth}-bit`,
					sound?.channel_layout,
					bytes(version.size_bytes),
				]
					.filter(Boolean)
					.join(" · "),
		],
	].filter((row): row is [string, string] => !!row[1]),
);
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Credits</Dialog.Title>
			<Dialog.Description>{item.title}</Dialog.Description>
		</Dialog.Header>
		<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
			{#each rows as [name, value] (name)}
				<dt class="text-ink-3">{name}</dt>
				<dd class="text-ink">{value}</dd>
			{/each}
		</dl>
	</Dialog.Content>
</Dialog.Root>
