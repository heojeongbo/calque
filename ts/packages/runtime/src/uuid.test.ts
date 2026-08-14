import { describe, expect, it } from "vitest";
import { str_u8, u8_str } from "./uuid";

describe("uuid", () => {
	const bytes = new Uint8Array([
		0x55, 0x0e, 0x84, 0x00, 0xe2, 0x9b, 0x41, 0xd4, 0xa7, 0x16, 0x44, 0x66, 0x55, 0x44, 0x00,
		0x00,
	]);
	const text = "550e8400-e29b-41d4-a716-446655440000";

	it("round-trips", () => {
		expect(u8_str(bytes)).toBe(text);
		expect(str_u8(text)).toEqual(bytes);
	});

	it("pads a byte that needs a leading zero", () => {
		const v = new Uint8Array(16);
		v[0] = 0x0a;
		expect(u8_str(v)).toBe("0a000000-0000-0000-0000-000000000000");
	});

	// A key that is not sixteen bytes is not a key. Returning undefined rather
	// than a truncated string is what lets the generated code refuse it with
	// InvalidArgument instead of looking up something that does not exist.
	it("refuses anything that is not sixteen bytes", () => {
		expect(u8_str(undefined)).toBeUndefined();
		expect(u8_str(new Uint8Array(15))).toBeUndefined();
		expect(u8_str(new Uint8Array(0))).toBeUndefined();
	});

	it("uses only the first sixteen bytes of a longer array", () => {
		const long = new Uint8Array(20);
		long.set(bytes);
		expect(u8_str(long)).toBe(text);
	});

	it("accepts the spellings a uuid arrives in", () => {
		for (const s of [text, `{${text}}`, text.toUpperCase()]) {
			expect(str_u8(s)).toEqual(bytes);
		}
	});

	/**
	 * KNOWN BUG, pinned deliberately, and found by writing this test rather
	 * than by reading the code.
	 *
	 * The published runtime's doc comment claims `urn:uuid:...` is accepted.
	 * It is not: stripping non-hex characters leaves the "d" of "uuid" behind,
	 * so the input becomes "d550e8400..." — thirty-three characters, truncated
	 * to thirty-two, parsed successfully, and wrong. Every byte is shifted.
	 *
	 * It returns a plausible uuid rather than undefined, which is the worst of
	 * the three possible behaviours: a caller cannot tell it failed.
	 *
	 * Reproduced because calque's first version has to behave exactly as the
	 * generator it replaces. The fix is to strip a `urn:uuid:` prefix before
	 * the hex filter, and it belongs in its own commit.
	 */
	it("mangles urn:uuid: rather than refusing it", () => {
		const got = str_u8(`urn:uuid:${text}`);
		expect(got).toBeDefined();
		expect(got).not.toEqual(bytes);
		expect(got?.[0]).toBe(0xd5);
	});

	it("refuses a string that is not thirty-two hex characters", () => {
		expect(str_u8(undefined)).toBeUndefined();
		expect(str_u8("")).toBeUndefined();
		expect(str_u8("550e8400")).toBeUndefined();
		expect(str_u8("zzzz")).toBeUndefined();
	});
});
