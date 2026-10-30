<script lang="ts">
import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
import CaptionsIcon from "@lucide/svelte/icons/captions";
import CaptionsOffIcon from "@lucide/svelte/icons/captions-off";
import CastIcon from "@lucide/svelte/icons/cast";
import LoaderIcon from "@lucide/svelte/icons/loader-circle";
import MaximizeIcon from "@lucide/svelte/icons/maximize";
import MinimizeIcon from "@lucide/svelte/icons/minimize";
import PauseIcon from "@lucide/svelte/icons/pause";
import PipIcon from "@lucide/svelte/icons/picture-in-picture-2";
import PlayIcon from "@lucide/svelte/icons/play";
import BackIcon from "@lucide/svelte/icons/rotate-ccw";
import ForwardIcon from "@lucide/svelte/icons/rotate-cw";
import PreviousIcon from "@lucide/svelte/icons/skip-back";
import NextIcon from "@lucide/svelte/icons/skip-forward";
import VolumeIcon from "@lucide/svelte/icons/volume-2";
import MuteIcon from "@lucide/svelte/icons/volume-x";
import type Hls from "hls.js";
import { onMount, untrack } from "svelte";
import { on } from "svelte/events";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { artworkURL } from "#lib/artwork.js";
import { Button } from "#lib/components/ui/button/index.js";
import { fullTitle, timecode } from "#lib/format.js";
import { cueStyle, keptAppearance } from "#lib/player/appearance.js";
import { castContext, castRequest } from "#lib/player/cast.js";
import { Casting } from "#lib/player/casting.svelte.js";
import { SubtitleDrawing } from "#lib/player/drawing.svelte.js";
import { type Preferences, skipping } from "#lib/player/preferences.js";
import {
	browserProfile,
	type Capabilities,
	castProfile,
	probe,
} from "#lib/player/profile.js";
import { ProgressReporter, type Report } from "#lib/player/progress.js";
import { type Advance, isDue, runAfter } from "#lib/player/stillwatching.js";
import {
	beside,
	choices,
	needsReplay,
	startingSubtitle,
	wants,
} from "#lib/player/subtitles.js";
import { type Fit, fits, skips } from "#lib/player/words.js";
import { queued } from "#lib/queue.js";
import { vocabulary } from "#lib/vocabulary.js";
import { goto } from "$app/navigation";
import EpisodeList from "./EpisodeList.svelte";
import PlaybackInfo from "./PlaybackInfo.svelte";
import SeekBar from "./SeekBar.svelte";
import SettingsMenu from "./SettingsMenu.svelte";
import StillWatching from "./StillWatching.svelte";
import UpNext from "./UpNext.svelte";

type Schemas = components["schemas"];

// One title playing: asks the server how this browser can have it, plays what
// it answers (the file itself, or HLS through hls.js or the browser's own),
// says where it has got to, and asks again when a choice needs another stream.
let {
	title,
	prefs,
	start,
	version: askedVersion,
	audio: askedAudio,
	subtitle: askedSubtitle,
	queued: at,
	run = 1,
}: {
	title: Schemas["TitlePage"];
	prefs: Preferences;
	start: number;
	version?: string;
	audio?: number;
	subtitle?: number | "off";
	// Where the title is in the queue it was opened from.
	queued?: number;
	// The episodes played one after another with nothing pressed, this one and
	// the one the reader started among them.
	run?: number;
} = $props();
const words = vocabulary();

const api = client();
const step = 10;
const nudge = 5;
const restAfter = 3000;

let root = $state<HTMLElement>();
let video = $state<HTMLVideoElement>();
let hls: Hls | undefined;
let caps: Capabilities | undefined;
let reporter: ProgressReporter | undefined;
let opening = 0;
let pendingSeek: number | undefined;

let playback = $state.raw<Schemas["Playback"]>();
let refusal = $state.raw<{
	message: string;
	reasons: Schemas["TranscodeReason"][];
}>();
const version = $derived(
	title.versions?.find(
		(v) => v.id === (playback?.version_id ?? askedVersion),
	) ?? title.versions?.[0],
);
// The choices start as the address asked, else as the server chose them for
// this profile, and are the reader's from then on.
const startVersion = untrack(() => version);
let audio = $derived(
	askedAudio ?? startVersion?.default_audio_stream ?? undefined,
);
let subtitleKey = $derived(
	startVersion && startingSubtitle(startVersion, askedSubtitle),
);
let lastSubtitle = $state<string>();
const drawing = new SubtitleDrawing(() => video);
const appearance = keptAppearance();
let quality = $derived(prefs.max_bitrate_kbps);
// The file as it is would not play here after all; the server converts it.
let convert = $state(false);
let part = $state(0);
let time = $state(0);
// What the video element says of itself, bound to it.
let buffered = $state<{ start: number; end: number }[]>([]);
let readyState = $state(0);
let paused = $state(true);
// The volume is the browser's, as it was last left, where storage allows.
const volumeKey = "photon:volume";
const kept = (() => {
	try {
		return JSON.parse(localStorage.getItem(volumeKey) ?? "null");
	} catch {
		return null;
	}
})();
let volume = $state(
	typeof kept?.volume === "number" && kept.volume >= 0 && kept.volume <= 1
		? kept.volume
		: 1,
);
let muted = $state(kept?.muted === true);
$effect(() => {
	try {
		localStorage.setItem(volumeKey, JSON.stringify({ volume, muted }));
	} catch {
		// Not kept where storage is refused.
	}
});
let rate = $state(1);
let fit = $state<Fit>("contain");
let fullscreenElement = $state<Element | null>(null);
const pipAvailable = document.pictureInPictureEnabled;
// Whether there is a screen to play on through the browser: AirPlay in
// Safari, Cast in Chrome on Android. It can only send a stream it plays itself,
// not one hls.js feeds it, so Google's sender stands in for it where it finds
// a Chromecast.
let remoteAvailable = $state(false);
const chromecast = new Casting();
const casting = $derived(chromecast.session);
let resting = $state(false);
let menuOpen = $state(false);
let infoOpen = $state(false);
let episodesOpen = $state(false);
let frames = $state.raw<VideoPlaybackQuality>();
let next = $state.raw<Schemas["Card"]>();
let upNextHidden = $state(false);
let episodes = $state(untrack(() => run));
// Whether the player has stopped to ask if anybody is still there.
let asking = $state(false);
let restTimer: ReturnType<typeof setTimeout> | undefined;
// When a broken stream was last opened again. One that breaks again within
// mendWithin of that is not mended by opening it once more.
let mendedAt = Number.NEGATIVE_INFINITY;
const mendWithin = 60_000;

const duration = $derived((version?.duration_ms ?? 0) / 1000);
const offset = $derived((playback?.parts?.[part]?.offset_ms ?? 0) / 1000);
const position = $derived(offset + time);
const bufferedTo = $derived(
	casting ? position : offset + (buffered.at(-1)?.end ?? 0),
);
const waiting = $derived(
	casting ? chromecast.waiting : readyState < HTMLMediaElement.HAVE_FUTURE_DATA,
);
const halted = $derived(casting ? chromecast.paused : paused);
const fullscreen = $derived(!!fullscreenElement);
const subtitleChoices = $derived(version ? choices(version) : []);
const subtitle = $derived(subtitleChoices.find((c) => c.key === subtitleKey));
const burned = $derived(
	playback?.video?.burned_subtitle != null ||
		playback?.video?.burned_subtitle_file != null,
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
const upNext = $derived(
	next &&
		!upNextHidden &&
		duration > 0 &&
		position >= (credits ? credits.start_ms / 1000 : duration - 30),
);
const chromeHidden = $derived(
	resting &&
		!halted &&
		!casting &&
		!menuOpen &&
		!infoOpen &&
		!episodesOpen &&
		!refusal,
);
const heading = $derived(fullTitle(title, title.show?.title));

function send(id: string) {
	return (r: Report) => {
		// A stop may go as the page closes, so it is kept alive past it.
		const call =
			r.kind === "stop"
				? api.POST("/api/v1/playbacks/{id}/stop", {
						params: { path: { id } },
						body: { position_ms: r.position_ms },
						keepalive: r.keepalive,
					})
				: api.POST("/api/v1/playbacks/{id}/progress", {
						params: { path: { id } },
						body: { position_ms: r.position_ms, state: r.state, ...tracks() },
					});
		return call.catch(() => undefined);
	};
}

// The tracks playing, so the server plays the title with them again.
function tracks(): Pick<
	Schemas["PlaybackProgress"],
	"audio_stream" | "subtitle_stream" | "subtitle_file"
> {
	return {
		audio_stream: playingAudio,
		subtitle_stream: subtitle ? subtitle.stream : -1,
		subtitle_file: subtitle?.id,
	};
}

// Opens a playback at a moment, stopping the one before it first so the
// server is never transcoding twice for one reader.
async function open(at: number) {
	const mine = ++opening;
	refusal = undefined;
	const previous = reporter;
	reporter = undefined;
	await previous?.stop();
	detach();
	const session = chromecast.join();
	caps ??= await probe();
	const asked = wants(subtitle);
	const profile = session
		? castProfile(quality)
		: browserProfile(caps, quality);
	// A plain file the browser cannot read is carried in HLS, so a profile that
	// wants one opens nothing as it is.
	if (asked.viaHLS || convert) profile.containers = [];
	const { data, error } = await api.POST("/api/v1/titles/{id}/play", {
		params: { path: { id: title.id } },
		body: {
			version_id: playback?.version_id ?? askedVersion,
			audio_stream: audio,
			subtitle_stream: asked.subtitle_stream,
			subtitle_file: asked.subtitle_file,
			profile,
			start_ms: Math.round(at * 1000),
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
	if (session) await castOn(session, data, at, mine);
	else await attach(data, at, mine);
}

// Plays a playback on the Chromecast, which fetches its playlist itself.
async function castOn(
	session: cast.framework.CastSession,
	p: Schemas["Playback"],
	at: number,
	mine: number,
) {
	// The server answers HLS to a profile that opens no file as it is.
	if (!p.playlist) {
		refusal = { message: "The server sent no stream to cast.", reasons: [] };
		return;
	}
	part = 0;
	time = at;
	const art = title.artwork?.thumb?.[0] ?? title.artwork?.poster?.[0];
	const played = await chromecast.play(
		session,
		castRequest(p.playlist, at, {
			title: heading,
			picture: art && artworkURL(art, 640),
		}),
		() => {
			if (mine === opening) ended();
		},
	);
	if (mine !== opening) return;
	if (!played) {
		refusal = {
			message: "The Chromecast couldn't play the stream.",
			reasons: [],
		};
		return;
	}
	await showSubtitle();
}

function reopen() {
	void open(position);
}

// Opens a stream that broke again where it was, as the server may have let the
// playback go or its connection dropped, unless that was just tried.
function mend(message: string) {
	if (performance.now() - mendedAt < mendWithin) {
		refusal = { message, reasons: [] };
		return;
	}
	mendedAt = performance.now();
	reopen();
}

async function attach(p: Schemas["Playback"], at: number, mine: number) {
	if (!video || !caps) return;
	if (p.playlist) {
		part = 0;
		time = at;
		if (caps.nativeHLS) {
			pendingSeek = at;
			video.src = p.playlist;
		} else {
			const { default: HLS } = await import("hls.js");
			// Opened again while hls.js loaded: that playback has the video.
			if (mine !== opening) return;
			const player = new HLS({ startPosition: at });
			hls = player;
			hls.on(HLS.Events.SUBTITLE_TRACKS_UPDATED, () => void showSubtitle());
			// A picture the decoder stopped on is mended in place first, as
			// Jellyfin's is, without asking the server again.
			let decoderMended = false;
			hls.on(HLS.Events.ERROR, (_, e) => {
				if (!e.fatal) return;
				if (e.type === HLS.ErrorTypes.MEDIA_ERROR && !decoderMended) {
					decoderMended = true;
					player.recoverMediaError();
				} else mend(`The stream stopped: ${e.details}.`);
			});
			hls.loadSource(p.playlist);
			hls.attachMedia(video);
		}
	} else {
		load(partAt(at), at);
	}
	await showSubtitle();
	video.play().catch(() => undefined);
}

function detach() {
	hls?.destroy();
	hls = undefined;
	drawing.clear();
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
	const to = Math.min(Math.max(seconds, 0), duration || seconds);
	if (casting && chromecast.seek(to)) {
		time = to;
		reporter?.seeked();
		return;
	}
	if (!video) return;
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

// Shows the subtitle chosen in the playback there is: styled text drawn by
// JASSUB, pictures drawn by libbitsub, plain text beside the version or read
// out of it as a track, or one of the HLS playlist's renditions.
async function showSubtitle() {
	if (!playback || !video) return;
	const n = burned ? -1 : (subtitle?.rendition ?? -1);
	if (casting) {
		chromecast.subtitle(n);
		return;
	}
	const direct = playback.method === "direct";
	await drawing.draw(beside(subtitle, playback), direct);
	if (direct) return;
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
	const chosen = subtitleChoices.find((c) => c.key === key);
	if (chosen) lastSubtitle = key;
	subtitleKey = chosen?.key;
	drawing.delay = 0;
	if (playback && needsReplay(chosen, playback)) reopen();
	else void showSubtitle();
}

function toggleSubtitles() {
	if (subtitleKey) chooseSubtitle("off");
	else chooseSubtitle(lastSubtitle ?? subtitleChoices[0]?.key ?? "off");
}

function chooseAudio(index: number) {
	if (index === playingAudio) return;
	audio = index;
	reopen();
}

function chooseQuality(kbps: number) {
	if (kbps === quality) return;
	quality = kbps;
	// The quality chosen here is the profile's from now on, as Jellyfin's is.
	api
		.PATCH("/api/v1/profile/preferences", { body: { max_bitrate_kbps: kbps } })
		.catch(() => undefined);
	reopen();
}

// A new source starts at the default rate, so it is the chosen one too.
function chooseSpeed(r: number) {
	if (video) video.defaultPlaybackRate = r;
	rate = r;
}

function toggle() {
	if (casting) {
		chromecast.toggle();
		return;
	}
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

function playNext(by: Advance) {
	if (!next) return;
	void goto(`/play/${next.id}`, {
		replace: true,
		state: {
			...(at === undefined ? {} : { queued: at + 1 }),
			run: runAfter(episodes, by),
		},
	});
}

// Anything the reader presses says somebody is there.
function present() {
	episodes = 1;
}

$effect(() => {
	if (
		asking ||
		prefs.still_watching !== "ask" ||
		title.kind !== "episode" ||
		!isDue(episodes, position)
	)
		return;
	asking = true;
	if (!untrack(() => halted)) toggle();
});

function keepWatching() {
	present();
	asking = false;
	if (halted) toggle();
}

// Whether there is a title before this one to go back to: the queue's, or
// the episode before in its show.
const hasPrevious = $derived(
	at === undefined ? title.kind === "episode" && !!title.show : at > 0,
);

// Plays the title before this one, as the queue or the show has them, or
// starts this one again where it is the first.
async function playPrevious() {
	let before: Schemas["Card"] | undefined;
	if (at !== undefined) before = queued(at - 1);
	else if (title.show) {
		const { data } = await api.GET("/api/v1/titles/{id}/playable", {
			params: { path: { id: title.show.id } },
		});
		const items = data?.items ?? [];
		before = items[items.findIndex((c) => c.id === title.id) - 1];
	}
	if (!before) {
		seek(0);
		return;
	}
	void goto(`/play/${before.id}`, {
		replace: true,
		state: at === undefined ? {} : { queued: at - 1 },
	});
}

function skip(m: Schemas["MarkerRef"], by: Advance) {
	if (next && m.end_ms / 1000 >= duration - 1) playNext(by);
	else seek(m.end_ms / 1000);
}

// Skips a marker the reader skips by itself, as the playback passes into it.
function passed() {
	if (!marker || skipping(marker.kind, prefs) !== "skip") return;
	if (skippedAt.has(marker.start_ms)) return;
	skippedAt.add(marker.start_ms);
	skip(marker, "itself");
}

function ended() {
	const parts = playback?.parts ?? [];
	if (!casting && part < parts.length - 1) {
		load(part + 1, parts[part + 1].offset_ms / 1000);
		void video?.play();
	} else if (next && !upNextHidden && prefs.next_episode === "play")
		playNext("itself");
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
	if (
		event.metaKey ||
		event.ctrlKey ||
		event.altKey ||
		asking ||
		infoOpen ||
		menuOpen ||
		episodesOpen
	)
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
	else if (key === "ArrowUp" && !control) volume = Math.min(volume + 0.05, 1);
	else if (key === "ArrowDown" && !control) volume = Math.max(volume - 0.05, 0);
	else if (key === "m") muted = !muted;
	else if (key === "f") toggleFullscreen();
	else if (key === "c") toggleSubtitles();
	else if (key === "N" && next) playNext("reader");
	else if (key === "P" && hasPrevious) void playPrevious();
	else if (/^[0-9]$/.test(key) && duration) seek((duration * Number(key)) / 10);
	else handled = false;
	if (handled) {
		event.preventDefault();
		present();
		wake();
	}
}

// The title, and the keys and controls of the system and of headphones, are
// the player's while it is open.
$effect(() => {
	if (!("mediaSession" in navigator)) return;
	const art = title.artwork?.thumb?.[0] ?? title.artwork?.poster?.[0];
	navigator.mediaSession.metadata = new MediaMetadata({
		title: title.title,
		artist: title.show?.title ?? "",
		album: title.season?.title ?? "",
		artwork: art
			? [{ src: new URL(artworkURL(art, 640), location.href).href }]
			: [],
	});
	const handlers: [MediaSessionAction, MediaSessionActionHandler | null][] = [
		["play", () => halted && toggle()],
		["pause", () => !halted && toggle()],
		["seekbackward", (d) => seek(position - (d.seekOffset ?? step))],
		["seekforward", (d) => seek(position + (d.seekOffset ?? step))],
		["seekto", (d) => seek(d.seekTime ?? position)],
		["nexttrack", next ? () => playNext("reader") : null],
		["previoustrack", hasPrevious ? () => void playPrevious() : null],
	];
	for (const [action, handler] of handlers) {
		try {
			navigator.mediaSession.setActionHandler(
				action,
				handler &&
					((d) => {
						present();
						handler(d);
					}),
			);
		} catch {
			// An action this browser has not heard of.
		}
	}
	return () => {
		navigator.mediaSession.metadata = null;
		for (const [action] of handlers) {
			try {
				navigator.mediaSession.setActionHandler(action, null);
			} catch {}
		}
	};
});

$effect(() => {
	if (!("mediaSession" in navigator) || !duration) return;
	navigator.mediaSession.setPositionState({
		duration,
		position: Math.min(Math.max(position, 0), duration),
		playbackRate: rate,
	});
});

$effect(() => {
	const remote = video?.remote;
	if (!remote) return;
	const watching: Promise<number | undefined> = remote
		.watchAvailability((available) => {
			remoteAvailable = available;
		})
		.catch(() => undefined);
	return () => {
		void watching.then((id) => {
			if (id !== undefined) remote.cancelWatchAvailability(id).catch(() => {});
		});
	};
});

// The browser's own HLS adds the playlist's subtitles as it reads them.
function tracksAdded() {
	if (playback?.playlist && !hls) void showSubtitle();
}

onMount(() => {
	if (at !== undefined) next = queued(at + 1);
	else if (title.kind === "episode") {
		api
			.GET("/api/v1/titles/{id}/next", { params: { path: { id: title.id } } })
			.then(({ data }) => {
				next = data;
			})
			.catch(() => undefined);
	}
	let unwatch: (() => void) | undefined;
	let gone = false;
	void castContext().then((context) => {
		if (context && !gone)
			unwatch = chromecast.watch(context, {
				moved: (seconds) => {
					time = seconds;
					passed();
				},
				playing: () => reporter?.playing(),
				paused: () => reporter?.paused(),
				changed: reopen,
			});
	});
	void open(start);
	wake();
	return () => {
		opening++;
		gone = true;
		unwatch?.();
		clearTimeout(restTimer);
		void reporter?.stop();
		chromecast.stop();
		detach();
	};
});
</script>

<!-- A page the browser kept and brought back has stopped its playback on
	leaving; it opens another where it is. -->
<svelte:window
	onkeydown={keydown}
	onpagehide={() => void reporter?.stop(true)}
	onpageshow={(e) => {
		if (e.persisted) reopen();
	}}
/>

<svelte:document bind:fullscreenElement />

{#snippet clock(place: string)}
	<span
		class={[
			"text-ink font-mono text-xs whitespace-nowrap tabular-nums sm:text-sm",
			place,
		]}
	>
		{timecode(position * 1000)}
		/ {timecode(duration * 1000)}
	</span>
{/snippet}

<svelte:head><title>{heading} · Photon</title></svelte:head>

<section
	bind:this={root}
	class={[
		"fixed inset-0 overflow-hidden bg-black select-none",
		chromeHidden && "cursor-none",
	]}
	aria-label="Player"
	onpointermove={wake}
	onpointerdown={() => {
		present();
		wake();
	}}
	onfocusin={wake}
>
	<!-- svelte-ignore a11y_media_has_caption -->
	<!-- biome-ignore lint/a11y/useMediaCaption: subtitles arrive as tracks once chosen, or as the stream's own renditions -->
	<video
		bind:this={video}
		bind:paused
		bind:volume
		bind:muted
		bind:playbackRate={rate}
		bind:buffered
		bind:readyState
		{@attach (v) => on(v.textTracks, "addtrack", tracksAdded)}
		class={["size-full", fits[fit].class]}
		style={cueStyle(appearance)}
		playsinline
		preload="auto"
		onclick={toggle}
		ondblclick={toggleFullscreen}
		onloadedmetadata={() => {
			if (pendingSeek !== undefined && video) video.currentTime = pendingSeek;
			pendingSeek = undefined;
		}}
		ontimeupdate={() => {
			if (!video || pendingSeek !== undefined) return;
			time = video.currentTime;
			drawing.follow();
			passed();
		}}
		onplay={wake}
		onplaying={() => reporter?.playing()}
		onpause={() => reporter?.paused()}
		onseeked={() => reporter?.seeked()}
		onended={ended}
		onerror={() => {
			if (playback?.method === "direct" && !convert) {
				convert = true;
				reopen();
			} else if (playback) mend("This browser couldn't play the stream.");
		}}
	>
		{#if drawing.track}
			<track
				kind="subtitles"
				src={drawing.track}
				label={subtitle?.label}
				default
			>
		{/if}
	</video>

	{#if casting}
		<div
			class="text-ink pointer-events-none absolute inset-0 grid place-content-center justify-items-center gap-3 p-6 text-center"
		>
			<CastIcon class="size-12" aria-hidden="true" />
			<p class="font-heading text-lg">Playing on {chromecast.device}</p>
		</div>
	{/if}

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
							<li>{words.reasons[reason].description}</li>
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
						paused={halted}
						autoplay={prefs.next_episode === "play"}
						onplay={playNext}
						ondismiss={() => {
							upNextHidden = true;
						}}
					/>
				{:else if marker && skipping(marker.kind, prefs) === "ask"}
					<Button
						variant="outline"
						class="bg-black/60"
						onclick={() => skip(marker, "reader")}
					>
						{skips[marker.kind]}
					</Button>
				{/if}
			</div>

			<!-- A phone's bar has no room for the time beside its buttons. -->
			{@render clock("sm:hidden")}
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
					aria-label={halted ? "Play" : "Pause"}
					onclick={toggle}
				>
					{#if halted}
						<PlayIcon class="size-5" />
					{:else}
						<PauseIcon class="size-5" />
					{/if}
				</Button>
				{#if hasPrevious}
					<Button
						variant="ghost"
						size="icon"
						class="hidden sm:inline-flex"
						aria-label="Previous"
						onclick={() => void playPrevious()}
					>
						<PreviousIcon class="size-5" />
					</Button>
				{/if}
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
				{#if next}
					<Button
						variant="ghost"
						size="icon"
						class="hidden sm:inline-flex"
						aria-label="Next: {next.title}"
						onclick={() => playNext("reader")}
					>
						<NextIcon class="size-5" />
					</Button>
				{/if}
				{#if title.show}
					<EpisodeList
						bind:open={episodesOpen}
						show={title.show}
						current={title.id}
						portal={root}
					/>
				{/if}
				<!-- A Chromecast's volume is the TV's, set by its own remote. -->
				{#if !casting}
					<Button
						variant="ghost"
						size="icon"
						aria-label={muted ? "Unmute" : "Mute"}
						onclick={() => (muted = !muted)}
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
							volume = Number(e.currentTarget.value);
							muted = false;
						}}
					>
				{/if}
				{@render clock("ml-1 hidden sm:inline")}

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
					<SettingsMenu
						bind:open={menuOpen}
						bind:fit
						portal={root}
						{version}
						audio={playingAudio}
						subtitles={subtitleChoices}
						subtitle={subtitleKey}
						{drawing}
						timing={!!subtitle && !burned && !casting}
						{quality}
						{rate}
						casting={!!casting}
						info={!!playback}
						onaudio={chooseAudio}
						onsubtitle={chooseSubtitle}
						onquality={chooseQuality}
						onspeed={chooseSpeed}
						onseek={seek}
						oninfo={() => {
							frames = video?.getVideoPlaybackQuality();
							infoOpen = true;
						}}
					/>
					{#if chromecast.devices}
						<Button
							variant="ghost"
							size="icon"
							aria-label={casting ? `Playing on ${chromecast.device}` : "Cast"}
							aria-pressed={!!casting}
							onclick={() =>
								cast.framework.CastContext.getInstance()
									.requestSession()
									.catch(() => {})}
						>
							<CastIcon class="size-5" />
						</Button>
					{:else if remoteAvailable}
						<Button
							variant="ghost"
							size="icon"
							aria-label="Play on another screen"
							onclick={() => video?.remote.prompt().catch(() => {})}
						>
							<CastIcon class="size-5" />
						</Button>
					{/if}
					{#if pipAvailable && !casting}
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
	{#if asking}
		<StillWatching
			{title}
			onkeep={keepWatching}
			onleave={() => void goto(`/titles/${title.id}`)}
		/>
	{/if}
</section>
