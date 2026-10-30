import type JASSUB from "jassub";
import type { PgsRenderer, VobSubRenderer } from "libbitsub";
import type { components } from "#lib/api/schema.js";
import { delayCues, webVTT } from "./subtitles.js";

type Subtitle = components["schemas"]["Subtitle"];

// What shows a subtitle beside the version that the browser does not show
// itself: styled text drawn by JASSUB and pictures drawn by libbitsub, over the
// video, and plain text given to it as a WebVTT track. Each loads the first
// time it is wanted; one asked for after a later one is dropped.
export class SubtitleDrawing {
	// The address of the track the video is given.
	track = $state<string>();
	// How much later than its file the subtitle shows, in seconds, as the reader
	// set it.
	delay = $state(0);

	#video: () => HTMLVideoElement | undefined;
	#trackURL: string | undefined;
	#styled: JASSUB | undefined;
	#styledURL: string | undefined;
	#pictures:
		| { renderer: PgsRenderer | VobSubRenderer; canvas: HTMLCanvasElement }
		| undefined;
	#picturesURL: string | undefined;
	#delayed = new WeakMap<object, number>();

	constructor(video: () => HTMLVideoElement | undefined) {
		this.#video = video;
	}

	// Draws `given`, or stops drawing what was. Plain text is a track only where
	// the file plays as it is; a stream carries its own.
	async draw(given: Subtitle | undefined, direct: boolean) {
		const drawn = given?.kind === "styled" || given?.kind === "picture";
		void this.#showStyled(given?.kind === "styled" ? given : undefined);
		void this.#showPictures(given?.kind === "picture" ? given : undefined);
		if (direct) await this.#showText(drawn ? undefined : given);
	}

	clear() {
		void this.draw(undefined, true);
	}

	// Shows the subtitle as late as the reader set it: its cues moved, or the
	// clock JASSUB and libbitsub draw by.
	follow() {
		for (const t of this.#video()?.textTracks ?? []) {
			if (t.mode === "showing" && t.cues)
				delayCues(t.cues, this.delay, this.#delayed);
		}
		if (this.#styled) this.#styled.timeOffset = -this.delay;
		if (this.#pictures) this.#pictures.renderer.timeOffset = -this.delay;
	}

	move(by: number) {
		this.delay = Math.round((this.delay + by) * 10) / 10;
		this.follow();
	}

	async #showText(file: Subtitle | undefined) {
		if (file?.url === this.#trackURL) return;
		if (this.track?.startsWith("blob:")) URL.revokeObjectURL(this.track);
		this.track = undefined;
		this.#trackURL = file?.url;
		if (!file) return;
		// A track inside the file is read out as WebVTT.
		if (file.codec === "webvtt" || file.stream != null) {
			this.track = file.url;
			return;
		}
		const srt = await fetch(file.url).then((r) => r.text());
		// Another file was chosen, or the playback closed, while this one came.
		if (this.#trackURL !== file.url) return;
		this.track = URL.createObjectURL(
			new Blob([webVTT(srt)], { type: "text/vtt" }),
		);
	}

	// With the fonts its file carries.
	async #showStyled(sub: Subtitle | undefined) {
		if (sub?.url === this.#styledURL) return;
		this.#styledURL = sub?.url;
		void this.#styled?.destroy();
		this.#styled = undefined;
		if (!sub || !this.#video()) return;
		const absolute = (url: string) => new URL(url, location.href).href;
		const [{ default: JASSUB }, fonts] = await Promise.all([
			import("jassub"),
			sub.fonts
				? fetch(sub.fonts)
						.then((r) => r.json() as Promise<components["schemas"]["Fonts"]>)
						.then((f) => f.fonts.map((font) => absolute(font.url)))
						.catch(() => [])
				: [],
		]);
		const video = this.#video();
		if (this.#styledURL !== sub.url || !video) return;
		this.#styled = new JASSUB({
			video,
			subUrl: absolute(sub.url),
			fonts,
			timeOffset: -this.delay,
		});
	}

	// PGS, or VobSub, which the server hands in Matroska. The canvas sits just
	// after the video, as JASSUB's does, so the controls stay above it.
	async #showPictures(sub: Subtitle | undefined) {
		if (sub?.url === this.#picturesURL) return;
		this.#picturesURL = sub?.url;
		this.#pictures?.renderer.dispose();
		this.#pictures?.canvas.remove();
		this.#pictures = undefined;
		if (!sub || !this.#video()) return;
		const { PgsRenderer, VobSubRenderer } = await import("libbitsub");
		const video = this.#video();
		if (this.#picturesURL !== sub.url || !video) return;
		const canvas = document.createElement("canvas");
		canvas.style.zIndex = "auto";
		video.after(canvas);
		const subUrl = new URL(sub.url, location.href).href;
		const renderer =
			sub.codec === "dvd_subtitle"
				? new VobSubRenderer({ video, canvas, subUrl, fileName: "track.mks" })
				: new PgsRenderer({ video, canvas, subUrl });
		renderer.timeOffset = -this.delay;
		this.#pictures = { renderer, canvas };
	}
}
