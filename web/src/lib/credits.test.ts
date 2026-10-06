import { expect, test } from "bun:test";
import type { components } from "./api/schema.d.ts";
import { fold } from "./credits.ts";

type C = {
	id: string;
	kind: components["schemas"]["CreditKind"];
	role?: string;
	photo?: string;
};

const byPerson = (credits: C[]) =>
	fold(
		credits,
		(c) => c.id,
		(c) => c.kind,
		(c) => c.role,
	);

test("a person credited for several jobs is one entry, jobs joined", () => {
	const folded = byPerson([
		{ id: "michael", kind: "creator" },
		{ id: "kat", kind: "actor", role: "Max" },
		{ id: "michael", kind: "writer" },
		{ id: "michael", kind: "writer" },
	]);
	expect(folded.map((f) => [f.all[0].id, f.said])).toEqual([
		["kat", "Max"],
		["michael", "Creator, Writer"],
	]);
});

test("a performer who also directed leads with the part, and stays a performer", () => {
	const [f] = byPerson([
		{ id: "clint", kind: "director" },
		{ id: "clint", kind: "actor", role: "Frankie", photo: "p" },
	]);
	expect(f.said).toBe("Frankie, Director");
	expect(f.kinds).toEqual(["director", "actor"]);
	expect(f.all.find((c) => c.photo)?.photo).toBe("p");
});
