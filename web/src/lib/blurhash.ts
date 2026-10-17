// A BlurHash (https://blurha.sh) is a picture's few strongest waves of colour,
// which the server takes as it keeps the picture. Drawn under the picture, it
// stands in while the picture loads.

const digits =
	"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~";

const base83 = (s: string) =>
	[...s].reduce((v, c) => v * 83 + digits.indexOf(c), 0);

const toLinear = (v: number) => {
	const f = v / 255;
	return f <= 0.04045 ? f / 12.92 : ((f + 0.055) / 1.055) ** 2.4;
};

const toSRGB = (v: number) => {
	const f = Math.max(0, Math.min(1, v));
	return Math.trunc(
		f <= 0.0031308
			? f * 12.92 * 255 + 0.5
			: (1.055 * f ** (1 / 2.4) - 0.055) * 255 + 0.5,
	);
};

const signPow = (v: number, e: number) => Math.sign(v) * Math.abs(v) ** e;

// The RGBA pixels of a hash drawn width by height, as the reference decoder
// draws them.
export function decodeBlurhash(hash: string, width: number, height: number) {
	const size = base83(hash[0]);
	const cx = (size % 9) + 1;
	const cy = Math.floor(size / 9) + 1;
	const max = (base83(hash[1]) + 1) / 166;
	const colours: number[][] = [];
	for (let i = 0; i < cx * cy; i++) {
		if (i === 0) {
			const v = base83(hash.slice(2, 6));
			colours.push([v >> 16, (v >> 8) & 255, v & 255].map(toLinear));
		} else {
			const v = base83(hash.slice(4 + i * 2, 6 + i * 2));
			colours.push(
				[Math.floor(v / 361), Math.floor(v / 19) % 19, v % 19].map(
					(q) => signPow((q - 9) / 9, 2) * max,
				),
			);
		}
	}
	const pixels = new Uint8ClampedArray(width * height * 4);
	for (let y = 0; y < height; y++) {
		for (let x = 0; x < width; x++) {
			const rgb = [0, 0, 0];
			for (let j = 0; j < cy; j++) {
				for (let i = 0; i < cx; i++) {
					const basis =
						Math.cos((Math.PI * x * i) / width) *
						Math.cos((Math.PI * y * j) / height);
					const c = colours[i + j * cx];
					for (let k = 0; k < 3; k++) rgb[k] += c[k] * basis;
				}
			}
			const at = 4 * (x + y * width);
			for (let k = 0; k < 3; k++) pixels[at + k] = toSRGB(rgb[k]);
			pixels[at + 3] = 255;
		}
	}
	return pixels;
}

// A picture's average colour is the first and strongest of its waves, which a
// hash writes plainly: the four digits after the two that say its shape. It is
// read without drawing the hash at all.
function average(hash: string): number {
	return base83(hash.slice(2, 6));
}

// Whether a hash's picture is dark: its average colour, the hash's first, under
// the luma at which dark lettering is lost on a dark page (navy, maroon and
// black are, a mid grey is not). The server hashes a logo by its letters alone,
// so a logo's hash says whether its letters are dark.
export function isDark(hash: string | undefined): boolean {
	if (!hash || hash.length < 6) return false;
	const v = average(hash);
	const [r, g, b] = [v >> 16, (v >> 8) & 255, v & 255].map((c) => c / 255);
	return 0.2126 * r + 0.7152 * g + 0.0722 * b < darkLuma;
}

// A corner with less colour than this is no colour at all: it tints a page
// muddy rather than giving it the title's colour.
const tintSaturation = 0.2;
// A tint is drawn at this lightness whatever the backdrop's own: the hue is
// what the page wants, where a pale backdrop would wash it and a dark one
// blacken it. The browser holds the hue and saturation and takes this.
const tintLightness = "35%";

// How much colour a pixel holds, as its saturation told apart from how light
// it is: a pale blue and a deep one are both blue.
function colourfulness(r: number, g: number, b: number): number {
	const [R, G, B] = [r / 255, g / 255, b / 255];
	const light = (Math.max(R, G, B) + Math.min(R, G, B)) / 2;
	const spread = Math.max(R, G, B) - Math.min(R, G, B);
	return spread === 0 ? 0 : spread / (1 - Math.abs(2 * light - 1));
}

// The colour to tint the page a picture fades into: the most colourful quarter
// of it, which the hash is drawn at two by two to find. A picture's average is
// a grey far more often than the picture is — a warm corner and a cold one
// cancel — so the quarters are asked, as Plex tints a page from its artwork's
// corners rather than its mean. Nothing is answered where there is no hash to
// read or no colour in any quarter, and such a page is left as it was.
export function tintColour(hash: string | undefined): string | undefined {
	if (!hash || hash.length < 6) return undefined;
	const quarters = decodeBlurhash(hash, 2, 2);
	let strongest = tintSaturation;
	let tint: number[] | undefined;
	for (let i = 0; i < 4; i++) {
		const quarter = Array.from(quarters.slice(i * 4, i * 4 + 3));
		const colour = colourfulness(quarter[0], quarter[1], quarter[2]);
		if (colour >= strongest) [strongest, tint] = [colour, quarter];
	}
	if (!tint) return undefined;
	const hex = tint.map((c) => c.toString(16).padStart(2, "0")).join("");
	return `hsl(from #${hex} h s ${tintLightness})`;
}

const darkLuma = 0.3;

// The blur is drawn this small and stretched: it has no detail to lose. Each
// side holds the hash's nine waves at most without folding them, at a seventh
// of the time the reference decoder's 32 takes, which a wall's first screen
// of new hashes waits on.
const drawn = 16;

// Each hash is drawn once: a wall redraws its cards as it scrolls, and a
// title shows on many rows. Each is under a kilobyte, so the oldest go past a
// few thousand.
const drawnStyles = new Map<string, string>();
const drawnLimit = 4_000;

// The style that draws a hash behind an <img>, stretched to its box, so the
// picture loads over it with nothing moving.
export function blurStyle(hash: string | undefined): string | undefined {
	if (!hash) return undefined;
	const drawnStyle = drawnStyles.get(hash);
	if (drawnStyle) return drawnStyle;
	const style = drawBlur(hash);
	if (style) drawnStyles.set(hash, style);
	if (drawnStyles.size > drawnLimit)
		drawnStyles.delete(drawnStyles.keys().next().value as string);
	return style;
}

function drawBlur(hash: string): string | undefined {
	const canvas = document.createElement("canvas");
	canvas.width = canvas.height = drawn;
	const context = canvas.getContext("2d");
	if (!context) return undefined;
	context.putImageData(
		new ImageData(decodeBlurhash(hash, drawn, drawn), drawn),
		0,
		0,
	);
	return `background: url(${canvas.toDataURL()}) 0 0 / 100% 100%`;
}
