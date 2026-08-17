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
