// How plain text subtitles look on this device, kept in its browser as
// Jellyfin's and Plex's players keep them. Styled subtitles keep the look
// their file gives them, and pictures are as they were made.
export const sizes = {
	small: { label: "Small", scale: 0.75 },
	normal: { label: "Normal", scale: 1 },
	large: { label: "Large", scale: 1.25 },
	larger: { label: "Larger", scale: 1.5 },
};
export const colours = {
	white: { label: "White", css: "#fff" },
	yellow: { label: "Yellow", css: "#ffe14d" },
};
export const backings = {
	box: { label: "Dark box", background: "rgb(0 0 0 / 0.75)", shadow: "none" },
	shadow: {
		label: "Shadow",
		background: "transparent",
		shadow: "0 0 3px #000, 0 0 3px #000, 0 0 3px #000",
	},
};

export type Appearance = {
	size: keyof typeof sizes;
	colour: keyof typeof colours;
	backing: keyof typeof backings;
};

const key = "photon:subtitles";
const usual: Appearance = { size: "normal", colour: "white", backing: "box" };

export function keptAppearance(): Appearance {
	let kept: Partial<Record<keyof Appearance, string>> = {};
	try {
		kept = JSON.parse(localStorage.getItem(key) ?? "{}") ?? {};
	} catch {
		// Storage refused, or not ours: the usual look.
	}
	const pick = <T extends object>(set: T, v: unknown, or: keyof T) =>
		(typeof v === "string" && Object.hasOwn(set, v) ? v : or) as keyof T;
	return {
		size: pick(sizes, kept.size, usual.size),
		colour: pick(colours, kept.colour, usual.colour),
		backing: pick(backings, kept.backing, usual.backing),
	};
}

export function keepAppearance(a: Appearance) {
	try {
		localStorage.setItem(key, JSON.stringify(a));
	} catch {
		// Not kept where storage is refused.
	}
}

// The look as the custom properties the player's ::cue rule reads.
export function cueStyle(a: Appearance): string {
	const backing = backings[a.backing];
	return [
		`--cue-scale: ${sizes[a.size].scale}`,
		`--cue-colour: ${colours[a.colour].css}`,
		`--cue-background: ${backing.background}`,
		`--cue-shadow: ${backing.shadow}`,
	].join("; ");
}
