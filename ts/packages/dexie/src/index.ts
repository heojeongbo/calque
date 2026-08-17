/**
 * The Dexie/IndexedDB backend runtime for calque-generated code.
 *
 * It re-exports `@heojeongbo/calque-runtime`, so generated code needs exactly
 * one import specifier. The split still matters: the runtime never names Dexie,
 * so a second backend is a package beside this one rather than a fork of it.
 */
export * from "@heojeongbo/calque-runtime";
export type { DbOf, EntityOf, ValueOf } from "./db";
export { TableBase } from "./db";
