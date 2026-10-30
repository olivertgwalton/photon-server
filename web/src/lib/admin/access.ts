import type { components } from "#lib/api/schema.d.ts";

// What a form holding AccessFields lets a profile see. No library ticked is
// every library, as the server reads it.
export function accessOf(form: FormData): components["schemas"]["Access"] {
	const age = String(form.get("max_age") ?? "any");
	const commonSenseAge = String(form.get("max_common_sense_age") ?? "any");
	return {
		max_age: age === "any" ? null : Number(age),
		unrated: form.get("unrated") === "block" ? "block" : "allow",
		max_common_sense_age:
			commonSenseAge === "any" ? null : Number(commonSenseAge),
		libraries: form.getAll("libraries").map(String),
	};
}
