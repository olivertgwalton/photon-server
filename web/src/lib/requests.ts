import type { components } from "#lib/api/schema.d.ts";

type RequestState = components["schemas"]["RequestState"];

// Where a request stands for the one who asked: what happens once it is
// approved, sent on or failed there, is the admin's, so it reads as approved.
export function askerState(state: RequestState): RequestState {
	return state === "sent" || state === "failed" ? "approved" : state;
}
