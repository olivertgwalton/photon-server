import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];

function videoURL(site: string, key: string): string | undefined {
	switch (site.toLowerCase()) {
		case "youtube":
			return `https://www.youtube.com/watch?v=${encodeURIComponent(key)}`;
		case "vimeo":
			return `https://vimeo.com/${encodeURIComponent(key)}`;
	}
}

export type Extra =
	| { id: string; extra: Schemas["ExtraCard"] }
	| { id: string; video: Schemas["VideoLink"] & { url: string } };

// A title's extras on the server, then the videos a provider links to
// elsewhere, on a site a browser can open.
export function extrasOf(t: Schemas["TitlePage"]): Extra[] {
	return [
		...(t.extras ?? []).map((extra) => ({ id: extra.id, extra })),
		...(t.videos ?? []).flatMap((v, i) => {
			const url = videoURL(v.site, v.key);
			return url
				? [{ id: `${v.site}:${v.key}:${i}`, video: { ...v, url } }]
				: [];
		}),
	];
}
