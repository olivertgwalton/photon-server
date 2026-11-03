import type { components } from "#lib/api/schema.d.ts";

type Playback = components["schemas"]["Playback"];

// Tracks played one after another on one timeline, as true gapless players
// play them: each track's HLS segments appended to one media source where
// the last left off, so no sample of silence falls between them. Only sound
// copied as it is joins this way: a lossy encoder's delay and padding would
// be heard between tracks, where the files carry no word of how much there is.
export type JoinedTrack = {
	key: string;
	playback: Playback;
	init: string;
	segments: Segment[];
	// How long it is, as its playlist says.
	length: number;
	// Where it starts on the element's timeline, once the track before it is
	// all appended; where it ends, once it is.
	start: number;
	end?: number;
	// Each feeding of it is numbered, so a seek's stops the one before.
	feeding: number;
};

// A segment, and where it starts in its track.
export type Segment = { url: string; at: number };

const lossless: Record<string, string> = { flac: "flac", alac: "alac" };

// How much is fetched ahead of where the element plays, and kept behind it
// to seek back into, in seconds: a browser keeps a few megabytes of sound in
// a media source, less than one track of FLAC.
const keepAhead = 60;
const keepBehind = 30;

// The codec an HLS playlist of sound alone carries, as a media source names
// it, where it is one copied without loss.
export function losslessCodec(master: string): string | undefined {
	const codecs = /CODECS="([^"]+)"/.exec(master)?.[1];
	return codecs ? lossless[codecs.toLowerCase()] : undefined;
}

// A media playlist's init segment and segments, as addresses beside it, each
// where it starts by the lengths of those before it.
export function segmentsOf(
	media: string,
	base: string,
): { init?: string; segments: Segment[]; length: number } {
	const href = (uri: string) => new URL(uri, base).href;
	const init = /#EXT-X-MAP:URI="([^"]+)"/.exec(media)?.[1];
	const segments: Segment[] = [];
	let at = 0;
	let next = 0;
	for (const raw of media.split("\n")) {
		const line = raw.trim();
		if (line.startsWith("#EXTINF:")) {
			next = Number.parseFloat(line.slice("#EXTINF:".length)) || 0;
		} else if (line && !line.startsWith("#")) {
			segments.push({ url: href(line), at });
			at += next;
			next = 0;
		}
	}
	return { init: init ? href(init) : undefined, segments, length: at };
}

// The track playing at time on the timeline: the last to have started.
export function trackAt(tracks: { start: number }[], time: number): number {
	return Math.max(
		tracks.findLastIndex((t) => t.start <= time + 0.001),
		0,
	);
}

// The segment a time in a track is in.
export function segmentAt(segments: Segment[], seconds: number): number {
	return Math.max(
		segments.findLastIndex((s) => s.at <= seconds),
		0,
	);
}

// Where a track's own timeline starts, in seconds: its first segment's
// decode time (ISO BMFF's tfdt) over its track's timescale (mdhd), as the
// server's segments may not start at nothing.
export function mediaStart(init: ArrayBuffer, first: ArrayBuffer): number {
	const timescale = boxField(init, "mdhd", 8, 16);
	const decoded = boxField(first, "tfdt", 0, 0);
	return timescale && decoded !== undefined ? decoded / timescale : 0;
}

// A number in the first box of a type: at v0 as 32 bits, at v1 as 64, each
// that far past the box's version and flags.
function boxField(
	data: ArrayBuffer,
	type: string,
	v0: number,
	v1: number,
): number | undefined {
	const bytes = new Uint8Array(data);
	const code = [...type].map((c) => c.charCodeAt(0));
	for (let i = 4; i + 8 <= bytes.length; i++) {
		if (code.some((c, j) => bytes[i + j] !== c)) continue;
		const view = new DataView(data, i + 4);
		const version = view.getUint8(0);
		if (version === 1) return Number(view.getBigUint64(4 + v1));
		return view.getUint32(4 + v0);
	}
	return undefined;
}

export function joinable(): boolean {
	return (
		typeof MediaSource !== "undefined" &&
		MediaSource.isTypeSupported('audio/mp4; codecs="flac"')
	);
}

export class Joined {
	readonly audio: HTMLAudioElement;
	tracks: JoinedTrack[] = [];
	#source = new MediaSource();
	#buffer: SourceBuffer | undefined;
	#codec: string;
	#end = 0;
	#working: Promise<unknown> = Promise.resolve();
	#abort = new AbortController();
	#url: string;

	private constructor(audio: HTMLAudioElement, codec: string) {
		this.audio = audio;
		this.#codec = codec;
		this.#url = URL.createObjectURL(this.#source);
		audio.src = this.#url;
	}

	// A joined timeline begun with a track from a time in it, or none where
	// its sound is not copied without loss.
	static async open(
		audio: HTMLAudioElement,
		key: string,
		playback: Playback,
		at: number,
	): Promise<Joined | undefined> {
		const read = await readPlaylists(playback);
		if (!read) return undefined;
		const joined = new Joined(audio, read.codec);
		await new Promise((done) =>
			joined.#source.addEventListener("sourceopen", done, { once: true }),
		);
		joined.#buffer = joined.#source.addSourceBuffer(
			`audio/mp4; codecs="${read.codec}"`,
		);
		joined.#append(key, playback, read, at);
		return joined;
	}

	// The next track, after the last: false where it cannot join.
	async append(key: string, playback: Playback): Promise<boolean> {
		const read = await readPlaylists(playback, this.#abort.signal);
		if (!read || read.codec !== this.#codec) return false;
		this.#append(key, playback, read, 0);
		return true;
	}

	// Where a time in a track stands on the timeline, fed again from the
	// segment it is in where it is not buffered; the tracks after it are let
	// go, to be joined again as it nears its end.
	seek(key: string, seconds: number): { time: number; dropped: JoinedTrack[] } {
		const at = this.tracks.findIndex((t) => t.key === key);
		const track = this.tracks[at];
		if (!track) return { time: seconds, dropped: [] };
		const time = track.start + seconds;
		if (this.#buffered(time)) return { time, dropped: [] };
		const dropped = this.tracks.splice(at + 1);
		const feeding = ++track.feeding;
		track.end = undefined;
		void this.#work(async (buffer) => {
			if (feeding !== track.feeding) return;
			await updated(buffer, () => buffer.remove(0, Number.POSITIVE_INFINITY));
			await this.#feed(
				buffer,
				track,
				segmentAt(track.segments, seconds),
				feeding,
			);
		});
		return { time, dropped };
	}

	// The tracks after the one at index let go, where the queue changed.
	dropAfter(index: number): JoinedTrack[] {
		const dropped = this.tracks.splice(index + 1);
		const from = dropped.find((t) => Number.isFinite(t.start))?.start;
		if (from !== undefined) {
			this.#end = from;
			void this.#work((buffer) =>
				updated(buffer, () => buffer.remove(from, Number.POSITIVE_INFINITY)),
			);
		}
		return dropped;
	}

	// No more is coming: the element ends where the last track does.
	finish() {
		void this.#work(async () => {
			if (this.#source.readyState === "open") this.#source.endOfStream();
		});
	}

	close() {
		this.#abort.abort();
		URL.revokeObjectURL(this.#url);
	}

	#append(key: string, playback: Playback, read: Read, from: number) {
		const track: JoinedTrack = {
			key,
			playback,
			init: read.init,
			segments: read.segments,
			length: read.length,
			start: Number.POSITIVE_INFINITY,
			feeding: 0,
		};
		this.tracks.push(track);
		void this.#work(async (buffer) => {
			if (!this.tracks.includes(track)) return;
			track.start = this.#end;
			// The element may be sought anywhere in the track before it is all here.
			const whole = track.start + track.length;
			if (!(this.#source.duration >= whole)) this.#source.duration = whole;
			await this.#feed(buffer, track, segmentAt(track.segments, from), 0);
		});
	}

	// Appends a track's segments from one on, as long as it is still fed by
	// this feeding.
	async #feed(
		buffer: SourceBuffer,
		track: JoinedTrack,
		from: number,
		feeding: number,
	) {
		const current = () =>
			this.tracks.includes(track) && track.feeding === feeding;
		const get = (url: string) =>
			fetch(url, { signal: this.#abort.signal }).then((r) => r.arrayBuffer());
		const [head, first] = await Promise.all([
			get(track.init),
			get(track.segments[from].url),
		]);
		if (!current()) return;
		// Each feeding places its first segment where its playlist says it starts,
		// whatever the media's own times say: the server's segments may count
		// from elsewhere when cut again for a seek.
		buffer.timestampOffset =
			track.start + track.segments[from].at - mediaStart(head, first);
		await updated(buffer, () => buffer.appendBuffer(head));
		await this.#room(buffer, first, current);
		for (const s of track.segments.slice(from + 1)) {
			if (!current()) return;
			await this.#room(buffer, await get(s.url), current);
		}
		if (!current()) return;
		const b = buffer.buffered;
		track.end = b.length ? b.end(b.length - 1) : track.start;
		this.#end = track.end;
	}

	// Appends data once what is buffered ahead has room for it, letting go of
	// what played long enough ago; nothing once its feeding is over.
	async #room(buffer: SourceBuffer, data: ArrayBuffer, current: () => boolean) {
		while (this.#ahead(buffer) > keepAhead) {
			await new Promise((wake) => setTimeout(wake, 1000));
			if (this.#abort.signal.aborted || !current()) return;
		}
		const played = this.audio.currentTime - keepBehind;
		const from = buffer.buffered.length ? buffer.buffered.start(0) : 0;
		if (played > from) await updated(buffer, () => buffer.remove(from, played));
		await updated(buffer, () => buffer.appendBuffer(data));
	}

	#ahead(buffer: SourceBuffer): number {
		const b = buffer.buffered;
		return b.length ? b.end(b.length - 1) - this.audio.currentTime : 0;
	}

	#buffered(time: number): boolean {
		const b = this.#buffer?.buffered;
		if (!b) return false;
		for (let i = 0; i < b.length; i++) {
			if (b.start(i) <= time && time < b.end(i)) return true;
		}
		return false;
	}

	// Work on the buffer, one at a time, as a source buffer takes it.
	#work(work: (buffer: SourceBuffer) => Promise<void>) {
		const buffer = this.#buffer;
		if (!buffer) return Promise.resolve();
		this.#working = this.#working
			.then(() => work(buffer))
			.catch(() => undefined);
		return this.#working;
	}
}

type Read = {
	codec: string;
	init: string;
	segments: Segment[];
	length: number;
};

async function readPlaylists(
	playback: Playback,
	signal?: AbortSignal,
): Promise<Read | undefined> {
	if (!playback.playlist) return undefined;
	const masterURL = new URL(playback.playlist, location.href).href;
	const master = await fetch(masterURL, { signal }).then((r) => r.text());
	const codec = losslessCodec(master);
	const variant = master
		.split("\n")
		.map((l) => l.trim())
		.find((l) => l && !l.startsWith("#"));
	if (!codec || !variant) return undefined;
	const mediaURL = new URL(variant, masterURL).href;
	const media = segmentsOf(
		await fetch(mediaURL, { signal }).then((r) => r.text()),
		mediaURL,
	);
	if (!media.init || !media.segments.length) return undefined;
	return {
		codec,
		init: media.init,
		segments: media.segments,
		length: media.length,
	};
}

// Runs a change to a source buffer, done once it says it is.
function updated(buffer: SourceBuffer, change: () => void): Promise<void> {
	return new Promise((done, fail) => {
		buffer.addEventListener("updateend", () => done(), { once: true });
		buffer.addEventListener("error", () => fail(new Error("append")), {
			once: true,
		});
		try {
			change();
		} catch (e) {
			fail(e);
		}
	});
}
