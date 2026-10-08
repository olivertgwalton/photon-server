import { expect, test } from "bun:test";
import { problemMessage } from "./problem.ts";

test("the server's own detail is what the reader is told", () => {
	expect(
		problemMessage({
			title: "Bad Request",
			status: 400,
			code: "invalid_body",
			detail: "a PIN is 4 to 6 digits",
		}),
	).toBe("a PIN is 4 to 6 digits");
});

test("a refusal with no detail is told in words, by its code", () => {
	expect(
		problemMessage({
			title: "Unauthorized",
			status: 401,
			code: "sign_in_refused",
		}),
	).toBe("That name and password don't match.");
});

test("a code with nothing more to say falls back to its title", () => {
	expect(
		problemMessage({
			title: "Service Unavailable",
			status: 503,
			code: "not_ready",
		}),
	).toBe("Service Unavailable");
});

test("something that is not a problem still says something", () => {
	expect(problemMessage("<html>proxy error</html>")).toBe(
		"Something went wrong. Try again.",
	);
	expect(problemMessage(undefined)).toBe("Something went wrong. Try again.");
});
