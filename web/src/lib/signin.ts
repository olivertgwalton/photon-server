import { problemMessage } from "#lib/api/problem.js";

// What the server says in `refused` when a provider's answer signed no one
// in, said to the reader.
const refusals: Record<string, string> = {
	expired:
		"That sign-in took too long, or was started in another browser. Try again.",
	denied: "Your provider didn't sign you in.",
	failed:
		"Your provider's answer couldn't be checked. Try again, or ask an admin to check how it's set up.",
	not_linked:
		"That account isn't linked to a profile here. Log in with your password and link it in Settings, or ask an admin.",
	not_in_group: "That account isn't allowed to sign in here.",
	linked_elsewhere: "That account is already linked to another profile.",
};

export function refusalMessage(reason: string | null): string | undefined {
	if (!reason) return undefined;
	return refusals[reason] ?? refusals.failed;
}

// Sends the browser to a provider, as the API answered where to; a refusal is
// answered as what to say.
export async function leaveFor(
	call: Promise<{ data?: { url: string }; error?: unknown }>,
): Promise<string> {
	const { data, error } = await call.catch(() => ({
		data: undefined,
		error: undefined,
	}));
	if (data) {
		location.assign(data.url);
		return "";
	}
	return error ? problemMessage(error) : "The server isn't answering.";
}
