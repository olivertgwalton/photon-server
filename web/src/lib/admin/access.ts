import type { components } from "#lib/api/schema.js";

// What a form holding AccessFields lets a profile see. No library ticked is
// every library, as the server reads it.
export function accessOf(form: FormData): components["schemas"]["Access"] {
	const age = String(form.get("max_age") ?? "any");
	return {
		max_age: age === "any" ? null : Number(age),
		unrated: form.get("unrated") === "block" ? "block" : "allow",
		libraries: form.getAll("libraries").map(String),
	};
}
