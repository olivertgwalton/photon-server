import { expect, test } from "bun:test";
import { returnPath, loginPath } from "./session.ts";

const from = (to: string) =>
	returnPath(
		new URL(`http://web.test/auth/login?to=${encodeURIComponent(to)}`),
	);

test("logging in returns to the page that sent it", () => {
	expect(from("/libraries/1?sort=title")).toBe("/libraries/1?sort=title");
	expect(returnPath(new URL("http://web.test/auth/login"))).toBe("/");
});

test("logging in never returns to another site", () => {
	for (const to of [
		"//evil.example/",
		"/\\evil.example/",
		"https://evil.example/",
		"javascript:alert(1)",
		"\t//evil.example",
	]) {
		expect(from(to)).toBe("/");
	}
});

test("a page sent to log in says where it was", () => {
	expect(loginPath(new URL("http://web.test/settings?x=1"))).toBe(
		"/auth/login?to=%2Fsettings%3Fx%3D1",
	);
	expect(loginPath(new URL("http://web.test/"))).toBe("/auth/login");
});
