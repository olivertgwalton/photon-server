<script lang="ts">
import type { components } from "#lib/api/schema.js";
import { thumbnail } from "#lib/player/trickplay.js";
import { clock } from "#lib/player/words.js";

type Schemas = components["schemas"];

// The timeline: what has played and arrived, the chapters as ticks, and a
// thumbnail of wherever the pointer or a drag is. Seconds throughout, on the
// copy's whole timeline.
let {
	position,
	duration,
	buffered,
	chapters = [],
	trickplay = [],
	onseek,
}: {
	position: number;
	duration: number;
	buffered: number;
	chapters?: Schemas["ChapterRef"][];
	trickplay?: Schemas["PartTrickplay"][];
	onseek: (seconds: number) => void;
} = $props();

// The drawn thumbnail's width; the sheets are scaled to it.
const previewWidth = 192;

let bar = $state<HTMLDivElement>();
let dragging = $state<number>();
let hover = $state<number>();

const shown = $derived(dragging ?? position);
const preview = $derived(dragging ?? hover);
const thumb = $derived(
	preview === undefined ? undefined : thumbnail(trickplay, preview * 1000),
);
const chapter = $derived(
	preview === undefined
		? undefined
		: chapters.findLast((c) => c.start_ms <= preview * 1000),
);
const percent = (s: number) =>
	duration > 0 ? Math.min(Math.max(s / duration, 0), 1) * 100 : 0;

const previewLeft = $derived(
	`clamp(${previewWidth / 2}px, ${percent(preview ?? 0)}%, calc(100% - ${previewWidth / 2}px))`,
);
const thumbStyle = $derived.by(() => {
	if (!thumb) return "";
	const k = previewWidth / thumb.width;
	return [
		`width: ${previewWidth}px`,
		`height: ${thumb.height * k}px`,
		`background: url("${thumb.url}") -${thumb.x * k}px -${thumb.y * k}px / ${thumb.sheetWidth * k}px ${thumb.sheetHeight * k}px no-repeat`,
	].join("; ");
});

function under(event: PointerEvent): number | undefined {
	if (!bar || event.pointerType === "touch") return undefined;
	const box = bar.getBoundingClientRect();
	return (
		Math.min(Math.max((event.clientX - box.left) / box.width, 0), 1) * duration
	);
}
</script>

<div
	bind:this={bar}
	class="group/seek relative flex h-6 items-center"
	role="presentation"
	onpointermove={(e) => (hover = under(e))}
	onpointerleave={() => (hover = undefined)}
>
	{#if preview !== undefined}
		<div
			class="pointer-events-none absolute bottom-full mb-3 flex -translate-x-1/2 flex-col items-center gap-1"
			style:left={previewLeft}
			aria-hidden="true"
		>
			{#if thumb}
				<div
					class="ring-line-strong rounded-md bg-black ring-1"
					style={thumbStyle}
				></div>
			{/if}
			{#if chapter?.title}
				<span class="text-ink max-w-48 truncate text-xs font-semibold">
					{chapter.title}
				</span>
			{/if}
			<span
				class="text-ink rounded bg-black/70 px-1.5 py-0.5 font-mono text-xs tabular-nums"
			>
				{clock(preview)}
			</span>
		</div>
	{/if}

	<div
		class="relative h-1 w-full rounded-full bg-white/20 transition-[height] group-hover/seek:h-1.5 group-has-focus-visible/seek:h-1.5"
	>
		<div
			class="absolute inset-y-0 left-0 rounded-full bg-white/30"
			style="width: {percent(buffered)}%"
		></div>
		<div
			class="bg-ink absolute inset-y-0 left-0 rounded-full"
			style="width: {percent(shown)}%"
		></div>
		{#each chapters.slice(1) as c, i (i)}
			<div
				class="bg-ground/80 absolute inset-y-0 w-0.5"
				style="left: {percent(c.start_ms / 1000)}%"
			></div>
		{/each}
		<div
			class="bg-ink group-has-focus-visible/seek:outline-ink absolute top-1/2 size-3.5 -translate-x-1/2 -translate-y-1/2 scale-0 rounded-full outline-offset-2 transition-transform group-hover/seek:scale-100 group-has-focus-visible/seek:scale-100 group-has-focus-visible/seek:outline-2"
			style="left: {percent(shown)}%"
		></div>
	</div>

	<input
		type="range"
		min="0"
		max={duration}
		step="any"
		value={shown}
		aria-label="Seek"
		aria-valuetext="{clock(shown)} of {clock(duration)}"
		class="absolute inset-0 w-full cursor-pointer opacity-0"
		oninput={(e) => (dragging = Number(e.currentTarget.value))}
		onchange={(e) => {
			onseek(Number(e.currentTarget.value));
			dragging = undefined;
		}}
	>
</div>
