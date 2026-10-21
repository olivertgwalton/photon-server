import { renderSVG } from "uqr";

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

// RFC 4648's base32, unpadded, as an otpauth:// address carries a key.
export function base32(bytes: Uint8Array): string {
	let out = "";
	let value = 0;
	let bits = 0;
	for (const byte of bytes) {
		value = (value << 8) | byte;
		bits += 8;
		for (; bits >= 5; bits -= 5) out += alphabet[(value >>> (bits - 5)) & 31];
	}
	if (bits > 0) out += alphabet[(value << (5 - bits)) & 31];
	return out;
}

// A new key for an authenticator app: RFC 4226's 160 random bits.
export function newKey(): string {
	return base32(crypto.getRandomValues(new Uint8Array(20)));
}

// The address an authenticator app is given the key by, named for the server
// and the profile, as its list shows them.
export function keyAddress(key: string, server: string, profile: string) {
	const issuer = encodeURIComponent(server);
	return `otpauth://totp/${issuer}:${encodeURIComponent(profile)}?secret=${key}&issuer=${issuer}`;
}

export const keyPicture = (address: string) =>
	renderSVG(address, { border: 2 });

// The key in fours, as it is easier to type.
export const spaced = (key: string) => key.replace(/(.{4})(?=.)/g, "$1 ");
