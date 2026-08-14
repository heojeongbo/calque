/**
 * Convert a 16-byte Uint8Array into a UUID string (8-4-4-4-12).
 *
 * Notes:
 * - If `v` is longer than 16 bytes, only the first 16 bytes are used.
 * - If `v` is not provided or has length other than 16, the function returns `undefined`.
 * - The returned string uses lowercase hex (e.g. "550e8400-e29b-41d4-a716-446655440000").
 *
 * This is the representation both of calque's targets store. IndexedDB cannot
 * index a byte array, and Go's `google/uuid` writes the same hyphenated text
 * through `driver.Valuer`, so the two databases hold the same bytes without
 * either side having been told about the other.
 */
export function u8_str(v?: Uint8Array): string | undefined {
	const b = v?.slice(0, 16);
	if (b?.length !== 16) return undefined;

	let hex = "";
	for (let i = 0; i < 16; i++) {
		const byte = b[i] as number;
		const h = byte.toString(16);
		hex += h.length === 1 ? `0${h}` : h;
	}
	return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/**
 * Parse a UUID string into a 16-byte Uint8Array.
 *
 * Any non-hex character is stripped first, so the standard spellings all work:
 * `550e8400-e29b-41d4-a716-446655440000`, `{...}`, `urn:uuid:...`. Exactly 32
 * hex characters must remain; anything else returns `undefined` rather than a
 * partial key.
 */
export function str_u8(v?: string): Uint8Array | undefined {
	if (!v) return undefined;

	const s = v.replace(/[^0-9a-fA-F]/g, "").slice(0, 32);
	if (s.length !== 32) return undefined;

	const out = new Uint8Array(16);
	for (let i = 0; i < 32; i += 2) {
		const parsed = Number.parseInt(s.slice(i, i + 2), 16);
		if (Number.isNaN(parsed)) return undefined;
		out[i >> 1] = parsed;
	}
	return out;
}
