import type { components } from "./schema.js";

type Problem = components["schemas"]["Problem"];

// What a refusal says to a reader where the server gives no detail of its own.
const messages: Partial<Record<Problem["code"], string>> = {
	sign_in_refused: "That name and password don't match.",
	wrong_secret: "That's not the right PIN or password.",
	pairing_not_found:
		"No device is showing that code. Check it, or start again on the device.",
	rate_limited: "Too many tries. Wait a minute and try again.",
	unauthenticated: "You've been signed out.",
	forbidden: "Your profile can't do that.",
	not_found: "That isn't here any more.",
};

function isProblem(value: unknown): value is Problem {
	return (
		typeof value === "object" &&
		value !== null &&
		typeof (value as Problem).code === "string" &&
		typeof (value as Problem).status === "number"
	);
}

// The sentence to show for a failed call: the server's detail, else what its
// code means, else its title.
export function problemMessage(value: unknown): string {
	if (!isProblem(value)) return "Something went wrong. Try again.";
	return value.detail || messages[value.code] || value.title;
}
