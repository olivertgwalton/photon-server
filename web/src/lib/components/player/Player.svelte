<script lang="ts">
import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
import CaptionsIcon from "@lucide/svelte/icons/captions";
import CaptionsOffIcon from "@lucide/svelte/icons/captions-off";
import CastIcon from "@lucide/svelte/icons/cast";
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
import PreviousIcon from "@lucide/svelte/icons/skip-back";
import NextIcon from "@lucide/svelte/icons/skip-forward";
import VolumeIcon from "@lucide/svelte/icons/volume-2";
import MuteIcon from "@lucide/svelte/icons/volume-x";
import type Hls from "hls.js";
import type JASSUB from "jassub";
import type { PgsRenderer, VobSubRenderer } from "libbitsub";
import { onMount, untrack } from "svelte";
import { on } from "svelte/events";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { artworkURL } from "#lib/artwork.js";
import { Button } from "#lib/components/ui/button/index.js";
import * as Menu from "#lib/components/ui/dropdown-menu/index.js";
import { bitrate, fullTitle, timecode } from "#lib/format.js";
import { cueStyle, keptAppearance } from "#lib/player/appearance.js";
import { castContext, castRequest, castSession } from "#lib/player/cast.js";
import { type Preferences, skipping } from "#lib/player/preferences.js";
import {
	browserProfile,
	type Capabilities,
	castProfile,
	probe,
} from "#lib/player/profile.js";
import { ProgressReporter, type Report } from "#lib/player/progress.js";
import {
	beside,
	choices,
	delayCues,
	needsReplay,
	startingSubtitle,
	wants,
	webVTT,
} from "#lib/player/subtitles.js";
import { qualities, skips } from "#lib/player/words.js";
import { queued } from "#lib/queue.js";
import { vocabulary } from "#lib/vocabulary.js";
import { goto } from "$app/navigation";
import EpisodeList from "./EpisodeList.svelte";
import PlaybackInfo from "./PlaybackInfo.svelte";
import SeekBar from "./SeekBar.svelte";
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
}: {
	title: Schemas["TitlePage"];
	prefs: Preferences;
	start: number;
	version?: string;
	audio?: number;
	subtitle?: number | "off";
	// Where the title is in the queue it was opened from.
	queued?: number;
} = $props();
const words = vocabulary();

const api = client();
const step = 10;
const nudge = 5;
const restAfter = 3000;
const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2];
// How the picture meets a screen of another shape: whole, cropped to fill it,
// or stretched to it.
const fits = {
	contain: { label: "Fit", class: "object-contain" },
	cover: { label: "Crop to fill", class: "object-cover" },
	fill: { label: "Stretch", class: "object-fill" },
};

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
// How much later than its file the subtitle shows, in seconds, as the reader
// set it; another subtitle starts on time.
let subtitleDelay = $state(0);
const delayed = new WeakMap<object, number>();
const appearance = keptAppearance();
let quality = $derived(prefs.max_bitrate_kbps);
// The file as it is would not play here after all; the server converts it.
let convert = $state(false);
let trackSrc = $state<string>();
let trackURL: string | undefined;
// The styled subtitle JASSUB draws over the video, by its address.
let styled: JASSUB | undefined;
let styledURL: string | undefined;
// The PGS or VobSub subtitle libbitsub draws over the video, by its address.
let pictures:
	| { renderer: PgsRenderer | VobSubRenderer; canvas: HTMLCanvasElement }
	| undefined;
let picturesURL: string | undefined;

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
let fit = $state<keyof typeof fits>("contain");
let fullscreenElement = $state<Element | null>(null);
const pipAvailable = document.pictureInPictureEnabled;
// Whether there is a screen to play on through the browser: AirPlay in
// Safari, Cast in Chrome on Android. It can only send a stream it plays itself,
// not one hls.js feeds it, so Google's sender stands in for it where it finds
// a Chromecast.
let remoteAvailable = $state(false);
// Whether a Chromecast is on the network, through Google's sender, and the
// session playing on one.
let castDevices = $state(false);
let casting = $state.raw<cast.framework.CastSession>();
let castPaused = $state(true);
let castWaiting = $state(false);
let remote:
	| {
			player: cast.framework.RemotePlayer;
			controller: cast.framework.RemotePlayerController;
	  }
	| undefined;
let resting = $state(false);
let menuOpen = $state(false);
let infoOpen = $state(false);
let episodesOpen = $state(false);
let frames = $state.raw<VideoPlaybackQuality>();
let next = $state.raw<Schemas["Card"]>();
let upNextHidden = $state(false);
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
	casting ? castWaiting : readyState < HTMLMediaElement.HAVE_FUTURE_DATA,
);
const halted = $derived(casting ? castPaused : paused);
const fullscreen = $derived(!!fullscreenElement);
const subtitleChoices = $derived(version ? choices(version) : []);
const subtitle = $derived(subtitleChoices.find((c) => c.key === subtitleKey));
const burned = $derived(
	playback?.video?.burned_subtitle != null ||
		playback?.video?.burned_subtitle_file != null,
);
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
	casting ??= castSession();
	const session = casting;
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
	castWaiting = true;
	const art = title.artwork?.thumb?.[0] ?? title.artwork?.poster?.[0];
	const failed = await session
		.loadMedia(
			castRequest(p.playlist, at, {
				title: heading,
				picture: art && artworkURL(art, 640),
			}),
		)
		.catch((code) => code ?? "failed");
	if (mine !== opening) return;
	if (failed) {
		refusal = {
			message: "The Chromecast couldn't play the stream.",
			reasons: [],
		};
		return;
	}
	const media = session.getMediaSession();
	media?.addUpdateListener(() => {
		if (
			mine === opening &&
			media.playerState === chrome.cast.media.PlayerState.IDLE &&
			media.idleReason === chrome.cast.media.IdleReason.FINISHED
		)
			ended();
	});
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
	void showStyled(undefined);
	void showPictures(undefined);
	clearTrack();
	if (video?.hasAttribute("src")) {
		video.removeAttribute("src");
		video.load();
	}
}

function clearTrack() {
	if (trackSrc?.startsWith("blob:")) URL.revokeObjectURL(trackSrc);
	trackSrc = undefined;
	trackURL = undefined;
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
	if (casting && remote) {
		remote.player.currentTime = to;
		remote.controller.seek();
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
	if (casting) {
		castSubtitle(casting);
		return;
	}
	const given = beside(subtitle, playback);
	const drawn = given?.kind === "styled" || given?.kind === "picture";
	void showStyled(given?.kind === "styled" ? given : undefined);
	void showPictures(given?.kind === "picture" ? given : undefined);
	if (playback.method === "direct") {
		const file = drawn ? undefined : given;
		if (file?.url === trackURL) return;
		clearTrack();
		trackURL = file?.url;
		if (!file) return;
		// A track inside the file is read out as WebVTT.
		if (file.codec === "webvtt" || file.stream != null) {
			trackSrc = file.url;
			return;
		}
		const srt = await fetch(file.url).then((r) => r.text());
		// Another file was chosen, or the playback closed, while this one came.
		if (trackURL !== file.url) return;
		trackSrc = URL.createObjectURL(
			new Blob([webVTT(srt)], { type: "text/vtt" }),
		);
		return;
	}
	const n = burned ? -1 : (subtitle?.rendition ?? -1);
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

// Shows the chosen subtitle on the Chromecast: the HLS playlist's rendition,
// which the receiver lists among its text tracks in the playlist's order.
function castSubtitle(session: cast.framework.CastSession) {
	const media = session.getMediaSession();
	if (!media) return;
	const n = burned ? -1 : (subtitle?.rendition ?? -1);
	const text = (media.media?.tracks ?? []).filter(
		(t) => t.type === chrome.cast.media.TrackType.TEXT,
	);
	const shown = text[n];
	media.editTracksInfo(
		new chrome.cast.media.EditTracksInfoRequest(shown ? [shown.trackId] : []),
		() => {},
		() => {},
	);
}

// Draws styled text over the video with JASSUB, with the fonts its file
// carries, or stops drawing it. JASSUB's worker and WebAssembly load the first
// time; one asked for after a later one is dropped.
async function showStyled(sub: Schemas["Subtitle"] | undefined) {
	if (sub?.url === styledURL) return;
	styledURL = sub?.url;
	void styled?.destroy();
	styled = undefined;
	if (!sub || !video) return;
	const absolute = (url: string) => new URL(url, location.href).href;
	const [{ default: JASSUB }, fonts] = await Promise.all([
		import("jassub"),
		sub.fonts
			? fetch(sub.fonts)
					.then((r) => r.json() as Promise<Schemas["Fonts"]>)
					.then((f) => f.fonts.map((font) => absolute(font.url)))
					.catch(() => [])
			: [],
	]);
	if (styledURL !== sub.url || !video) return;
	styled = new JASSUB({
		video,
		subUrl: absolute(sub.url),
		fonts,
		timeOffset: -subtitleDelay,
	});
}

// Draws PGS or VobSub over the video with libbitsub, or stops drawing it. The
// server hands VobSub in Matroska. The canvas sits just after the video, as
// JASSUB's does, so the controls stay above it.
async function showPictures(sub: Schemas["Subtitle"] | undefined) {
	if (sub?.url === picturesURL) return;
	picturesURL = sub?.url;
	pictures?.renderer.dispose();
	pictures?.canvas.remove();
	pictures = undefined;
	if (!sub || !video) return;
	const { PgsRenderer, VobSubRenderer } = await import("libbitsub");
	if (picturesURL !== sub.url || !video) return;
	const canvas = document.createElement("canvas");
	canvas.style.zIndex = "auto";
	video.after(canvas);
	const subUrl = new URL(sub.url, location.href).href;
	const renderer =
		sub.codec === "dvd_subtitle"
			? new VobSubRenderer({ video, canvas, subUrl, fileName: "track.mks" })
			: new PgsRenderer({ video, canvas, subUrl });
	renderer.timeOffset = -subtitleDelay;
	pictures = { renderer, canvas };
}

// Shows the subtitle as late as the reader set it: its cues moved, or the
// clock JASSUB and libbitsub draw by.
function delaySubtitle() {
	for (const t of video?.textTracks ?? []) {
		if (t.mode === "showing" && t.cues)
			delayCues(t.cues, subtitleDelay, delayed);
	}
	if (styled) styled.timeOffset = -subtitleDelay;
	if (pictures) pictures.renderer.timeOffset = -subtitleDelay;
}

function moveSubtitle(by: number) {
	subtitleDelay = Math.round((subtitleDelay + by) * 10) / 10;
	delaySubtitle();
}

// A choice in one of the settings menu's submenus leaves bits-ui's menu open,
// so each closes it.
function chooseSubtitle(key: string) {
	menuOpen = false;
	const chosen = subtitleChoices.find((c) => c.key === key);
	if (chosen) lastSubtitle = key;
	subtitleKey = chosen?.key;
	subtitleDelay = 0;
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
	// The quality chosen here is the profile's from now on, as Jellyfin's is.
	api
		.PATCH("/api/v1/profile/preferences", { body: { max_bitrate_kbps: kbps } })
		.catch(() => undefined);
	reopen();
}

// A new source starts at the default rate, so it is the chosen one too.
function chooseSpeed(r: number) {
	menuOpen = false;
	if (video) video.defaultPlaybackRate = r;
	rate = r;
}

function toggle() {
	if (casting) {
		remote?.controller.playOrPause();
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

function playNext() {
	if (!next) return;
	void goto(`/play/${next.id}`, {
		replace: true,
		state: at === undefined ? {} : { queued: at + 1 },
	});
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

function skip(m: Schemas["MarkerRef"]) {
	if (next && m.end_ms / 1000 >= duration - 1) playNext();
	else seek(m.end_ms / 1000);
}

// Skips a marker the reader skips by itself, as the playback passes into it.
function passed() {
	if (!marker || skipping(marker.kind, prefs) !== "skip") return;
	if (skippedAt.has(marker.start_ms)) return;
	skippedAt.add(marker.start_ms);
	skip(marker);
}

function ended() {
	const parts = playback?.parts ?? [];
	if (!casting && part < parts.length - 1) {
		load(part + 1, parts[part + 1].offset_ms / 1000);
		void video?.play();
	} else if (next && !upNextHidden && prefs.next_episode === "play") playNext();
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
	else if (key === "N" && next) playNext();
	else if (key === "P" && hasPrevious) void playPrevious();
	else if (/^[0-9]$/.test(key) && duration) seek((duration * Number(key)) / 10);
	else handled = false;
	if (handled) {
		event.preventDefault();
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
		["nexttrack", next ? playNext : null],
		["previoustrack", hasPrevious ? () => void playPrevious() : null],
	];
	for (const [action, handler] of handlers) {
		try {
			navigator.mediaSession.setActionHandler(action, handler);
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
		if (context && !gone) unwatch = watchCast(context);
	});
	void open(start);
	wake();
	return () => {
		opening++;
		gone = true;
		unwatch?.();
		clearTimeout(restTimer);
		void reporter?.stop();
		// The server lets the playback go, so the receiver stops it too; a
		// title opened next on the same session plays on it again.
		casting?.getMediaSession()?.stop(
			new chrome.cast.media.StopRequest(),
			() => {},
			() => {},
		);
		detach();
	};
});

// Follows the Chromecasts on the network and the session on one: a session
// started opens the playback on it where it is, and one ended opens it here
// again.
function watchCast(context: cast.framework.CastContext) {
	const {
		CastContextEventType,
		CastState,
		RemotePlayerEventType,
		SessionState,
	} = cast.framework;
	const player = new cast.framework.RemotePlayer();
	const controller = new cast.framework.RemotePlayerController(player);
	remote = { player, controller };
	castDevices = context.getCastState() !== CastState.NO_DEVICES_AVAILABLE;
	const devices = (e: cast.framework.CastStateEventData) => {
		castDevices = e.castState !== CastState.NO_DEVICES_AVAILABLE;
	};
	const sessions = (e: cast.framework.SessionStateEventData) => {
		if (
			e.sessionState === SessionState.SESSION_STARTED ||
			e.sessionState === SessionState.SESSION_RESUMED
		) {
			if (casting === e.session) return;
			casting = e.session;
			reopen();
		} else if (e.sessionState === SessionState.SESSION_ENDED && casting) {
			casting = undefined;
			reopen();
		}
	};
	const moved = () => {
		if (!casting || !player.isMediaLoaded) return;
		time = player.currentTime;
		passed();
	};
	const state = () => {
		if (!casting) return;
		const { PlayerState } = chrome.cast.media;
		castWaiting = player.playerState === PlayerState.BUFFERING;
		if (player.playerState === PlayerState.PLAYING) {
			castPaused = false;
			reporter?.playing();
		} else if (player.playerState === PlayerState.PAUSED) {
			castPaused = true;
			reporter?.paused();
		}
	};
	context.addEventListener(CastContextEventType.CAST_STATE_CHANGED, devices);
	context.addEventListener(
		CastContextEventType.SESSION_STATE_CHANGED,
		sessions,
	);
	controller.addEventListener(
		RemotePlayerEventType.CURRENT_TIME_CHANGED,
		moved,
	);
	controller.addEventListener(
		RemotePlayerEventType.PLAYER_STATE_CHANGED,
		state,
	);
	// A session already playing, from another page of this server, is joined.
	const current = context.getCurrentSession();
	if (current && casting !== current) {
		casting = current;
		reopen();
	}
	return () => {
		context.removeEventListener(
			CastContextEventType.CAST_STATE_CHANGED,
			devices,
		);
		context.removeEventListener(
			CastContextEventType.SESSION_STATE_CHANGED,
			sessions,
		);
		controller.removeEventListener(
			RemotePlayerEventType.CURRENT_TIME_CHANGED,
			moved,
		);
		controller.removeEventListener(
			RemotePlayerEventType.PLAYER_STATE_CHANGED,
			state,
		);
		remote = undefined;
	};
}
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

{#snippet clock(
	place: string,
)}
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
	onpointerdown={wake}
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
			delaySubtitle();
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
		{#if trackSrc}
			<track kind="subtitles" src={trackSrc} label={subtitle?.label} default>
		{/if}
	</video>

	{#if casting}
		<div
			class="text-ink pointer-events-none absolute inset-0 grid place-content-center justify-items-center gap-3 p-6 text-center"
		>
			<CastIcon class="size-12" aria-hidden="true" />
			<p class="font-heading text-lg">
				Playing on {casting.getCastDevice().friendlyName}
			</p>
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
						onclick={() => skip(marker)}
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
						onclick={playNext}
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
													>{s.display_title}</Menu.RadioItem
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
							{#if subtitle && !burned && !casting}
								<Menu.Sub>
									<Menu.SubTrigger>
										Subtitle timing
										<span class="text-ink-2 ml-auto text-xs tabular-nums">
											{subtitleDelay
												? `${Math.abs(subtitleDelay)} s ${subtitleDelay > 0 ? "later" : "earlier"}`
												: "On time"}
										</span>
									</Menu.SubTrigger>
									<Menu.SubContent portalProps={{ to: root }}>
										{#each [-1, -0.1, 0.1, 1] as by (by)}
											<Menu.Item
												closeOnSelect={false}
												onSelect={() => moveSubtitle(by)}
											>
												{by < 0 ? "Earlier" : "Later"}
												by {Math.abs(by)} s
											</Menu.Item>
										{/each}
										<Menu.Item
											closeOnSelect={false}
											disabled={!subtitleDelay}
											onSelect={() => moveSubtitle(-subtitleDelay)}
										>
											On time
										</Menu.Item>
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
												>{kbps ? bitrate(kbps) : "Original"}</Menu.RadioItem
											>
										{/each}
									</Menu.RadioGroup>
								</Menu.SubContent>
							</Menu.Sub>
							{#if !casting}
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
								<Menu.Sub>
									<Menu.SubTrigger>Picture</Menu.SubTrigger>
									<Menu.SubContent portalProps={{ to: root }}>
										<Menu.RadioGroup
											value={fit}
											onValueChange={(v) => {
												menuOpen = false;
												fit = v as keyof typeof fits;
											}}
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
										portalProps={{ to: root }}
										class="max-h-80 overflow-y-auto"
									>
										{#each version.chapters as c, i}
											<Menu.Item onSelect={() => seek(c.start_ms / 1000)}>
												<span class="truncate"
													>{c.title || `Chapter ${i + 1}`}</span
												>
												<Menu.Shortcut>{timecode(c.start_ms)}</Menu.Shortcut>
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
					{#if castDevices}
						<Button
							variant="ghost"
							size="icon"
							aria-label={casting
								? `Playing on ${casting.getCastDevice().friendlyName}`
								: "Cast"}
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
</section>
