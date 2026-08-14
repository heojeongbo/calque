/**
 * The types a store can use as a key.
 *
 * This is IndexedDB's valid-key subset, which is narrower than proto's: no
 * bigint, no byte array, no composite object. It lives in the runtime rather
 * than in the Dexie adapter because generated code names it, and generated code
 * must not have to know which store it ends up on.
 *
 * A store with a wider key domain does not have to narrow to this — it is the
 * floor every backend can carry, and the reason a uuid is stored as text.
 */
export type Key = string | number;
