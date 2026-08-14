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
	 * KNOWN BUG, pinned deliberately: `_reconcile` always resolves true, even
	 * when the stored value was newer and nothing was written.
	 *
	 * The `false` returned inside the Dexie transaction is the callback's
	 * result, not the method's, and it is discarded. The consuming application
	 * reads this value and skips work when it is false — a branch that
	 * therefore never runs.
	 *
	 * This test asserts the bug so that fixing it is a deliberate act with its
	 * own diff, and so the fix cannot arrive as a side effect of something
	 * else. See docs/conformance.md item 5. When it is fixed, this expectation
	 * flips to `false` in the same commit.
	 */
	it("reports success even when it did not write (conformance item 5)", async () => {
		await rows.put(row("a", "second", 2));

		const applied = await rows.put(row("a", "first", 1));
		expect(applied).toBe(true);
		expect((await rows.read("a")).name).toBe("second");
	});
});
