<script lang="ts">
import InfoIcon from "@lucide/svelte/icons/info";
import SettingsIcon from "@lucide/svelte/icons/settings";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Menu from "#lib/components/ui/dropdown-menu/index.js";
import { bitrate, timecode } from "#lib/format.js";
import type { SubtitleDrawing } from "#lib/player/drawing.svelte.js";
import type { Choice } from "#lib/player/subtitles.js";
import { type Fit, fits, qualities, speeds } from "#lib/player/words.js";

// The player's settings: the tracks, the subtitle's timing, the quality, the
// speed and the picture, its chapters, and how it is playing.
let {
	open = $bindable(false),
	fit = $bindable(),
	portal,
	version,
	audio,
	subtitles,
	subtitle,
	drawing,
	timing,
	quality,
	rate,
	casting,
	info,
	onaudio,
	onsubtitle,
	onquality,
	onspeed,
	onseek,
	oninfo,
}: {
	open?: boolean;
	fit: Fit;
	portal: HTMLElement | undefined;
	version: components["schemas"]["VersionPage"] | undefined;
	audio: number | undefined;
	subtitles: Choice[];
	subtitle: string | undefined;
	drawing: SubtitleDrawing;
	// Whether the subtitle shown can be moved in time.
	timing: boolean;
	quality: number;
	rate: number;
	casting: boolean;
	// Whether there is a playback to say how it plays.
	info: boolean;
	onaudio: (index: number) => void;
	onsubtitle: (key: string) => void;
	onquality: (kbps: number) => void;
	onspeed: (rate: number) => void;
	onseek: (seconds: number) => void;
	oninfo: () => void;
} = $props();

const audioStreams = $derived(
	version?.streams.filter((s) => s.kind === "audio") ?? [],
);

// A choice in one of the submenus leaves bits-ui's menu open, so each closes
// it.
function chose<T>(then: (value: T) => void) {
	return (value: T) => {
		open = false;
		then(value);
	};
}
</script>

<Menu.Root bind:open>
	<Menu.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="ghost" size="icon" aria-label="Settings">
				<SettingsIcon class="size-5" />
			</Button>
		{/snippet}
	</Menu.Trigger>
	<Menu.Content
		portalProps={{ to: portal }}
		side="top"
		align="end"
		class="w-56"
	>
		{#if audioStreams.length > 1}
			<Menu.Sub>
				<Menu.SubTrigger>Audio</Menu.SubTrigger>
				<Menu.SubContent
					portalProps={{ to: portal }}
					class="max-h-80 overflow-y-auto"
				>
					<Menu.RadioGroup
						value={String(audio)}
						onValueChange={chose((v: string) => onaudio(Number(v)))}
					>
						{#each audioStreams as s (s.index)}
							<Menu.RadioItem value={String(s.index)}
								>{s.display_title}</Menu.RadioItem
							>
						{/each}
					</Menu.RadioGroup>
				</Menu.SubContent>
			</Menu.Sub>
		{/if}
		{#if subtitles.length}
			<Menu.Sub>
				<Menu.SubTrigger>Subtitles</Menu.SubTrigger>
				<Menu.SubContent
					portalProps={{ to: portal }}
					class="max-h-80 overflow-y-auto"
				>
					<Menu.RadioGroup
						value={subtitle ?? "off"}
						onValueChange={chose(onsubtitle)}
					>
						<Menu.RadioItem value="off">Off</Menu.RadioItem>
						{#each subtitles as c (c.key)}
							<Menu.RadioItem value={c.key}>{c.label}</Menu.RadioItem>
						{/each}
					</Menu.RadioGroup>
				</Menu.SubContent>
			</Menu.Sub>
		{/if}
		{#if timing}
			<Menu.Sub>
				<Menu.SubTrigger>
					Subtitle timing
					<span class="text-ink-2 ml-auto text-xs tabular-nums">
						{drawing.delay
							? `${Math.abs(drawing.delay)} s ${drawing.delay > 0 ? "later" : "earlier"}`
							: "On time"}
					</span>
				</Menu.SubTrigger>
				<Menu.SubContent portalProps={{ to: portal }}>
					{#each [-1, -0.1, 0.1, 1] as by (by)}
						<Menu.Item closeOnSelect={false} onSelect={() => drawing.move(by)}>
							{by < 0 ? "Earlier" : "Later"}
							by {Math.abs(by)} s
						</Menu.Item>
					{/each}
					<Menu.Item
						closeOnSelect={false}
						disabled={!drawing.delay}
						onSelect={() => drawing.move(-drawing.delay)}
					>
						On time
					</Menu.Item>
				</Menu.SubContent>
			</Menu.Sub>
		{/if}
		<Menu.Sub>
			<Menu.SubTrigger>Quality</Menu.SubTrigger>
			<Menu.SubContent
				portalProps={{ to: portal }}
				class="max-h-80 overflow-y-auto"
			>
				<Menu.RadioGroup
					value={String(quality)}
					onValueChange={chose((v: string) => onquality(Number(v)))}
				>
					{#each qualities as kbps (kbps)}
						<Menu.RadioItem value={String(kbps)}
							>{kbps ? bitrate(kbps) : "Original"}</Menu.RadioItem
						>
					{/each}
				</Menu.RadioGroup>
			</Menu.SubContent>
		</Menu.Sub>
		{#if !casting}
			<Menu.Sub>
				<Menu.SubTrigger>Speed</Menu.SubTrigger>
				<Menu.SubContent portalProps={{ to: portal }}>
					<Menu.RadioGroup
						value={String(rate)}
						onValueChange={chose((v: string) => onspeed(Number(v)))}
					>
						{#each speeds as s (s)}
							<Menu.RadioItem value={String(s)}
								>{s === 1 ? "Normal" : `${s}×`}</Menu.RadioItem
							>
						{/each}
					</Menu.RadioGroup>
				</Menu.SubContent>
			</Menu.Sub>
			<Menu.Sub>
				<Menu.SubTrigger>Picture</Menu.SubTrigger>
				<Menu.SubContent portalProps={{ to: portal }}>
					<Menu.RadioGroup
						value={fit}
						onValueChange={chose((v: string) => {
							fit = v as Fit;
						})}
					>
						{#each Object.entries(fits) as [value, f] (value)}
							<Menu.RadioItem {value}>{f.label}</Menu.RadioItem>
						{/each}
					</Menu.RadioGroup>
				</Menu.SubContent>
			</Menu.Sub>
		{/if}
		{#if version?.chapters?.length}
			<Menu.Sub>
				<Menu.SubTrigger>Chapters</Menu.SubTrigger>
				<Menu.SubContent
					portalProps={{ to: portal }}
					class="max-h-80 overflow-y-auto"
				>
					{#each version.chapters as c, i}
						<Menu.Item onSelect={() => onseek(c.start_ms / 1000)}>
							<span class="truncate">{c.title || `Chapter ${i + 1}`}</span>
							<Menu.Shortcut>{timecode(c.start_ms)}</Menu.Shortcut>
						</Menu.Item>
					{/each}
				</Menu.SubContent>
			</Menu.Sub>
		{/if}
		{#if info}
			<Menu.Separator />
			<Menu.Item onSelect={oninfo}>
				<InfoIcon />
				Playback info
			</Menu.Item>
		{/if}
	</Menu.Content>
</Menu.Root>
