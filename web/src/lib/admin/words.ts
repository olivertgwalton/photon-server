import type { components } from "#lib/api/schema.js";
import { fullTitle } from "#lib/format.js";

type Schemas = components["schemas"];

// Where a node's limit on transcodes at once comes from.
export const limitSources: Record<Schemas["LimitSource"], string> = {
	automatic: "worked out from the encoder",
	set: "set here",
};

// Whether a node takes new streams, and how far one draining has got.
export function nodeAvailability(n: Schemas["KnownNode"]): string {
	if (n.availability === "active") return "Takes new streams";
	const left = n.online?.transcodes ?? 0;
	if (!left) return "Drained · safe to stop";
	return `Draining · ${left === 1 ? "1 stream" : `${left} streams`} left`;
}

// Videos being transcoded of the most at once, as words: "3 of 8", or "3, no limit".
export function transcodeLoad(active: number, limit?: number): string {
	return limit ? `${active} of ${limit}` : `${active}, no limit`;
}

// The roles a profile of this role may give: an admin any, a manager users.
export function roleOptions(
	giver: Schemas["Role"],
	roles: Schemas["Vocabulary"]["roles"],
) {
	return Object.entries(roles)
		.filter(([value]) => giver === "admin" || value === "user")
		.map(([value, label]) => ({ value: value as Schemas["Role"], label }));
}

// Each item's name by its id, for lists that name what an event was about.
export function byName(
	items: readonly { id: string; name: string }[],
): Map<string, string> {
	return new Map(items.map((i) => [i.id, i.name]));
}

// A played title as one line: a show's episode by its show and place in it.
export function playedTitle(t: Schemas["PlaybackTitle"]): string {
	return fullTitle(t, t.kind === "episode" ? t.show : undefined);
}

// The reverse of timecode: "1:02:03", "2:03" or "3" seconds, with an optional
// fraction; undefined for anything else.
export function parseClock(text: string): number | undefined {
	const match = /^(?:(?:(\d+):)?(\d{1,2}):)?(\d{1,2}(?:\.\d{1,3})?)$/.exec(
		text.trim(),
	);
	if (!match) return undefined;
	const [, h = "0", m = "0", s] = match;
	return Math.round((Number(h) * 3600 + Number(m) * 60 + Number(s)) * 1000);
}

const units: [
	Intl.RelativeTimeFormatUnit & ("day" | "hour" | "minute"),
	number,
][] = [
	["day", 86_400],
	["hour", 3_600],
	["minute", 60],
];

// How long ago, or how far ahead, a moment is, in its largest whole unit.
export function relative(at: string | number, now: number): string {
	const seconds = Math.round(
		((typeof at === "number" ? at : Date.parse(at)) - now) / 1000,
	);
	const format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
	for (const [unit, size] of units) {
		if (Math.abs(seconds) >= size)
			return format.format(Math.trunc(seconds / size), unit);
	}
	return format.format(seconds, "second");
}

// How long something has run, in its largest whole unit: "3 days", "5 minutes".
export function elapsed(ms: number): string {
	const seconds = Math.max(0, Math.round(ms / 1000));
	const [unit, size] = units.find(([, size]) => seconds >= size) ?? [
		"second",
		1,
	];
	return new Intl.NumberFormat(undefined, {
		style: "unit",
		unit,
		unitDisplay: "long",
	}).format(Math.trunc(seconds / size));
}

export const when = new Intl.DateTimeFormat(undefined, {
	dateStyle: "medium",
	timeStyle: "short",
});
