import { expect, test } from "bun:test";
import { base32, keyAddress, newKey, spaced } from "./authenticator.ts";

test("a key is written as an authenticator app reads it", () => {
	// RFC 4648's own examples, and RFC 6238's test key.
	const bytes = (s: string) => new TextEncoder().encode(s);
	expect(base32(bytes("f"))).toBe("MY");
	expect(base32(bytes("foobar"))).toBe("MZXW6YTBOI");
	expect(base32(bytes("12345678901234567890"))).toBe(
		"GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
	);
	expect(newKey()).toMatch(/^[A-Z2-7]{32}$/);
	expect(spaced("GEZDGNBVGY")).toBe("GEZD GNBV GY");
	expect(keyAddress("GEZD", "Den & Co", "Ada")).toBe(
		"otpauth://totp/Den%20%26%20Co:Ada?secret=GEZD&issuer=Den%20%26%20Co",
	);
});
