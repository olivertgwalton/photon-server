import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";

// Makes an invite for a profile without a password to choose one; a refusal
// says why in a toast.
export async function invite(
	id: string,
): Promise<components["schemas"]["Invite"] | undefined> {
	const { data, error } = await client().POST(
		"/api/v1/admin/profiles/{id}/invites",
		{ params: { path: { id } } },
	);
	if (error) toast.error(problemMessage(error));
	return data;
}

// How long an invite lasts, as the reader is told it.
export function inviteLasts(ms: number): string {
	const days = Math.round(ms / 86_400_000);
	return days === 1 ? "a day" : `${days} days`;
}
