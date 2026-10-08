import { toast } from "svelte-sonner";
import { libraryChange } from "#lib/admin/library.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";

// Adds the library a LibraryForm describes, and says so. It starts as the
// server's defaults; what the form says differently is set before its first
// scan has read much. Answers the library, or nothing once a refusal is said.
export async function addLibrary(
	form: FormData,
): Promise<components["schemas"]["AdminLibrary"] | undefined> {
	const api = client();
	const added = await api.POST("/api/v1/admin/libraries", {
		body: {
			name: String(form.get("name") ?? ""),
			kind: form.get("kind") === "shows" ? "shows" : "movies",
			root: String(form.get("root") ?? ""),
		},
	});
	if (added.error) {
		toast.error(problemMessage(added.error));
		return;
	}
	const { error } = await api.PATCH("/api/v1/admin/libraries/{id}", {
		params: { path: { id: added.data.id } },
		body: libraryChange(form, added.data),
	});
	if (error) toast.error(problemMessage(error));
	else toast.success(`${added.data.name} was added and is being scanned.`);
	return added.data;
}
