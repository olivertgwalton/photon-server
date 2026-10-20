import { describe, expect, test } from "bun:test";
import { askerState } from "./requests.ts";

describe("askerState", () => {
	test("reads what happens after approval as approved", () => {
		expect(askerState("sent")).toBe("approved");
		expect(askerState("failed")).toBe("approved");
	});
	test("leaves what the asker decides by as it is", () => {
		for (const s of ["pending", "approved", "declined", "available"] as const)
			expect(askerState(s)).toBe(s);
	});
});
