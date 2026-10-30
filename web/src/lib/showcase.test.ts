import { expect, test } from "bun:test";
import type { components } from "./api/schema.d.ts";
import { showcasePool } from "./showcase.ts";

type Card = components["schemas"]["Card"];

const card = (id: string) =>
	({ id, kind: "movie", title: id, added_at: "" }) as Card;
const library = { id: "l", name: "Films" };
const answered = (...ids: string[]) =>
	Promise.resolve({ items: ids.map(card) });

// Refused, as the page takes a row's refusal: whether or not it is asked for.
const refused = () => {
	const p = Promise.reject(new Error("refused"));
	p.catch(() => {});
	return p;
};

const rails = () => [
	{
		key: "continue_watching",
		kind: "continue_watching" as const,
		cards: answered("half-seen"),
	},
	{
		key: "recently_added_films/l",
		kind: "recently_added_films" as const,
		library,
		cards: refused(),
	},
	{
		key: "recently_released/l",
		kind: "recently_released" as const,
		library,
		cards: answered(),
	},
	{
		key: "top_rated_unwatched/l",
		kind: "top_rated_unwatched" as const,
		library,
		cards: answered("best", "next"),
	},
];

test("the curated showcase is the first library row with titles, never the reader's", async () => {
	const pool = await showcasePool(rails(), undefined);
	expect(pool.cards.map((c) => c.id)).toEqual(["best", "next"]);
	expect(pool.turn).toBe("rests");
});

test("a row the reader chose slides, whoever's it is", async () => {
	const pool = await showcasePool(rails(), "continue_watching");
	expect(pool.cards.map((c) => c.id)).toEqual(["half-seen"]);
	expect(pool.turn).toBe("slides");
});

test("a chosen row that is empty or gone leaves the showcase curated", async () => {
	for (const row of ["recently_released/l", "gone"]) {
		const pool = await showcasePool(rails(), row);
		expect(pool.cards.map((c) => c.id)).toEqual(["best", "next"]);
		expect(pool.turn).toBe("rests");
	}
});
