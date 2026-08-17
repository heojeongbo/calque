/**
 * Delete a property.
 *
 * It exists because a stored row must not carry protobuf's synthetic fields,
 * and removing them is the one thing the type system cannot express about a
 * message that has already been widened to `any`.
 */
export function rm(obj: Record<string, unknown>, key: string): void {
	delete obj[key];
}

/** Assign a property on a possibly-absent object. */
export function set<T extends object, V>(
	obj: T | undefined,
	key: keyof T,
	value: V,
): void {
	if (obj === undefined) return;
	(obj as Record<keyof T, unknown>)[key] = value;
}
