<script lang="ts">
import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
import CaptionsIcon from "@lucide/svelte/icons/captions";
import CaptionsOffIcon from "@lucide/svelte/icons/captions-off";
import InfoIcon from "@lucide/svelte/icons/info";
import LoaderIcon from "@lucide/svelte/icons/loader-circle";
import MaximizeIcon from "@lucide/svelte/icons/maximize";
import MinimizeIcon from "@lucide/svelte/icons/minimize";
import PauseIcon from "@lucide/svelte/icons/pause";
import PipIcon from "@lucide/svelte/icons/picture-in-picture-2";
import PlayIcon from "@lucide/svelte/icons/play";
import BackIcon from "@lucide/svelte/icons/rotate-ccw";
import ForwardIcon from "@lucide/svelte/icons/rotate-cw";
import SettingsIcon from "@lucide/svelte/icons/settings";
import VolumeIcon from "@lucide/svelte/icons/volume-2";
import MuteIcon from "@lucide/svelte/icons/volume-x";
import type Hls from "hls.js";
import { onDestroy, onMount, untrack } from "svelte";
import { goto } from "$app/navigation";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Menu from "#lib/components/ui/dropdown-menu/index.js";
import {
	browserProfile,
	type Capabilities,
	probe,
} from "#lib/player/profile.js";
import {
	loadPreferences,
	pickAudio,
	pickSubtitle,
	savePreferences,
	skipping,
} from "#lib/player/preferences.js";
import { ProgressReporter, type Report } from "#lib/player/progress.js";
import { choices, needsReplay, wants, webVTT } from "#lib/player/subtitles.js";
import {
	audioLabel,
	bitrate,
	clock,
	qualities,
	reasons,
	skips,
} from "#lib/player/words.js";
import PlaybackInfo from "./PlaybackInfo.svelte";
import SeekBar from "./SeekBar.svelte";
import UpNext from "./UpNext.svelte";

type Schemas = components["schemas"];

// One title playing: asks the server how this browser can have it, plays what
// it answers (the file itself, or HLS through hls.js or the browser's own),
// says where it has got to, and asks again when a choice needs another stream.
let {
	title,
	start,
	version: askedVersion,
	audio: askedAudio,
	subtitle: askedSubtitle,
}: {
	title: Schemas["TitlePage"];
	start: number;
	version?: string;
	audio?: number;
	subtitle?: number | "off";
} = $props();

const api = client();
const step = 10;
const nudge = 5;
const restAfter = 3000;
const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2];

let root = $state<HTMLElement>();
let video = $state<HTMLVideoElement>();
let hls: Hls | undefined;
let caps: Capabilities | undefined;
let reporter: ProgressReporter | undefined;
let opening = 0;
let pendingSeek: number | undefined;

let playback = $state<Schemas["Playback"]>();
let refusal = $state<{
	message: string;
	reasons: Schemas["TranscodeReason"][];
}>();
// The choices start as the address asked and are the reader's from then on.
// What this browser's settings ask for where the address chose nothing.
const prefs = loadPreferences();
const startVersion = untrack(
	() =>
		title.versions?.find((v) => v.id === askedVersion) ?? title.versions?.[0],
);
let audio = $state(
	untrack(() =>
		askedAudio === undefined && startVersion
			? pickAudio(startVersion.streams, prefs)
			: askedAudio,
	),
);
let subtitleKey = $state(
	untrack(() => {
		if (askedSubtitle === "off") return undefined;
		if (askedSubtitle !== undefined) return `s${askedSubtitle}`;
		if (!startVersion) return undefined;
		const sound = startVersion.streams.find(
			(t) =>
				t.kind === "audio" &&
				(audio === undefined ? t.default : t.index === audio),
		);
		return pickSubtitle(
			choices(startVersion),
			sound?.language,
			prefs,
			navigator.language,
		);
	}),
);
let lastSubtitle = $state<string>();
let quality = $state(prefs.quality);
// The file as it is would not play here after all; the server converts it.
let convert = $state(false);
let trackSrc = $state<string>();
let trackFile: string | undefined;

let part = $state(0);
let time = $state(0);
let bufferedTo = $state(0);
let paused = $state(true);
let waiting = $state(true);
let volume = $state(1);
let muted = $state(false);
let rate = $state(1);
let fullscreen = $state(false);
let pipAvailable = $state(false);
let resting = $state(false);
let menuOpen = $state(false);
let infoOpen = $state(false);
let frames = $state<VideoPlaybackQuality>();
let next = $state<Schemas["Card"]>();
let upNextHidden = $state(false);
let restTimer: ReturnType<typeof setTimeout> | undefined;

const version = $derived(
	title.versions?.find(
		(v) => v.id === (playback?.version_id ?? askedVersion),
	) ?? title.versions?.[0],
);
const duration = $derived((version?.duration_ms ?? 0) / 1000);
const offset = $derived((playback?.parts?.[part]?.offset_ms ?? 0) / 1000);
const position = $derived(offset + time);
const subtitleChoices = $derived(version ? choices(version) : []);
const subtitle = $derived(subtitleChoices.find((c) => c.key === subtitleKey));
const audioStreams = $derived(
	version?.streams.filter((s) => s.kind === "audio") ?? [],
);
const playingAudio = $derived(playback?.audio?.stream ?? audio);
const marker = $derived(
	version?.markers?.find(
		(m) => position * 1000 >= m.start_ms && position * 1000 < m.end_ms - 1000,
	),
);
const credits = $derived(version?.markers?.find((m) => m.kind === "credits"));
// A marker skipped by itself is skipped once: seeking back into it plays it.
const skippedAt = new Set<number>();
$effect(() => {
	if (!marker || skipping(marker.kind, prefs) !== "auto") return;
	if (skippedAt.has(marker.start_ms)) return;
	skippedAt.add(marker.start_ms);
	untrack(() => skip(marker));
});
const upNext = $derived(
	next &&
		!upNextHidden &&
		duration > 0 &&
		position >= (credits ? credits.start_ms / 1000 : duration - 30),
);
const chromeHidden = $derived(
	resting && !paused && !menuOpen && !infoOpen && !refusal,
);
const heading = $derived(
	title.show
		? [
				title.show.title,
				title.season_number != null && title.episode_number != null
					? `S${title.season_number} E${title.episode_number}`
					: "",
				title.title,
			]
				.filter(Boolean)
				.join(" · ")
		: title.title,
);

function send(id: string) {
	return (r: Report) => {
		// The stop is fetched directly: openapi-fetch starts its request a
		// tick later, after a closing page has gone.
		const call =
			r.kind === "stop"
				? fetch(`/api/v1/playback/${id}/stop`, {
						method: "POST",
						headers: { "content-type": "application/json" },
						body: JSON.stringify({
							position_ms: r.position_ms,
						} satisfies Schemas["Position"]),
						keepalive: r.keepalive,
					})
				: api.POST("/api/v1/playback/{id}/progress", {
						params: { path: { id } },
						body: { position_ms: r.position_ms, state: r.state },
					});
		return call.catch(() => undefined);
	};
}

// Opens a playback at a moment, stopping the one before it first so the
// server is never transcoding twice for one reader.
async function open(at: number) {
	const mine = ++opening;
	waiting = true;
	refusal = undefined;
	const previous = reporter;
	reporter = undefined;
	await previous?.stop();
	detach();
	caps ??= await probe();
	const asked = wants(subtitle);
	const profile = browserProfile(caps, quality);
	// A file opened as it is shows no subtitle inside it, so a profile that
	// wants one opens nothing as it is and the server carries it in HLS.
	if (asked.viaHLS || convert) profile.containers = [];
	const { data, error } = await api.POST("/api/v1/titles/{id}/play", {
		params: { path: { id: title.id } },
		body: {
			version_id: playback?.version_id ?? askedVersion,
			audio_stream: audio,
			subtitle_stream: asked.subtitle_stream,
			profile,
		},
	});
	// A playback opened after the reader moved on is stopped where it began.
	if (mine !== opening) {
		if (data)
			void send(data.playback_id)({
				kind: "stop",
				position_ms: Math.round(at * 1000),
				keepalive: false,
			});
		return;
	}
	if (!data) {
		waiting = false;
		refusal = {
			message: problemMessage(error),
			reasons:
				error && "reasons" in error && Array.isArray(error.reasons)
					? error.reasons
					: [],
		};
		return;
	}
	playback = data;
	reporter = new ProgressReporter(
		() => position * 1000,
		send(data.playback_id),
	);
	await attach(data, at);
}

function reopen() {
	void open(position);
}

async function attach(p: Schemas["Playback"], at: number) {
	if (!video || !caps) return;
	if (p.playlist) {
		part = 0;
		time = at;
		if (caps.nativeHLS) {
			pendingSeek = at;
			video.src = p.playlist;
		} else {
			const { default: HLS } = await import("hls.js");
			hls = new HLS({ startPosition: at });
			hls.on(HLS.Events.SUBTITLE_TRACKS_UPDATED, () => void showSubtitle());
			hls.on(HLS.Events.ERROR, (_, e) => {
				if (e.fatal)
					refusal = {
						message: `The stream stopped: ${e.details}.`,
						reasons: [],
					};
			});
			hls.loadSource(p.playlist);
			hls.attachMedia(video);
		}
	} else {
		load(partAt(at), at);
	}
	await showSubtitle();
	video.play().catch(() => {
		paused = true;
	});
}

function detach() {
	hls?.destroy();
	hls = undefined;
	if (trackSrc?.startsWith("blob:")) URL.revokeObjectURL(trackSrc);
	trackSrc = undefined;
	trackFile = undefined;
	if (video?.hasAttribute("src")) {
		video.removeAttribute("src");
		video.load();
	}
}

function partAt(seconds: number): number {
	const parts = playback?.parts ?? [];
	return Math.max(
		parts.findLastIndex((p) => p.offset_ms / 1000 <= seconds),
		0,
	);
}

function load(index: number, at: number) {
	const p = playback?.parts?.[index];
	if (!video || !p) return;
	part = index;
	time = at - p.offset_ms / 1000;
	pendingSeek = time;
	video.src = p.url;
}

function seek(seconds: number) {
	if (!video) return;
	const to = Math.min(Math.max(seconds, 0), duration || seconds);
	const index = partAt(to);
	if (playback?.parts && index !== part) {
		const playing = !video.paused;
		load(index, to);
		if (playing) void video.play();
		return;
	}
	video.currentTime = to - offset;
	time = video.currentTime;
}

// Shows the subtitle chosen in the playback there is: a file beside the copy
// as a track, or one of the HLS playlist's renditions.
async function showSubtitle() {
	if (!playback || !video) return;
	if (playback.method === "direct") {
		const file =
			subtitle?.file === undefined
				? undefined
				: playback.subtitles?.[subtitle.file];
		if (file?.id === trackFile) return;
		if (trackSrc?.startsWith("blob:")) URL.revokeObjectURL(trackSrc);
		trackSrc = undefined;
		trackFile = file?.id;
		if (!file) return;
		if (file.codec === "webvtt") {
			trackSrc = file.url;
			return;
		}
		const srt = await fetch(file.url).then((r) => r.text());
		trackSrc = URL.createObjectURL(
			new Blob([webVTT(srt)], { type: "text/vtt" }),
		);
		return;
	}
	const n =
		playback.video?.burned_subtitle == null ? (subtitle?.rendition ?? -1) : -1;
	if (hls) {
		hls.subtitleTrack = n;
		hls.subtitleDisplay = n >= 0;
		return;
	}
	const tracks = [...video.textTracks].filter(
		(t) => t.kind === "subtitles" || t.kind === "captions",
	);
	tracks.forEach((t, i) => {
		t.mode = i === n ? "showing" : "disabled";
	});
}

function chooseSubtitle(key: string) {
	menuOpen = false;
	const chosen = subtitleChoices.find((c) => c.key === key);
	if (chosen) lastSubtitle = key;
	subtitleKey = chosen?.key;
	if (playback && needsReplay(chosen, playback)) reopen();
	else void showSubtitle();
}

function toggleSubtitles() {
	if (subtitleKey) chooseSubtitle("off");
	else chooseSubtitle(lastSubtitle ?? subtitleChoices[0]?.key ?? "off");
}

function chooseAudio(index: number) {
	menuOpen = false;
	if (index === playingAudio) return;
	audio = index;
	reopen();
}

function chooseQuality(kbps: number) {
	menuOpen = false;
	if (kbps === quality) return;
	quality = kbps;
	savePreferences({ quality: kbps });
	reopen();
}

function chooseSpeed(r: number) {
	menuOpen = false;
	if (!video) return;
	video.defaultPlaybackRate = r;
	video.playbackRate = r;
}

function toggle() {
	if (!video) return;
	if (video.paused) void video.play();
	else video.pause();
}

function toggleFullscreen() {
	if (document.fullscreenElement) void document.exitFullscreen();
	else if (root?.requestFullscreen) void root.requestFullscreen();
	else
		(
			video as HTMLVideoElement & { webkitEnterFullscreen?: () => void }
		)?.webkitEnterFullscreen?.();
}

function togglePip() {
	if (document.pictureInPictureElement) void document.exitPictureInPicture();
	else void video?.requestPictureInPicture();
}

function playNext() {
	if (next) void goto(`/play/${next.id}`, { replaceState: true });
}

function skip(m: Schemas["MarkerRef"]) {
	if (next && m.end_ms / 1000 >= duration - 1) playNext();
	else seek(m.end_ms / 1000);
}

function wake() {
	resting = false;
	clearTimeout(restTimer);
	restTimer = setTimeout(() => {
		resting = true;
	}, restAfter);
}

// Jellyfin's and YouTube's keys. A control under focus keeps Space, Enter
// and its arrows; the seek bar's arrows are the player's.
function keydown(event: KeyboardEvent) {
	if (event.metaKey || event.ctrlKey || event.altKey || infoOpen || menuOpen)
		return;
	const target = event.target as HTMLElement;
	const seekBar = target.matches?.("input[aria-label='Seek']");
	const control =
		!seekBar && target.closest?.("button, a, input, [role='menuitem']");
	if (target.matches?.("input:not([type='range'])")) return;
	const key = event.key;
	let handled = true;
	if ((key === " " || key === "k") && !(control && key === " ")) toggle();
	else if (key === "ArrowLeft" && (!control || seekBar)) seek(position - nudge);
	else if (key === "ArrowRight" && (!control || seekBar))
		seek(position + nudge);
	else if (key === "j") seek(position - step);
	else if (key === "l") seek(position + step);
	else if (key === "ArrowUp" && !control && video)
		video.volume = Math.min(video.volume + 0.05, 1);
	else if (key === "ArrowDown" && !control && video)
		video.volume = Math.max(video.volume - 0.05, 0);
	else if (key === "m" && video) video.muted = !video.muted;
	else if (key === "f") toggleFullscreen();
	else if (key === "c") toggleSubtitles();
	else if (/^[0-9]$/.test(key) && duration) seek((duration * Number(key)) / 10);
	else handled = false;
	if (handled) {
		event.preventDefault();
		wake();
	}
}

onMount(() => {
	const fullscreenChange = () => {
		fullscreen = !!document.fullscreenElement;
	};
	// The browser's own HLS adds the playlist's subtitles as it reads them.
	const tracksAdded = () => {
		if (playback?.playlist && !hls) void showSubtitle();
	};
	pipAvailable = document.pictureInPictureEnabled;
	document.addEventListener("fullscreenchange", fullscreenChange);
	video?.textTracks.addEventListener("addtrack", tracksAdded);
	if (title.kind === "episode") {
		api
			.GET("/api/v1/titles/{id}/next", { params: { path: { id: title.id } } })
			.then(({ data }) => {
				next = data;
			})
			.catch(() => undefined);
	}
	void open(start);
	wake();
	return () => {
		document.removeEventListener("fullscreenchange", fullscreenChange);
		video?.textTracks.removeEventListener("addtrack", tracksAdded);
	};
});

onDestroy(() => {
	opening++;
	clearTimeout(restTimer);
	void reporter?.stop();
	detach();
});
</script>

<svelte:window
	onkeydown={keydown}
	onpagehide={() => void reporter?.stop(true)}
/>

<svelte:head><title>{heading} · Photon</title></svelte:head>

<section
	bind:this={root}
	class={[
		"fixed inset-0 overflow-hidden bg-black select-none",
		chromeHidden && "cursor-none",
	]}
	aria-label="Player"
	onpointermove={wake}
	onpointerdown={wake}
	onfocusin={wake}
>
	<!-- svelte-ignore a11y_media_has_caption -->
	<!-- biome-ignore lint/a11y/useMediaCaption: subtitles arrive as tracks once chosen, or as the stream's own renditions -->
	<video
		bind:this={video}
		class="size-full object-contain"
		playsinline
		preload="auto"
		onclick={toggle}
		ondblclick={toggleFullscreen}
		onloadedmetadata={() => {
			if (pendingSeek !== undefined && video) video.currentTime = pendingSeek;
			pendingSeek = undefined;
		}}
		ontimeupdate={() => {
			if (video && pendingSeek === undefined) time = video.currentTime;
		}}
		onprogress={() => {
			if (video?.buffered.length)
				bufferedTo = offset + video.buffered.end(video.buffered.length - 1);
		}}
		onplay={() => {
			paused = false;
			wake();
		}}
		onplaying={() => {
			waiting = false;
			reporter?.playing();
		}}
		onpause={() => {
			paused = true;
			reporter?.paused();
		}}
		oncanplay={() => {
			waiting = false;
		}}
		onwaiting={() => {
			waiting = true;
		}}
		onseeked={() => reporter?.seeked()}
		onended={() => {
			const parts = playback?.parts ?? [];
			if (part < parts.length - 1) {
				load(part + 1, parts[part + 1].offset_ms / 1000);
				void video?.play();
			} else if (next && !upNextHidden && prefs.autoplay) playNext();
		}}
		onerror={() => {
			if (playback?.method === "direct" && !convert) {
				convert = true;
				reopen();
			} else if (playback) {
				refusal = {
					message: "This browser couldn't play the stream.",
					reasons: [],
				};
			}
		}}
		onvolumechange={() => {
			if (!video) return;
			volume = video.volume;
			muted = video.muted;
		}}
		onratechange={() => {
			if (video) rate = video.playbackRate;
		}}
	>
		{#if trackSrc}
			<track kind="subtitles" src={trackSrc} label={subtitle?.label} default>
		{/if}
	</video>

	{#if waiting && !refusal}
		<div class="pointer-events-none absolute inset-0 grid place-items-center">
			<LoaderIcon class="text-ink size-12 animate-spin" aria-hidden="true" />
			<span class="sr-only" role="status">Loading</span>
		</div>
	{/if}

	{#if refusal}
		<div class="absolute inset-0 grid place-items-center bg-black/80 p-6">
			<div role="alert" class="grid max-w-md gap-4 text-center">
				<h1 class="title">Can't play this here</h1>
				<p class="text-ink">{refusal.message}</p>
				{#if refusal.reasons.length}
					<ul class="text-ink-2 list-disc text-left text-sm">
						{#each refusal.reasons as reason (reason)}
							<li>{reasons[reason]}</li>
						{/each}
					</ul>
				{/if}
				<div class="flex justify-center gap-2">
					<Button onclick={reopen}>Try again</Button>
					<Button variant="outline" href="/titles/{title.id}">Back</Button>
				</div>
			</div>
		</div>
	{/if}

	<div
		class={[
			"pointer-events-none absolute inset-0 flex flex-col justify-between transition-opacity duration-300",
			chromeHidden && "opacity-0",
		]}
	>
		<header
			class="pointer-events-auto flex items-center gap-3 bg-linear-to-b from-black/80 to-transparent p-3 pb-12 sm:p-5 sm:pb-16"
		>
			<Button
				href="/titles/{title.id}"
				variant="ghost"
				size="icon"
				aria-label="Back to {title.title}"
			>
				<ArrowLeftIcon class="size-5" />
			</Button>
			<h1 class="font-heading text-ink truncate text-base font-bold sm:text-lg">
				{heading}
			</h1>
		</header>

		<div
			class="pointer-events-auto flex flex-col gap-2 bg-linear-to-t from-black/85 to-transparent px-3 pt-16 pb-3 sm:px-5 sm:pb-5"
		>
			<div class="flex justify-end gap-3">
				{#if upNext && next}
					<UpNext
						card={next}
						{paused}
						autoplay={prefs.autoplay}
						onplay={playNext}
						ondismiss={() => {
							upNextHidden = true;
						}}
					/>
				{:else if marker && skipping(marker.kind, prefs) === "button"}
					<Button
						variant="outline"
						class="bg-black/60"
						onclick={() => skip(marker)}
					>
						{skips[marker.kind]}
					</Button>
				{/if}
			</div>

			<SeekBar
				{position}
				{duration}
				buffered={bufferedTo}
				chapters={version?.chapters}
				trickplay={version?.trickplay}
				onseek={seek}
			/>

			<div class="flex items-center gap-1 sm:gap-2">
				<Button
					variant="ghost"
					size="icon"
					aria-label={paused ? "Play" : "Pause"}
					onclick={toggle}
				>
					{#if paused}
						<PlayIcon class="size-5" />
					{:else}
						<PauseIcon class="size-5" />
					{/if}
				</Button>
				<Button
					variant="ghost"
					size="icon"
					aria-label="Back {step} seconds"
					onclick={() => seek(position - step)}
				>
					<BackIcon class="size-5" />
				</Button>
				<Button
					variant="ghost"
					size="icon"
					aria-label="Forward {step} seconds"
					onclick={() => seek(position + step)}
				>
					<ForwardIcon class="size-5" />
				</Button>
				<Button
					variant="ghost"
					size="icon"
					aria-label={muted ? "Unmute" : "Mute"}
					onclick={() => {
						if (video) video.muted = !video.muted;
					}}
				>
					{#if muted || volume === 0}
						<MuteIcon class="size-5" />
					{:else}
						<VolumeIcon class="size-5" />
					{/if}
				</Button>
				<input
					type="range"
					min="0"
					max="1"
					step="0.05"
					value={muted ? 0 : volume}
					aria-label="Volume"
					class="accent-ink hidden w-24 sm:block"
					oninput={(e) => {
						if (!video) return;
						video.volume = Number(e.currentTarget.value);
						video.muted = false;
					}}
				>
				<span class="text-ink ml-1 font-mono text-xs tabular-nums sm:text-sm">
					{clock(position)}
					/ {clock(duration)}
				</span>

				<div class="ml-auto flex items-center gap-1 sm:gap-2">
					{#if subtitleChoices.length}
						<Button
							variant="ghost"
							size="icon"
							aria-label="Subtitles"
							aria-pressed={!!subtitleKey}
							onclick={toggleSubtitles}
						>
							{#if subtitleKey}
								<CaptionsIcon class="size-5" />
							{:else}
								<CaptionsOffIcon class="size-5" />
							{/if}
						</Button>
					{/if}
					<Menu.Root bind:open={menuOpen}>
						<Menu.Trigger>
							{#snippet child({
								props,
							})}
								<Button
									{...props}
									variant="ghost"
									size="icon"
									aria-label="Settings"
								>
									<SettingsIcon class="size-5" />
								</Button>
							{/snippet}
						</Menu.Trigger>
						<Menu.Content
							portalProps={{ to: root }}
							side="top"
							align="end"
							class="w-56"
						>
							{#if audioStreams.length > 1}
								<Menu.Sub>
									<Menu.SubTrigger>Audio</Menu.SubTrigger>
									<Menu.SubContent
										portalProps={{ to: root }}
										class="max-h-80 overflow-y-auto"
									>
										<Menu.RadioGroup
											value={String(playingAudio)}
											onValueChange={(v) => chooseAudio(Number(v))}
										>
											{#each audioStreams as s (s.index)}
												<Menu.RadioItem value={String(s.index)}
													>{audioLabel(s)}</Menu.RadioItem
												>
											{/each}
										</Menu.RadioGroup>
									</Menu.SubContent>
								</Menu.Sub>
							{/if}
							{#if subtitleChoices.length}
								<Menu.Sub>
									<Menu.SubTrigger>Subtitles</Menu.SubTrigger>
									<Menu.SubContent
										portalProps={{ to: root }}
										class="max-h-80 overflow-y-auto"
									>
										<Menu.RadioGroup
											value={subtitleKey ?? "off"}
											onValueChange={chooseSubtitle}
										>
											<Menu.RadioItem value="off">Off</Menu.RadioItem>
											{#each subtitleChoices as c (c.key)}
												<Menu.RadioItem value={c.key}>{c.label}</Menu.RadioItem>
											{/each}
										</Menu.RadioGroup>
									</Menu.SubContent>
								</Menu.Sub>
							{/if}
							<Menu.Sub>
								<Menu.SubTrigger>Quality</Menu.SubTrigger>
								<Menu.SubContent
									portalProps={{ to: root }}
									class="max-h-80 overflow-y-auto"
								>
									<Menu.RadioGroup
										value={String(quality)}
										onValueChange={(v) => chooseQuality(Number(v))}
									>
										{#each qualities as kbps (kbps)}
											<Menu.RadioItem value={String(kbps)}
												>{bitrate(kbps)}</Menu.RadioItem
											>
										{/each}
									</Menu.RadioGroup>
								</Menu.SubContent>
							</Menu.Sub>
							<Menu.Sub>
								<Menu.SubTrigger>Speed</Menu.SubTrigger>
								<Menu.SubContent portalProps={{ to: root }}>
									<Menu.RadioGroup
										value={String(rate)}
										onValueChange={(v) => chooseSpeed(Number(v))}
									>
										{#each speeds as s (s)}
											<Menu.RadioItem value={String(s)}
												>{s === 1 ? "Normal" : `${s}×`}</Menu.RadioItem
											>
										{/each}
									</Menu.RadioGroup>
								</Menu.SubContent>
							</Menu.Sub>
							{#if version?.chapters?.length}
								<Menu.Sub>
									<Menu.SubTrigger>Chapters</Menu.SubTrigger>
									<Menu.SubContent
										portalProps={{ to: root }}
										class="max-h-80 overflow-y-auto"
									>
										{#each version.chapters as c, i (i)}
											<Menu.Item onSelect={() => seek(c.start_ms / 1000)}>
												<span class="truncate"
													>{c.title || `Chapter ${i + 1}`}</span
												>
												<Menu.Shortcut
													>{clock(c.start_ms / 1000)}</Menu.Shortcut
												>
											</Menu.Item>
										{/each}
									</Menu.SubContent>
								</Menu.Sub>
							{/if}
							{#if playback}
								<Menu.Separator />
								<Menu.Item
									onSelect={() => {
										frames = video?.getVideoPlaybackQuality();
										infoOpen = true;
									}}
								>
									<InfoIcon />
									Playback info
								</Menu.Item>
							{/if}
						</Menu.Content>
					</Menu.Root>
					{#if pipAvailable}
						<Button
							variant="ghost"
							size="icon"
							aria-label="Picture in picture"
							onclick={togglePip}
						>
							<PipIcon class="size-5" />
						</Button>
					{/if}
					<Button
						variant="ghost"
						size="icon"
						aria-label={fullscreen ? "Exit full screen" : "Full screen"}
						onclick={toggleFullscreen}
					>
						{#if fullscreen}
							<MinimizeIcon class="size-5" />
						{:else}
							<MaximizeIcon class="size-5" />
						{/if}
					</Button>
				</div>
			</div>
		</div>
	</div>

	{#if playback}
		<PlaybackInfo
			bind:open={infoOpen}
			{playback}
			{version}
			quality={frames}
			portal={root}
		/>
	{/if}
</section>
