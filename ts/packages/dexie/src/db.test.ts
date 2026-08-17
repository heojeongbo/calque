import "fake-indexeddb/auto";

import type { DescMessage } from "@bufbuild/protobuf";
import Dexie from "dexie";
import { IDBFactory } from "fake-indexeddb";
import { beforeEach, describe, expect, it } from "vitest";

import { type DbOf, TableBase } from "./db";

// A hand-rolled stand-in for a protoc-gen-es descriptor. The real one carries
// far more, but TableBase only ever reads typeName and hands the descriptor to
// `clone`, which the subclass below overrides past.
const RowSchema = { typeName: "test.Row" } as unknown as DescMessage;

type Row = {
	$typeName: "test.Row";
	id: string;
	name: string;
	version: number;
};

/**
 * The shape a generated table has: dehydrate to a key plus a row, hydrate back,
 * and compare by version.
 */
class Rows extends TableBase<DescMessage> {
	protected override _dehydrate(v: Row): [string, unknown] {
		return [v.id, { ...v }];
	}

	protected override _hydrate(v: unknown): Row {
		return v as Row;
	}

	protected override _versioned(): boolean {
		return true;
	}

	override _compare(a: Row, b: Row): number {
		return a.version - b.version;
	}

	// `clone` needs a real descriptor; generated code has one and this test does
	// not, so the copy step is a shallow one.
	protected override _clone(v: never): never {
		return { ...(v as Row) } as never;
	}

	async put(v: Row): Promise<boolean> {
		return this._reconcile(v as never);
	}

	async read(id: string): Promise<Row> {
		return this._query((t) => t.get(id)) as unknown as Promise<Row>;
	}
}

function row(id: string, name: string, version: number): Row {
	return { $typeName: "test.Row", id, name, version };
}

describe("TableBase over a real IndexedDB", () => {
	let db: DbOf<DescMessage>;
	let rows: Rows;

	beforeEach(() => {
		const indexedDB = new IDBFactory();
		db = new Dexie("test", { indexedDB }) as DbOf<DescMessage>;
		db.version(1).stores({ "test.Row": "id" });
		rows = new Rows(db, RowSchema);
	});

	it("writes and reads a row back", async () => {
		await rows.put(row("a", "first", 1));
		expect((await rows.read("a")).name).toBe("first");
	});

	it("applies a newer value", async () => {
		await rows.put(row("a", "first", 1));
		await rows.put(row("a", "second", 2));
		expect((await rows.read("a")).name).toBe("second");
	});

	// The behaviour that matters, and the reason this file exists.
	it("does not apply an older value", async () => {
		await rows.put(row("a", "second", 2));
		await rows.put(row("a", "first", 1));
		expect((await rows.read("a")).name).toBe("second");
	});

	it("reports a missing row rather than resolving undefined", async () => {
		await expect(rows.read("nope")).rejects.toThrow(/not found/);
	});

	/**
	 * conformance.md item 5, fixed in v0.2.0. Until then this resolved `true`
	 * whatever happened, so a caller's `if (!ok)` branch never ran.
	 */
	it("reports that it did not write", async () => {
		await rows.put(row("a", "second", 2));

		const applied = await rows.put(row("a", "first", 1));
		expect(applied).toBe(false);
		expect((await rows.read("a")).name).toBe("second");
	});

	it("reports that it did write", async () => {
		expect(await rows.put(row("a", "first", 1))).toBe(true);
		expect(await rows.put(row("a", "second", 2))).toBe(true);
	});

	// Equal versions are not newer, so the write is declined. Otherwise two
	// writers at the same version would each overwrite the other and the last
	// one to arrive would win, which is the thing a version field exists to
	// stop.
	it("declines an equal version", async () => {
		await rows.put(row("a", "first", 1));

		expect(await rows.put(row("a", "second", 1))).toBe(false);
		expect((await rows.read("a")).name).toBe("first");
	});

	// An unversioned table has nothing to compare, so every write lands and
	// says so.
	it("always writes when the table is not versioned", async () => {
		const plain = new (class extends Rows {
			protected override _versioned(): boolean {
				return false;
			}
		})(db, RowSchema);

		expect(await plain.put(row("a", "second", 2))).toBe(true);
		expect(await plain.put(row("a", "first", 1))).toBe(true);
		expect((await plain.read("a")).name).toBe("first");
	});
});

/**
 * What Dexie can actually hold, measured rather than read off a doc page.
 *
 * calque's dexie backend declared `UniqueCompoundIndex: false` and
 * docs/conformance.md item 3 was built on it: a unique index over more than one
 * property was said to be inexpressible, so the generator emitted `[a+b]`
 * without the `&` and reported the constraint as one the store could not keep.
 *
 * Dexie 4's parser decides `unique` from `/&/` and `compound` from whether the
 * key path is an array, independently (dist/dexie.js:4056-4060), nothing in
 * _parseStoresSpec forbids the combination (4079-4090), and addIndex passes
 * `unique` straight to createIndex whatever the key path is (3983-3987). So the
 * claim looked wrong. This is the check.
 */
describe("what Dexie can hold", () => {
	function open(stores: string): Dexie {
		const indexedDB = new IDBFactory();
		const db = new Dexie("caps", { indexedDB });
		db.version(1).stores({ rows: stores });
		return db;
	}

	it("enforces a unique index over two properties", async () => {
		const db = open("id,&[a+b]");
		const rows = db.table("rows");

		await rows.add({ id: 1, a: "x", b: "y" });
		await expect(rows.add({ id: 2, a: "x", b: "y" })).rejects.toThrow(
			/ConstraintError|constraint/i,
		);

		// A different pair is not a collision, so the index is on the pair and
		// not on either member.
		await rows.add({ id: 3, a: "x", b: "z" });
		expect(await rows.count()).toBe(2);
	});

	// The form the generator used to emit. It is a *different index name* --
	// Dexie keeps the brackets in the name -- and it is not unique.
	it("does not enforce anything for a bracketed index without &", async () => {
		const db = open("id,[a+b]");
		const rows = db.table("rows");

		await rows.add({ id: 1, a: "x", b: "y" });
		await rows.add({ id: 2, a: "x", b: "y" });
		expect(await rows.count()).toBe(2);
	});

	/**
	 * The bug this file was extended for. A one-member index in compound syntax
	 * registers under the name "[a]", and `where({a})` takes the single-key
	 * branch (dist/dexie.js:1493-1495), which looks an index up by the name "a".
	 * So the declared index can never be queried the way the generated code
	 * queries it.
	 */
	it("cannot query a one-member bracketed index by its member", async () => {
		const db = open("id,[a]");
		const rows = db.table("rows");
		await rows.add({ id: 1, a: "x" });

		await expect(rows.where({ a: "x" }).first()).rejects.toThrow(
			/SchemaError|not indexed/i,
		);
	});

	it("queries it when the same index is declared as a plain unique one", async () => {
		const db = open("id,&a");
		const rows = db.table("rows");
		await rows.add({ id: 1, a: "x" });

		expect(await rows.where({ a: "x" }).first()).toMatchObject({ id: 1 });
		await expect(rows.add({ id: 2, a: "x" })).rejects.toThrow(
			/ConstraintError|constraint/i,
		);
	});

	// Two members query fine either way, because the multi-key branch searches
	// schema.indexes by key path rather than by name (1496-1513). That is why
	// only the one-member case was broken.
	it("queries a two-member index whether or not it is unique", async () => {
		for (const stores of ["id,[a+b]", "id,&[a+b]"]) {
			const rows = open(stores).table("rows");
			await rows.add({ id: 1, a: "x", b: "y" });
			expect(await rows.where({ a: "x", b: "y" }).first()).toMatchObject({
				id: 1,
			});
		}
	});
});
