import { client } from "#lib/api/client.ts";
import { LOGIN } from "#lib/session.ts";
import { goto } from "$app/navigation";

// Signs this browser out, on the server too, so it leaves the devices list and
// its cookie is cleared.
export async function logOut() {
	await client()
		.POST("/api/v1/auth/logout")
		.catch(() => {});
	await goto(LOGIN, { refreshAll: true });
}
