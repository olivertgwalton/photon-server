import type Hls from "hls.js";
import { on } from "svelte/events";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";
import { artworkURL } from "#lib/artwork.ts";
import { shuffled } from "#lib/queue.ts";
import {
	type AudioCapabilities,
	audioProfile,
	type Listen,
	nextAt,
	previousAt,
	probeAudio,
} from "./listen.ts";
import { ProgressReporter, type Report } from "./progress.ts";

type Playback = components["schemas"]["Playback"];
export type Order = "in_order" | "shuffled";

// The audio player: one queue for the tab, played on an audio element of its
// own for as long as the shell's bar is drawn, so it goes on from page to page. Seconds
// throughout, on the whole of what plays: an audiobook in several files is
// one timeline, as the video player's parts are.
class Listening {
	queue = $state<Listen[]>([]);
	at = $state(0);
	playing = $state(false);
	position = $state(0);
	duration = $state(0);
	rate = $state(1);
	volume = $state(1);
	muted = $state(false);
	// How far ahead of the start has arrived, for the seek bar.
	buffered = $state(0);

	current = $derived<Listen | undefined>(this.queue[this.at]);

	#audio: HTMLAudioElement | undefined;
	#hls: Hls | undefined;
	#caps: AudioCapabilities | undefined;
	#playback: Playback | undefined;
	#reporter: ProgressReporter | undefined;
	#part = 0;
	#seekTo: number | undefined;
	// Each open is numbered, so one answered after another began is let go.
	#opening = 0;

	// The element, made once the bar is drawn: the player stops once it is
	// gone, as a page without the shell has a player of its own.
	start = () => {
		const audio = new Audio();
		audio.preload = "auto";
		this.#audio = audio;
		const off = [
			on(audio, "timeupdate", this.#timeupdate),
			on(audio, "progress", this.#progress),
			on(audio, "loadedmetadata", this.#loadedmetadata),
			on(audio, "playing", this.#onplaying),
			on(audio, "pause", this.#onpause),
			on(audio, "ended", this.#ended),
			on(audio, "volumechange", () => {
				this.volume = audio.volume;
				this.muted = audio.muted;
			}),
			on(window, "pagehide", () => void this.#reporter?.stop(true)),
		];
		return () => {
			this.close();
			for (const stop of off) stop();
			this.#audio = undefined;
		};
	};

	setVolume = (volume: number) => {
		if (this.#audio) this.#audio.volume = volume;
	};

	toggleMute = () => {
		if (this.#audio) this.#audio.muted = !this.#audio.muted;
	};

	play = (items: Listen[], from = 0, order: Order = "in_order") => {
		if (!items.length) return;
		const first = items[from];
		this.queue =
			order === "shuffled"
				? [first, ...shuffled(items.toSpliced(from, 1))]
				: items;
		this.at = order === "shuffled" ? 0 : from;
		void this.#open(first.start_ms ? first.start_ms / 1000 : 0);
	};

	// Puts a thing after the one playing, or plays it where nothing is.
	playNext = (item: Listen) => {
		if (!this.current) return this.play([item]);
		this.queue = this.queue.toSpliced(this.at + 1, 0, item);
	};

	jump = (index: number) => {
		if (!this.queue[index]) return;
		this.at = index;
		const item = this.queue[index];
		void this.#open(item.start_ms ? item.start_ms / 1000 : 0);
	};

	toggle = () => {
		const audio = this.#audio;
		if (!audio || !this.current) return;
		if (!this.#playback) {
			void this.#open(this.position);
			return;
		}
		if (audio.paused) void audio.play().catch(() => undefined);
		else audio.pause();
	};

	next = () => {
		const within = nextAt(this.position, this.current?.chapters);
		if (within !== null) this.seek(within);
		else if (this.at < this.queue.length - 1) this.jump(this.at + 1);
	};

	previous = () => {
		const within = previousAt(this.position, this.current?.chapters);
		if (within !== null) this.seek(within);
		else if (this.at > 0) this.jump(this.at - 1);
		else this.seek(0);
	};

	seek = (seconds: number) => {
		const audio = this.#audio;
		if (!audio) return;
		const to = Math.min(Math.max(seconds, 0), this.duration || seconds);
		this.position = to;
		const parts = this.#playback?.parts;
		if (parts) {
			const index = partAt(parts, to);
			if (index !== this.#part) {
				const playing = !audio.paused;
				this.#load(index, to);
				if (playing) void audio.play().catch(() => undefined);
				return;
			}
			audio.currentTime = to - parts[index].offset_ms / 1000;
		} else {
			audio.currentTime = to;
		}
		this.#reporter?.seeked();
	};

	setRate = (rate: number) => {
		this.rate = rate;
		if (this.#audio) this.#audio.playbackRate = rate;
	};

	close = () => {
		void this.#reporter?.stop();
		this.#reporter = undefined;
		this.#opening++;
		this.#detach();
		this.#playback = undefined;
		this.queue = [];
		this.at = 0;
		this.playing = false;
		this.position = 0;
		this.duration = 0;
	};

	#timeupdate = () => {
		const audio = this.#audio;
		if (!audio || this.#seekTo !== undefined) return;
		const offset = this.#playback?.parts?.[this.#part]?.offset_ms ?? 0;
		this.position = offset / 1000 + audio.currentTime;
	};

	#progress = () => {
		const audio = this.#audio;
		if (!audio) return;
		const ranges = audio.buffered;
		const offset = this.position - audio.currentTime;
		this.buffered = ranges.length ? offset + ranges.end(ranges.length - 1) : 0;
	};

	#loadedmetadata = () => {
		const audio = this.#audio;
		if (!audio) return;
		if (this.#seekTo !== undefined) {
			audio.currentTime = this.#seekTo;
			this.#seekTo = undefined;
		}
		audio.playbackRate = this.rate;
	};

	#onplaying = () => {
		this.playing = true;
		this.#reporter?.playing();
	};

	#onpause = () => {
		this.playing = false;
		// The pause an ended file makes is not the reader's.
		if (!this.#audio?.ended) this.#reporter?.paused();
	};

	#ended = () => {
		const parts = this.#playback?.parts;
		if (parts && this.#part < parts.length - 1) {
			this.#load(this.#part + 1, parts[this.#part + 1].offset_ms / 1000);
			void this.#audio?.play().catch(() => undefined);
			return;
		}
		if (this.at < this.queue.length - 1) {
			this.jump(this.at + 1);
			return;
		}
		// The last is done: it stays in the bar, to be played again.
		void this.#reporter?.stop();
		this.#reporter = undefined;
		this.#playback = undefined;
		this.position = 0;
	};

	// Opens a playback of what the queue is at, stopping the one before it
	// first so the server never encodes twice for one listener.
	async #open(at: number) {
		const mine = ++this.#opening;
		const item = this.current;
		if (!item) return;
		const previous = this.#reporter;
		this.#reporter = undefined;
		await previous?.stop();
		this.#detach();
		this.#playback = undefined;
		this.position = at;
		this.duration = (item.duration_ms ?? 0) / 1000;
		this.#caps ??= probeAudio();
		const { data, error } = await client().POST("/api/v1/titles/{id}/play", {
			params: { path: { id: item.id } },
			body: {
				version_id: item.version,
				profile: audioProfile(this.#caps, 0),
				start_ms: Math.round(at * 1000),
			},
		});
		if (mine !== this.#opening) {
			if (data)
				void send(data.playback_id)({
					kind: "stop",
					position_ms: 0,
					keepalive: false,
				});
			return;
		}
		if (!data) {
			toast.error(`Couldn't play ${item.title}: ${problemMessage(error)}`);
			return;
		}
		this.#playback = data;
		this.#reporter = new ProgressReporter(
			() => this.position * 1000,
			send(data.playback_id),
		);
		await this.#attach(data, at, mine);
	}

	async #attach(p: Playback, at: number, mine: number) {
		const audio = this.#audio;
		if (!audio || !this.#caps) return;
		if (p.parts?.length) {
			this.#load(partAt(p.parts, at), at);
		} else if (p.playlist) {
			this.#part = 0;
			if (this.#caps.nativeHLS) {
				this.#seekTo = at;
				audio.src = p.playlist;
			} else {
				const { default: HLS } = await import("hls.js");
				if (mine !== this.#opening) return;
				const hls = new HLS({ startPosition: at });
				this.#hls = hls;
				hls.on(HLS.Events.ERROR, (_, e) => {
					if (!e.fatal) return;
					toast.error(`The stream stopped: ${e.details}.`);
					this.#detach();
					this.#playback = undefined;
				});
				hls.loadSource(p.playlist);
				hls.attachMedia(audio);
			}
		}
		await audio.play().catch(() => undefined);
	}

	#load(index: number, at: number) {
		const p = this.#playback?.parts?.[index];
		if (!this.#audio || !p) return;
		this.#part = index;
		this.#seekTo = at - p.offset_ms / 1000;
		this.#audio.src = p.url;
	}

	#detach() {
		this.#hls?.destroy();
		this.#hls = undefined;
		this.#seekTo = undefined;
		const audio = this.#audio;
		if (audio?.hasAttribute("src")) {
			audio.removeAttribute("src");
			audio.load();
		}
	}
}

function partAt(parts: { offset_ms: number }[], seconds: number): number {
	return Math.max(
		parts.findLastIndex((p) => p.offset_ms / 1000 <= seconds),
		0,
	);
}

function send(id: string) {
	return (r: Report) => {
		// A stop may go as the page closes, so it is kept alive past it.
		const call =
			r.kind === "stop"
				? client().POST("/api/v1/playbacks/{id}/stop", {
						params: { path: { id } },
						body: { position_ms: r.position_ms },
						keepalive: r.keepalive,
					})
				: client().POST("/api/v1/playbacks/{id}/progress", {
						params: { path: { id } },
						body: { position_ms: r.position_ms, state: r.state },
					});
		return call.catch(() => undefined);
	};
}

export const listening = new Listening();

// The lock screen's and the keyboard's media keys, and what they show: the
// browser's Media Session, kept to what plays.
export function mediaSession(): () => void {
	if (!("mediaSession" in navigator)) return () => undefined;
	const session = navigator.mediaSession;
	const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
		["play", () => listening.toggle()],
		["pause", () => listening.toggle()],
		["nexttrack", () => listening.next()],
		["previoustrack", () => listening.previous()],
		["seekto", (d) => d.seekTime !== undefined && listening.seek(d.seekTime)],
		[
			"seekbackward",
			(d) => listening.seek(listening.position - (d.seekOffset ?? 10)),
		],
		[
			"seekforward",
			(d) => listening.seek(listening.position + (d.seekOffset ?? 30)),
		],
	];
	for (const [action, handler] of handlers) {
		try {
			session.setActionHandler(action, handler);
		} catch {
			// A browser that has no such action leaves it off.
		}
	}
	return () => {
		for (const [action] of handlers) {
			try {
				session.setActionHandler(action, null);
			} catch {}
		}
		session.metadata = null;
	};
}

export function showNowPlaying(item: Listen | undefined) {
	if (!("mediaSession" in navigator)) return;
	navigator.mediaSession.metadata = item
		? new MediaMetadata({
				title: item.title,
				artist: item.by ?? "",
				album: item.from?.title ?? "",
				artwork: item.artwork
					? [{ src: artworkURL(item.artwork, 480), sizes: "480x480" }]
					: [],
			})
		: null;
}
