import { expect, test } from "bun:test";
import {
	bitrate,
	holding,
	episodeLabel,
	fullTitle,
	playHref,
	ratingMark,
	runtime,
	score,
	timecode,
} from "./format.ts";

test("a running time reads as a listing prints it", () => {
	expect(runtime(48 * 60_000)).toBe("48m");
	expect(runtime(112 * 60_000)).toBe("1h 52m");
	expect(runtime(120 * 60_000)).toBe("2h");
});

test("a resume point reads as a player's clock", () => {
	expect(timecode(249_000)).toBe("4:09");
	expect(timecode(3_849_000)).toBe("1:04:09");
});

test("a bitrate reads in Mbps from a thousand kbps, a whole one without a point", () => {
	expect(bitrate(40_000)).toBe("40 Mbps");
	expect(bitrate(8_460)).toBe("8.5 Mbps");
	expect(bitrate(420)).toBe("420 kbps");
});

test("an episode is placed by season and number, a double by both ends", () => {
	expect(episodeLabel(1, 2)).toBe("S1 E2");
	expect(episodeLabel(1, 2, 3)).toBe("S1 E2–E3");
	expect(episodeLabel(null, 5)).toBe("E5");
});

test("an episode is named with its show and place, anything else alone", () => {
	expect(
		fullTitle({ title: "Second", season_number: 1, episode_number: 2 }, "Show"),
	).toBe("Show S1 E2 · Second");
	expect(fullTitle({ title: "Heat" }, undefined)).toBe("Heat");
});

test("each site's score is printed on that site's own scale", () => {
	expect(score("imdb", 78)).toBe("7.8");
	expect(score("rotten_tomatoes", 93)).toBe("93%");
});

test("rotten tomatoes is fresh or upright from 60%, rotten or spilled below", () => {
	expect(ratingMark("rotten_tomatoes", 60)).toBe("tomatometer-fresh");
	expect(ratingMark("rotten_tomatoes", 59)).toBe("tomatometer-rotten");
	expect(ratingMark("rotten_tomatoes_audience", 60)).toBe("popcorn-upright");
	expect(ratingMark("rotten_tomatoes_audience", 59)).toBe("popcorn-spilled");
});

test("the player is told only what the reader chose", () => {
	expect(playHref("t-1")).toBe("/play/t-1");
	expect(
		playHref("t-1", { version: "v-2", audio: 1, subtitle: "off", t: 0 }),
	).toBe("/play/t-1?version=v-2&audio=1&subtitle=off&t=0");
});

test("a library says what it holds as its kind counts it", () => {
	const c = {
		movies: 1204,
		shows: 1,
		seasons: 4,
		episodes: 40,
		collections: 0,
	};
	expect(holding("movies", c)).toBe("1,204 films");
	expect(holding("shows", c)).toBe("1 show · 4 seasons · 40 episodes");
});
