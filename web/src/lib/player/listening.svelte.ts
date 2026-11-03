import type Hls from "hls.js";
import { on } from "svelte/events";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";
import { artworkURL } from "#lib/artwork.ts";
import { shuffled } from "#lib/shuffle.ts";
import { Joined, joinable, trackAt } from "./joined.ts";
import {
	type AudioCapabilities,
	after,
	audioProfile,
	chapterAt,
	type GainMode,
	gainFactor,
	gainModes,
	keptOf,
	type Listen,
	movedTo,
	nextAt,
	playingNext,
	previousAt,
	probeAudio,
	type Quality,
	type Queue,
	type Queued,
	qualities,
	qualityKbps,
	queued,
	type Repeat,
	readKept,
	reshuffled,
	without,
} from "./listen.ts";
import { ProgressReporter, type Report } from "./progress.ts";

type Playback = components["schemas"]["Playback"];
export type Order = "in_order" | "shuffled";

// When the player stops by itself: after so long, or at the end of the track
// or chapter playing, as Finamp's and Plexamp's sleep timers do.
export type Sleep =
	| { kind: "off" }
	| { kind: "at"; until: number }
	| { kind: "end"; at: number };

// One thing on an audio element of its own: what plays, or the next, opened
// and buffering before the one playing ends, so it starts at once.
// A deck whose tracks are joined plays one after another on one timeline,
// its key and playback the track's it is at.
type Deck = {
	key: string;
	audio: HTMLAudioElement;
	hls?: Hls;
	joined?: Joined;
	playback: Playback;
	part: number;
	off: () => void;
};

// The next is asked for this long before the one playing ends.
const readyAhead = 20;
const qualityKey = "photon:audio-quality";
const gainKey = "photon:audio-gain";
const keptKey = "photon:audio-queue";
// A playing queue is written again each this many seconds, not at every tick.
const keepEvery = 5;

// The audio player: one queue for the tab, played for as long as the shell's
// bar is drawn, so it goes on from page to page. Seconds throughout, on the
// whole of what plays: an audiobook in several files is one timeline, as the
// video player's parts are.
class Listening {
	// Replaced, never changed in place.
	queue = $state.raw<Queued[]>([]);
	at = $state(0);
	order = $state<Order>("in_order");
	repeat = $state<Repeat>("off");
	quality = $state<Quality>(stored(qualityKey, qualities, "original"));
	gainMode = $state<GainMode>(stored(gainKey, gainModes, "track"));
	sleep = $state<Sleep>({ kind: "off" });
	playing = $state(false);
	position = $state(0);
	duration = $state(0);
	rate = $state(1);
	volume = $state(1);
	muted = $state(false);
	// How far ahead of the start has arrived, for the seek bar.
	buffered = $state(0);

	current = $derived<Queued | undefined>(this.queue[this.at]);

	#started = false;
	#deck: Deck | undefined;
	#ready: Deck | undefined;
	// The key of the one being made ready, while the server is asked.
	#readying: string | undefined;
	#reporter: ProgressReporter | undefined;
	#caps: AudioCapabilities | undefined;
	#seekTo: number | undefined;
	#sleepTimer: ReturnType<typeof setTimeout> | undefined;
	// The queue in the order it was queued in, which unshuffling goes back to.
	#original: Queued[] = [];
	// Each open is numbered, so one answered after another began is let go.
	#opening = 0;

	// Drawn once the bar is: the player stops once it is gone, as a page
	// without the shell has a player of its own.
	// What was queued is back as it was left, paused, as Finamp and Plexamp
	// keep a queue on each device; gone, the queue is still kept.
	start = () => {
		this.#started = true;
		this.#restore();
		const off = on(window, "pagehide", () => {
			this.#keep();
			void this.#reporter?.stop(true);
		});
		return () => {
			this.#keep();
			this.#halt();
			this.#reset();
			off();
			this.#started = false;
		};
	};

	// From one of them, or shuffled from any.
	play = (
		items: Listen[],
		{ from, order = "in_order" }: { from?: number; order?: Order } = {},
	) => {
		if (!items.length) return;
		const all = queued(items);
		const first =
			all[from ?? (order === "shuffled" ? randomIndex(all.length) : 0)];
		this.#original = all;
		this.order = order;
		this.queue =
			order === "shuffled"
				? [first, ...shuffled(all.filter((i) => i !== first))]
				: all;
		this.at = this.queue.findIndex((i) => i.key === first.key);
		this.#keep();
		void this.#open(startOf(first));
	};

	// Straight after the one playing, or played where nothing is.
	playNext = (items: Listen[]) => {
		if (!this.current) return this.play(items);
		const added = queued(items);
		// Unshuffled, they stay after the one they were put after.
		const playing = this.#original.findIndex(
			(o) => o.key === this.current?.key,
		);
		this.#original = this.#original.toSpliced(playing + 1, 0, ...added);
		this.#set(playingNext(this.#q, added));
	};

	// At the end of the queue, or played where nothing is.
	addToQueue = (items: Listen[]) => {
		if (!this.current) return this.play(items);
		const added = queued(items);
		this.#original = [...this.#original, ...added];
		this.queue = [...this.queue, ...added];
		this.#keep();
	};

	move = (from: number, to: number) => this.#set(movedTo(this.#q, from, to));

	remove = (index: number) => {
		const playing = index === this.at;
		this.#set(without(this.#q, index));
		if (!this.queue.length) this.close();
		else if (playing) this.jump(this.at);
	};

	// What is still to play goes; what played and the one playing stay.
	clearUpcoming = () => {
		this.#set({ items: this.queue.slice(0, this.at + 1), at: this.at });
	};

	toggleShuffle = () => {
		const order = this.order === "shuffled" ? "in_order" : "shuffled";
		this.order = order;
		this.#set(reshuffled(this.#q, order, this.#original));
	};

	cycleRepeat = () => {
		this.repeat = (
			{ off: "all", all: "one", one: "off" } as const satisfies Record<
				Repeat,
				Repeat
			>
		)[this.repeat];
		this.#settleReady();
		this.#keep();
	};

	setQuality = (quality: Quality) => {
		this.quality = quality;
		keep(qualityKey, quality);
		this.#discardReady();
		if (this.#deck) void this.#open(this.position);
	};

	setSleep = (sleep: Sleep) => {
		clearTimeout(this.#sleepTimer);
		this.sleep = sleep;
		if (sleep.kind === "at") {
			this.#sleepTimer = setTimeout(
				() => this.#fallAsleep(),
				sleep.until - Date.now(),
			);
		}
	};

	// To the end of the chapter playing, or of the track.
	sleepAtEnd = () => {
		const chapter = chapterAt(this.position, this.current?.chapters);
		this.setSleep({
			kind: "end",
			at: chapter ? chapter.end_ms / 1000 : this.duration,
		});
	};

	jump = (index: number) => {
		const item = this.queue[index];
		if (!item) return;
		this.at = index;
		this.#keep();
		void this.#open(startOf(item));
	};

	toggle = () => {
		if (!this.current) return;
		const audio = this.#deck?.audio;
		if (!audio) {
			void this.#open(this.position);
			return;
		}
		if (audio.paused) void audio.play().catch(() => undefined);
		else audio.pause();
	};

	next = () => {
		const within = nextAt(this.position, this.current?.chapters);
		if (within !== null) return this.seek(within);
		const to = after(this.#q, this.repeat === "one" ? "all" : this.repeat);
		if (to !== null) this.jump(to);
	};

	previous = () => {
		const within = previousAt(this.position, this.current?.chapters);
		if (within !== null) this.seek(within);
		else if (this.at > 0) this.jump(this.at - 1);
		else this.seek(0);
	};

	seek = (seconds: number) => {
		const deck = this.#deck;
		if (!deck) return;
		const to = Math.min(Math.max(seconds, 0), this.duration || seconds);
		this.position = to;
		const parts = deck.playback.parts;
		if (deck.joined) {
			const { time, dropped } = deck.joined.seek(deck.key, to);
			for (const t of dropped) {
				void send(t.playback.playback_id)({
					kind: "stop",
					position_ms: 0,
					keepalive: false,
				});
			}
			deck.audio.currentTime = time;
		} else if (parts?.length) {
			const index = partAt(parts, to);
			if (index !== deck.part) {
				const playing = !deck.audio.paused;
				this.#load(deck, index, to);
				if (playing) void deck.audio.play().catch(() => undefined);
				return;
			}
			deck.audio.currentTime = to - parts[index].offset_ms / 1000;
		} else {
			deck.audio.currentTime = to;
		}
		this.#reporter?.seeked();
	};

	setRate = (rate: number) => {
		this.rate = rate;
		if (this.#deck) this.#deck.audio.playbackRate = rate;
	};

	setGainMode = (mode: GainMode) => {
		this.gainMode = mode;
		keep(gainKey, mode);
		if (this.#deck) this.#setUp(this.#deck);
	};

	setVolume = (volume: number) => {
		this.volume = volume;
		if (this.#deck) this.#setUp(this.#deck);
	};

	toggleMute = () => {
		this.muted = !this.muted;
		if (this.#deck) this.#deck.audio.muted = this.muted;
	};

	// The queue goes, here and from this browser's keeping.
	close = () => {
		this.#halt();
		this.#reset();
		forget(keptKey);
	};

	#halt() {
		void this.#reporter?.stop();
		this.#reporter = undefined;
		this.#opening++;
		this.#drop(this.#deck);
		this.#deck = undefined;
		this.#discardReady();
		this.setSleep({ kind: "off" });
	}

	#reset() {
		this.queue = [];
		this.#original = [];
		this.at = 0;
		this.playing = false;
		this.position = 0;
		this.duration = 0;
	}

	#keep() {
		if (!this.queue.length) return forget(keptKey);
		const position = Math.floor(this.position);
		const kept = keptOf(
			this.queue,
			this.at,
			position,
			this.order,
			this.repeat,
			this.#original,
		);
		keep(keptKey, JSON.stringify(kept));
	}

	#restore() {
		const kept = readKept(storedText(keptKey));
		if (!kept) return;
		const items = queued(kept.items);
		this.queue = items;
		this.at = kept.at;
		this.order = kept.order;
		this.repeat = kept.repeat;
		this.#original = kept.original.map((i) => items[i]);
		this.position = kept.position;
		this.duration = (items[kept.at].duration_ms ?? 0) / 1000;
	}

	get #q(): Queue {
		return { items: this.queue, at: this.at };
	}

	#set(q: Queue) {
		this.queue = q.items;
		this.at = q.at;
		this.#settleReady();
		this.#keep();
	}

	#fallAsleep() {
		this.setSleep({ kind: "off" });
		this.#deck?.audio.pause();
	}

	// What the element playing says. A deck made ready is not yet listened to.
	#listen(deck: Deck): () => void {
		const { audio } = deck;
		const mine = () => deck === this.#deck;
		const off = [
			on(audio, "timeupdate", () => mine() && this.#timeupdate(deck)),
			on(audio, "progress", () => mine() && this.#progress(deck)),
			on(audio, "loadedmetadata", () => {
				if (!mine()) return;
				if (this.#seekTo !== undefined) {
					audio.currentTime = this.#seekTo;
					this.#seekTo = undefined;
				}
				audio.playbackRate = this.rate;
			}),
			on(audio, "playing", () => {
				if (!mine()) return;
				this.playing = true;
				this.#reporter?.playing();
			}),
			on(audio, "pause", () => {
				if (!mine()) return;
				this.playing = false;
				// The pause an ended file makes is not the listener's.
				if (!audio.ended) this.#reporter?.paused();
				this.#keep();
			}),
			on(audio, "ended", () => mine() && this.#ended(deck)),
		];
		return () => {
			for (const stop of off) stop();
		};
	}

	#timeupdate(deck: Deck) {
		if (this.#seekTo !== undefined) return;
		let offset = (deck.playback.parts?.[deck.part]?.offset_ms ?? 0) / 1000;
		const joined = deck.joined;
		if (joined) {
			const track =
				joined.tracks[trackAt(joined.tracks, deck.audio.currentTime)];
			if (track.key !== deck.key)
				this.#crossed(deck, track.key, track.playback);
			offset = -track.start;
		}
		const before = Math.floor(this.position / keepEvery);
		this.position = offset + deck.audio.currentTime;
		if (Math.floor(this.position / keepEvery) !== before) this.#keep();
		if (this.sleep.kind === "end" && this.position >= this.sleep.at - 0.25) {
			this.#fallAsleep();
			return;
		}
		const lastPart = deck.part >= (deck.playback.parts?.length ?? 1) - 1;
		const lastJoined = !joined || joined.tracks.at(-1)?.key === deck.key;
		if (lastPart && lastJoined && this.duration - this.position < readyAhead) {
			void this.#makeReady();
		}
	}

	// The joined timeline has gone on into the next track: the one before is
	// told it ended, and the next is reported on, without a sample between.
	#crossed(deck: Deck, key: string, playback: Playback) {
		const to = this.queue.findIndex((q) => q.key === key);
		if (to < 0) return;
		void this.#reporter?.stop();
		deck.key = key;
		deck.playback = playback;
		this.at = to;
		this.duration = (this.queue[to].duration_ms ?? 0) / 1000;
		this.#setUp(deck);
		this.#keep();
		this.#reporter = new ProgressReporter(
			() => this.position * 1000,
			send(playback.playback_id),
		);
		this.#reporter.playing();
	}

	#progress(deck: Deck) {
		const ranges = deck.audio.buffered;
		const offset = this.position - deck.audio.currentTime;
		this.buffered = ranges.length ? offset + ranges.end(ranges.length - 1) : 0;
	}

	#ended(deck: Deck) {
		const parts = deck.playback.parts;
		if (parts && deck.part < parts.length - 1) {
			this.#load(deck, deck.part + 1, parts[deck.part + 1].offset_ms / 1000);
			void deck.audio.play().catch(() => undefined);
			return;
		}
		const to = after(this.#q, this.repeat);
		if (this.sleep.kind === "end") {
			this.#fallAsleep();
		} else if (to !== null) {
			const ready = this.#ready;
			if (ready && ready.key === this.queue[to]?.key) {
				this.at = to;
				this.#keep();
				this.#takeReady(ready);
			} else {
				this.jump(to);
			}
			return;
		}
		// The last is done: it stays in the bar, to be played again.
		void this.#reporter?.stop();
		this.#reporter = undefined;
		this.#drop(this.#deck);
		this.#deck = undefined;
		this.position = 0;
	}

	// The next, opened and buffering on an element of its own, where it plays
	// from its start: what Gapless can be in a browser, without decoding the
	// sound itself.
	async #makeReady() {
		const to = after(this.#q, this.repeat);
		const item = to === null || to === this.at ? undefined : this.queue[to];
		const joined = this.#deck?.joined;
		if (!item) {
			// Nothing comes after: the joined timeline ends with this track.
			joined?.finish();
			return;
		}
		if (item.start_ms || this.#ready?.key === item.key) return;
		if (this.#readying === item.key) return;
		this.#readying = item.key;
		const join = joined !== undefined && this.#joins(item);
		const data = await this.#ask(item, 0, join);
		this.#readying = undefined;
		if (!data) return;
		if (this.queue[after(this.#q, this.repeat) ?? -1]?.key !== item.key) {
			void send(data.playback_id)({
				kind: "stop",
				position_ms: 0,
				keepalive: false,
			});
			return;
		}
		if (join && joined && (await joined.append(item.key, data))) return;
		this.#discardReady();
		const deck = await this.#deckOf(item.key, data, 0, false);
		if (deck) this.#ready = deck;
	}

	// A track joins the one before where the browser takes lossless sound in a
	// media source and nothing asks the server to encode it: an audiobook,
	// played from its place, keeps a file at a time.
	#joins(item: Listen): boolean {
		return (
			this.quality === "original" &&
			!item.version &&
			!item.chapters?.length &&
			joinable()
		);
	}

	#takeReady(ready: Deck) {
		void this.#reporter?.stop();
		this.#drop(this.#deck);
		this.#ready = undefined;
		this.#deck = ready;
		ready.off = this.#listen(ready);
		this.#setUp(ready);
		const item = this.current;
		this.position = 0;
		this.duration = (item?.duration_ms ?? 0) / 1000;
		this.#reporter = new ProgressReporter(
			() => this.position * 1000,
			send(ready.playback.playback_id),
		);
		void ready.audio.play().catch(() => undefined);
	}

	// A deck made ready for what is no longer next is let go.
	#settleReady() {
		const to = after(this.#q, this.repeat);
		const next = this.queue[to ?? -1]?.key;
		if (this.#ready && next !== this.#ready.key) {
			this.#discardReady();
		}
		const deck = this.#deck;
		const joined = deck?.joined;
		if (!deck || !joined) return;
		const at = joined.tracks.findIndex((t) => t.key === deck.key);
		if (joined.tracks[at + 1] && joined.tracks[at + 1].key !== next) {
			for (const t of joined.dropAfter(at)) {
				void send(t.playback.playback_id)({
					kind: "stop",
					position_ms: 0,
					keepalive: false,
				});
			}
		}
	}

	#discardReady() {
		const ready = this.#ready;
		this.#ready = undefined;
		if (!ready) return;
		void send(ready.playback.playback_id)({
			kind: "stop",
			position_ms: 0,
			keepalive: false,
		});
		this.#drop(ready);
	}

	// Opens a playback of what the queue is at, stopping the one before it
	// first so the server never encodes twice for one listener.
	async #open(at: number) {
		const mine = ++this.#opening;
		const item = this.current;
		if (!item || !this.#started) return;
		const previous = this.#reporter;
		this.#reporter = undefined;
		await previous?.stop();
		this.#drop(this.#deck);
		this.#deck = undefined;
		this.#settleReady();
		this.position = at;
		this.duration = (item.duration_ms ?? 0) / 1000;
		const join = this.#joins(item);
		const data = await this.#ask(item, at, join);
		if (mine !== this.#opening) {
			if (data)
				void send(data.playback_id)({
					kind: "stop",
					position_ms: 0,
					keepalive: false,
				});
			return;
		}
		if (!data) return;
		const deck = await this.#deckOf(item.key, data, at, join);
		if (!deck || mine !== this.#opening) return;
		this.#deck = deck;
		deck.off = this.#listen(deck);
		this.#setUp(deck);
		this.#reporter = new ProgressReporter(
			() => this.position * 1000,
			send(data.playback_id),
		);
		await deck.audio.play().catch(() => undefined);
	}

	// A track to be joined is asked for as HLS, which the server copies its
	// sound into as it is.
	async #ask(
		item: Listen,
		at: number,
		join = false,
	): Promise<Playback | undefined> {
		this.#caps ??= probeAudio();
		const profile = audioProfile(this.#caps, qualityKbps(this.quality));
		if (join) profile.containers = [];
		const { data, error } = await client().POST("/api/v1/titles/{id}/play", {
			params: { path: { id: item.id } },
			body: {
				version_id: item.version,
				profile,
				start_ms: Math.round(at * 1000),
			},
		});
		if (!data)
			toast.error(`Couldn't play ${item.title}: ${problemMessage(error)}`);
		return data;
	}

	async #deckOf(
		key: string,
		playback: Playback,
		at: number,
		join: boolean,
	): Promise<Deck | undefined> {
		const audio = new Audio();
		audio.preload = "auto";
		const deck: Deck = { key, audio, playback, part: 0, off: () => undefined };
		const joined = join
			? await Joined.open(audio, key, playback, at)
			: undefined;
		if (joined) {
			deck.joined = joined;
			if (at) this.#seekTo = at;
		} else if (playback.parts?.length) {
			this.#load(deck, partAt(playback.parts, at), at);
		} else if (playback.playlist) {
			if (this.#caps?.nativeHLS) {
				if (at) this.#seekTo = at;
				audio.src = playback.playlist;
			} else {
				const { default: HLS } = await import("hls.js");
				const hls = new HLS({ startPosition: at });
				deck.hls = hls;
				hls.on(HLS.Events.ERROR, (_, e) => {
					if (!e.fatal || deck !== this.#deck) return;
					toast.error(`The stream stopped: ${e.details}.`);
					void this.#reporter?.stop();
					this.#reporter = undefined;
					this.#drop(deck);
					this.#deck = undefined;
				});
				hls.loadSource(playback.playlist);
				hls.attachMedia(audio);
			}
		}
		return deck;
	}

	// The listener's volume, turned by the track's ReplayGain.
	#setUp({ audio, playback }: Deck) {
		audio.volume =
			this.volume * gainFactor(playback.replay_gain, this.gainMode);
		audio.muted = this.muted;
		audio.playbackRate = this.rate;
	}

	#load(deck: Deck, index: number, at: number) {
		const p = deck.playback.parts?.[index];
		if (!p) return;
		deck.part = index;
		const into = at - p.offset_ms / 1000;
		if (deck === this.#deck || into > 0) this.#seekTo = into || undefined;
		deck.audio.src = p.url;
	}

	#drop(deck: Deck | undefined) {
		if (!deck) return;
		deck.off();
		deck.hls?.destroy();
		deck.joined?.close();
		deck.audio.pause();
		deck.audio.removeAttribute("src");
		deck.audio.load();
		if (deck === this.#deck) this.#seekTo = undefined;
	}
}

function randomIndex(length: number): number {
	return Math.floor(Math.random() * length);
}

function startOf(item: Listen): number {
	return item.start_ms ? item.start_ms / 1000 : 0;
}

// A setting this browser keeps, one of options.
function stored<T extends string>(
	key: string,
	options: readonly T[],
	otherwise: T,
): T {
	const kept = storedText(key);
	return options.find((o) => o === kept) ?? otherwise;
}

// What this browser keeps under key, nothing where it keeps nothing or
// refuses storage.
function storedText(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

function forget(key: string) {
	try {
		localStorage.removeItem(key);
	} catch {
		// Nothing was kept where storage is refused.
	}
}

function keep(key: string, value: string) {
	try {
		localStorage.setItem(key, value);
	} catch {
		// Kept for this page alone where storage is refused.
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
