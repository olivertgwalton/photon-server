import { expect, test } from "bun:test";
import { reachable } from "./cast.ts";

test("a Chromecast cannot reach a page opened on the browser's own loopback", () => {
	for (const origin of [
		"http://localhost:8096",
		"https://photon.localhost",
		"http://127.0.0.1:8096",
		"http://[::1]:8096",
	])
		expect(reachable(origin)).toBe(false);
});

test("a Chromecast reaches a page opened at a network address or a name", () => {
	for (const origin of [
		"https://192.168.1.20:8920",
		"https://photon.example.com",
		"https://mini.local:8920",
	])
		expect(reachable(origin)).toBe(true);
});
