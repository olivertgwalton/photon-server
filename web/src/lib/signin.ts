import { problemMessage } from "#lib/api/problem.js";
import { CLIENT, deviceName } from "#lib/device.js";

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

// A browser signs in to a session its cookie keeps.
export function browserSession() {
	return {
		device: deviceName(navigator.userAgent),
		client: CLIENT,
		keep: "cookie" as const,
	};
}

// A call's answer, or what to tell the reader when it failed: the server's
// problem, kept as refused, or that the server never answered.
export async function attempt<
	A extends { error?: unknown; response: Response },
>(
	call: Promise<A>,
): Promise<
	| { answer: A; message?: undefined; refused?: undefined }
	| { answer?: undefined; message: string; refused?: unknown }
> {
	const answer = await call.catch(() => undefined);
	if (!answer) return { message: "The server isn't answering." };
	if (!answer.response.ok) {
		return { message: problemMessage(answer.error), refused: answer.error };
	}
	return { answer };
}

// Sends the browser to a provider, as the API answered where to; a refusal is
// answered as what to say.
export async function leaveFor(
	call: Promise<{
		data?: { url: string };
		error?: unknown;
		response: Response;
	}>,
): Promise<string> {
	const { answer, message } = await attempt(call);
	if (answer?.data) location.assign(answer.data.url);
	return message ?? "";
}
