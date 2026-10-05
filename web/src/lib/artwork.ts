// The widths a picture is asked for at. The server resizes and keeps a copy
// at each width it is asked for, so every page draws from this one set.
const widths = { poster: [160, 240, 320, 480], still: [320, 480, 640, 960] };

export type Shape = keyof typeof widths;

export function artworkURL(id: string, width: number): string {
	return `/api/v1/artwork/${id}?width=${width}`;
}

export function artworkSrcset(id: string, shape: Shape): string {
	return widths[shape].map((w) => `${artworkURL(id, w)} ${w}w`).join(", ");
}

export function artworkSrc(id: string, shape: Shape): string {
	return artworkURL(id, widths[shape][1]);
}
