import type { components } from "#lib/api/schema.js";

type Kind = components["schemas"]["CreditKind"];

const jobs: Record<Kind, string> = {
	actor: "",
	guest_star: "Guest star",
	director: "Director",
	writer: "Writer",
	producer: "Producer",
	composer: "Composer",
	creator: "Creator",
};

export const performs = (kind: Kind) =>
	kind === "actor" || kind === "guest_star";

export type Folded<T> = {
	// Every credit folded in, in the order given.
	all: T[];
	kinds: Kind[];
	// The parts played, then the jobs done: "Host, Director".
	said: string;
};

// One entry per person on a title, or per title in a person's work, as the
// Photon apps fold a credit: parts and jobs joined, performers first, then the
// rest in the order credited. A person credited as creator and writer is one
// card, and so one key.
export function fold<T>(
	credits: T[],
	key: (c: T) => string,
	kind: (c: T) => Kind,
	role: (c: T) => string | undefined,
): Folded<T>[] {
	const byKey = new Map<
		string,
		Folded<T> & { parts: string[]; jobs: string[] }
	>();
	for (const c of credits) {
		let f = byKey.get(key(c));
		if (!f) {
			f = { all: [], kinds: [], said: "", parts: [], jobs: [] };
			byKey.set(key(c), f);
		}
		f.all.push(c);
		if (!f.kinds.includes(kind(c))) f.kinds.push(kind(c));
		const words = role(c) || jobs[kind(c)];
		const into = performs(kind(c)) ? f.parts : f.jobs;
		if (words && !into.includes(words)) into.push(words);
	}
	const out = [...byKey.values()].map(({ parts, jobs, ...f }) => ({
		...f,
		said: [...parts, ...jobs].join(", "),
	}));
	const acted = (f: Folded<T>) => (f.kinds.some(performs) ? 0 : 1);
	return out.sort((a, b) => acted(a) - acted(b));
}

// A title's cast and crew, one per person, by the copy with a photograph.
export function castOf(credits: components["schemas"]["CreditRef"][]) {
	return fold(
		credits,
		(c) => c.person_id,
		(c) => c.kind,
		(c) => c.role,
	).map((f) => ({
		...(f.all.find((c) => c.photo) ?? f.all[0]),
		id: f.all[0].person_id,
		kinds: f.kinds,
		said: f.said,
	}));
}

const crafts: [string, Kind[]][] = [
	["Acting", ["actor", "guest_star"]],
	["Directing", ["director"]],
	["Creating", ["creator"]],
	["Writing", ["writer"]],
	["Producing", ["producer"]],
	["Music", ["composer"]],
];

// A person's work, one card a title whatever they did on it, under the first
// of these crafts they did, acting first. Each card's caption is its year and
// what they did.
export function workOf(credits: components["schemas"]["Credit"][]) {
	const titles = fold(
		credits,
		(c) => c.id,
		(c) => c.credit,
		(c) => c.role,
	);
	const craftOf = (kinds: Kind[]) =>
		crafts.find(([, ks]) => kinds.some((k) => ks.includes(k)))?.[0];
	return crafts
		.map(([name]) => {
			const mine = titles.filter((f) => craftOf(f.kinds) === name);
			return {
				name,
				slug: name.toLowerCase(),
				cards: mine.map((f) => f.all[0]),
				captions: mine.map((f) =>
					[f.all[0].year, f.said].filter(Boolean).join(" · "),
				),
			};
		})
		.filter((g) => g.cards.length);
}
