import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.ts";
import { problemMessage } from "#lib/api/problem.ts";
import type { components } from "#lib/api/schema.d.ts";

type Version = components["schemas"]["VersionPage"];

// The profile a play is sent with to be given a version's file as it is: its
// own container and sound, nothing to convert.
export function asItIs(
	version: Version,
): components["schemas"]["ClientProfile"] {
	return {
		containers: [version.container],
		video: [],
		audio: version.streams
			.filter((s) => s.kind === "audio")
			.map((s) => ({ codec: s.codec })),
		max_bitrate_kbps: 0,
		parts: "each",
	};
}

// Saves a version's first file as it is. It comes from a playback of it, as
// every file does, at an address that opens it alone; an ebook is served as
// the download it is, named as its file is, and anything else is named
// here.
export async function downloadFile(
	id: string,
	version: Version,
	name?: string,
): Promise<boolean> {
	const { data, error } = await client().POST("/api/v1/titles/{id}/play", {
		params: { path: { id } },
		body: { version_id: version.id, profile: asItIs(version) },
	});
	const url = data?.parts?.[0]?.url;
	if (!url) {
		toast.error(problemMessage(error));
		return false;
	}
	const link = document.createElement("a");
	link.href = url;
	link.download = name ? `${name}.${extension(version.container)}` : "";
	link.click();
	return true;
}

// A container's usual file extension: a sound file in MP4 is an .m4a.
function extension(container: string): string {
	return container === "mp4" ? "m4a" : container;
}
