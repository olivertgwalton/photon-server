<script lang="ts">
import { vocabulary } from "#lib/vocabulary.js";
import type { components } from "#lib/api/schema.js";
import * as Dialog from "#lib/components/ui/dialog/index.js";
import { bitrate, bytes, language, runtime, timecode } from "#lib/format.js";

type Version = components["schemas"]["VersionPage"];
type Stream = components["schemas"]["StreamPage"];

// What a title's copies are made of, as Jellyfin's Media Info lists it.
let {
	open = $bindable(false),
	title,
	versions,
}: { open?: boolean; title: string; versions: Version[] } = $props();
const words = vocabulary();

function facts(s: Stream): [string, string | number | undefined][] {
	return [
		["Codec", [s.codec.toUpperCase(), s.profile].filter(Boolean).join(" ")],
		["Language", language(s.language)],
		["Title", s.title],
		["Size", s.width && s.height ? `${s.width}×${s.height}` : undefined],
		[
			"Frame rate",
			s.frame_rate ? `${+s.frame_rate.toFixed(3)} fps` : undefined,
		],
		["Bit depth", s.bit_depth ? `${s.bit_depth}-bit` : undefined],
		["Level", s.level],
		["Range", s.range ? words.ranges[s.range] : undefined],
		["Dolby Vision profile", s.dv_profile],
		["Channels", s.channel_layout ?? s.channels],
		["Sample rate", s.sample_rate ? `${s.sample_rate / 1000} kHz` : undefined],
		["Bitrate", s.bitrate_kbps ? bitrate(s.bitrate_kbps) : undefined],
		[
			"Flags",
			[
				s.default && "Default",
				s.forced && "Forced",
				s.hearing_impaired && "SDH",
				s.commentary && "Commentary",
			]
				.filter(Boolean)
				.join(", "),
		],
	];
}
</script>

{#snippet list(
	rows: [string, string | number | undefined][],
)}
	<dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
		{#each rows.filter(
			([, v]) => v !== undefined && v !== "",
		) as [name, value] (name)}
			<dt class="text-ink-3">{name}</dt>
			<dd class="text-ink min-w-0 wrap-break-word">{value}</dd>
		{/each}
	</dl>
{/snippet}

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[85svh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>Media info</Dialog.Title>
			<Dialog.Description>{title}</Dialog.Description>
		</Dialog.Header>
		{#each versions as version (version.id)}
			<section class="grid gap-4">
				<h3 class="heading">
					{version.label ?? version.edition ?? "Version"}
					{#if version.missing_since}
						<span class="label ml-2">Missing</span>
					{/if}
				</h3>
				{@render list([
					["Container", version.container.toUpperCase()],
					["Duration", runtime(version.duration_ms)],
					["Size", bytes(version.size_bytes)],
					[
						"Bitrate",
						version.bitrate_kbps ? bitrate(version.bitrate_kbps) : undefined,
					],
					["Files", version.parts > 1 ? version.parts : undefined],
				])}
				{#each version.streams as stream (stream.index)}
					<div class="border-line grid gap-2 border-t pt-3">
						<h4 class="label">
							{words.stream_kinds[stream.kind]}
							· stream {stream.index}
						</h4>
						{@render list(facts(stream))}
					</div>
				{/each}
				{#each version.subtitles ?? [] as sub, i (i)}
					<div class="border-line grid gap-2 border-t pt-3">
						<h4 class="label">Subtitle file</h4>
						{@render list([
							["Format", sub.codec.toUpperCase()],
							["Language", language(sub.language)],
							["Title", sub.title],
							[
								"Flags",
								[sub.forced && "Forced", sub.hearing_impaired && "SDH"]
									.filter(Boolean)
									.join(", "),
							],
						])}
					</div>
				{/each}
				{#if version.chapters?.length}
					<div class="border-line grid gap-2 border-t pt-3">
						<h4 class="label">Chapters</h4>
						<ol class="grid gap-1 text-sm">
							{#each version.chapters as chapter, i (i)}
								<li class="flex gap-3">
									<span class="text-ink-3 w-16 font-mono">
										{timecode(chapter.start_ms)}
									</span>
									<span class="text-ink">
										{chapter.title ?? `Chapter ${i + 1}`}
									</span>
								</li>
							{/each}
						</ol>
					</div>
				{/if}
			</section>
		{/each}
	</Dialog.Content>
</Dialog.Root>
