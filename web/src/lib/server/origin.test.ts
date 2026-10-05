import { expect, test } from "bun:test";
import { crossSite, servedOverHTTPS } from "./origin.ts";

const url = new URL("https://192.168.1.10:3000/auth/login?/login");
const post = (origin?: string) =>
	new Request(url, {
		method: "POST",
		headers: origin ? { origin } : {},
	});

test("a form posted from this page over plain HTTP is not a forgery", () => {
	expect(crossSite(post("http://192.168.1.10:3000"), url)).toBe(false);
	expect(servedOverHTTPS(post("http://192.168.1.10:3000"))).toBe(false);
});

test("a form posted from another site is", () => {
	expect(crossSite(post("https://evil.example"), url)).toBe(true);
	expect(crossSite(post("http://192.168.1.10:4000"), url)).toBe(true);
	expect(crossSite(post("null"), url)).toBe(true);
});

test("reading is never a forgery", () => {
	expect(
		crossSite(
			new Request(url, { headers: { origin: "https://evil.example" } }),
			url,
		),
	).toBe(false);
});

test("a page served over HTTPS keeps a Secure cookie", () => {
	expect(servedOverHTTPS(post("https://photon.example.com"))).toBe(true);
	expect(servedOverHTTPS(post())).toBe(false);
});
