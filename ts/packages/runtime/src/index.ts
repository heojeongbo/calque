/**
 * The TypeScript runtime calque-generated code depends on, minus any store.
 *
 * Nothing here imports a database. That is the whole point of the split: a
 * consumer who does not use Dexie never installs it, and a second backend is a
 * package beside `@heojeongbo/calque-dexie` rather than a fork of it.
 */
export type { Key } from "./key";
export type {
	InputOf,
	OutputOf,
	QueryDesc,
	QueryDescOf,
	RefOf,
} from "./service";
export * as unsafe from "./unsafe";
export * as uuid from "./uuid";
