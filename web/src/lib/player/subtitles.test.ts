import { expect, test } from "bun:test";
import type { components } from "#lib/api/schema.js";
import { choices, needsReplay, wants, webVTT } from "./subtitles.ts";

type Schemas = components["schemas"];

const version: Schemas["VersionPage"] = {
	id: "v",
	container: "matroska,webm",
	duration_ms: 1,
	size_bytes: 1,
	parts: 1,
	files: [{ id: "p", index: 0, size_bytes: 1, duration_ms: 1, offset_ms: 0 }],
	streams: [
		{ index: 0, kind: "video", codec: "h264" },
		{ index: 2, kind: "subtitle", codec: "hdmv_pgs_subtitle", language: "en" },
		{
			index: 3,
			kind: "subtitle",
			codec: "subrip",
			language: "fr",
			forced: true,
		},
	],
	subtitles: [
		{ id: "s1", codec: "subrip", language: "en", hearing_impaired: true },
		{ id: "s2", codec: "dvd_subtitle", language: "de" },
		{ id: "s3", codec: "ass", title: "Signs" },
	],
};

const direct: Schemas["Playback"] = {
	playback_id: "p",
	method: "direct",
	version_id: "v",
	expires_at: "",
};
const remux: Schemas["Playback"] = { ...direct, method: "remux" };

test("each subtitle is named and placed where HLS publishes it", () => {
	expect(choices(version)).toEqual([
		{
			key: "s2",
			label: "English",
			codec: "hdmv_pgs_subtitle",
			stream: 2,
			language: "en",
		},
		{
			key: "s3",
			label: "French (Forced)",
			codec: "subrip",
			stream: 3,
			rendition: 0,
			language: "fr",
			forced: true,
		},
		{
			key: "f0",
			label: "English (SDH)",
			codec: "subrip",
			file: 0,
			rendition: 1,
			language: "en",
		},
	]);
});

test("a picture is asked to be drawn in, then left alone once it is", () => {
	const pgs = choices(version)[0];
	expect(wants(pgs)).toEqual({ subtitle_stream: 2, viaHLS: false });
	expect(needsReplay(pgs, direct)).toBe(true);
	const burning = {
		...remux,
		video: { stream: 0, decision: "transcode" as const, burned_subtitle: 2 },
	};
	expect(needsReplay(pgs, burning)).toBe(false);
	expect(needsReplay(undefined, burning)).toBe(true);
});

test("a file a browser reads plays beside the file as it is", () => {
	const srt = choices(version)[2];
	expect(wants(srt).viaHLS).toBe(false);
	expect(needsReplay(srt, direct)).toBe(false);
});

test("a plain track inside the file comes through HLS", () => {
	const inside = choices(version)[1];
	expect(wants(inside)).toEqual({ subtitle_stream: 3, viaHLS: true });
	expect(needsReplay(inside, direct)).toBe(true);
	expect(needsReplay(inside, remux)).toBe(false);
});

test("SubRip reads as WebVTT", () => {
	expect(webVTT("﻿1\r\n00:00:01,500 --> 00:00:02,000\r\nHello\r\n")).toBe(
		"WEBVTT\n\n1\n00:00:01.500 --> 00:00:02.000\nHello\n",
	);
});
