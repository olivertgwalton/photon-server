// The widths a picture is asked for at. The server resizes and keeps a copy
// at each width it is asked for, so every page draws from this one set.
const widths = {
	poster: [160, 240, 320, 480],
	still: [320, 480, 640, 960],
	// An album's cover or an artist's picture, as Plex and Jellyfin draw music.
	square: [160, 240, 320, 480],
	// A page's backdrop, from a phone to a television.
	backdrop: [960, 1280, 1920, 2560],
};

export type Shape = "poster" | "still" | "square";

// How a card of each shape is drawn, and how tall it is for its width.
export const aspect: Record<Shape, { class: string; height: number }> = {
	poster: { class: "aspect-2/3", height: 3 / 2 },
	still: { class: "aspect-video", height: 9 / 16 },
	square: { class: "aspect-square", height: 1 },
};
export type ArtworkShape = keyof typeof widths;

export function artworkURL(id: string, width: number): string {
	return `/api/v1/artwork/${id}?width=${width}`;
}

export function artworkSrcset(id: string, shape: ArtworkShape): string {
	return widths[shape].map((w) => `${artworkURL(id, w)} ${w}w`).join(", ");
}

export function artworkSrc(id: string, shape: ArtworkShape): string {
	return artworkURL(id, widths[shape][1]);
}
