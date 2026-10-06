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
