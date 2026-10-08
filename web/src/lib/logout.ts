import { goto } from "$app/navigation";
import { forget } from "#lib/answers.js";
import { client } from "#lib/api/client.js";
import { LOGIN } from "#lib/session.js";

// Signs this browser out, on the server too, so it leaves the devices list and
// its cookie is cleared.
export async function logOut() {
	await client()
		.POST("/api/v1/auth/logout")
		.catch(() => {});
	forget();
	await goto(LOGIN, { refreshAll: true });
}
