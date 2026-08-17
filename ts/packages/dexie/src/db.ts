import { clone, type DescMessage, type MessageShape } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { type Key, unsafe } from "@heojeongbo/calque-runtime";
import type { default as Dexie, Table } from "dexie";

type V_<Desc extends DescMessage> = MessageShape<Desc>;

/**
 * A dehydrated value: what is actually stored.
 *
 * The distinction between this and the hydrated message is the one genuinely
 * store-neutral idea the predecessor had. It is just that its implementation was
 * generated per entity rather than derived, which is what put three bugs in
 * twenty-five lines of generated code.
 */
type E_<Desc extends DescMessage> = Omit<V_<Desc>, "$typeName" | "$unknown">;

type T_<Desc extends DescMessage> = Table<E_<Desc>, Key>;
type D_<Desc extends DescMessage> = Dexie &
	Record<V_<Desc>["$typeName"], T_<Desc>>;

export type ValueOf<Desc extends DescMessage> = V_<Desc>;
export type EntityOf<Desc extends DescMessage> = E_<Desc>;
export type DbOf<Desc extends DescMessage> = D_<Desc>;

/**
 * The base every generated table extends.
 *
 * Ported from `@protobuf-orm/runtime@0.0.1` deliberately unchanged at first,
 * bug in `_reconcile` included: the first version had to behave exactly as the
 * generator it replaces, so that swapping them changed nothing and could be
 * reverted. The bugs are fixed one at a time after that, each in a commit whose
 * diff is only the change it claims to be — `_reconcile` in v0.2.0, which is
 * why moving to that version is a minor bump and not a patch.
 */
export class TableBase<Desc extends DescMessage = DescMessage> {
	protected readonly _db: D_<Desc>;
	readonly _schema: Desc;

	constructor(db: D_<Desc>, schema: Desc) {
		this._db = db;
		this._schema = schema;
	}

	get _typeName(): V_<Desc>["$typeName"] {
		return this._schema.typeName;
	}

	get _table(): T_<Desc> {
		return this._db[this._typeName] as T_<Desc>;
	}

	protected _err(msg: string, code: Code): ConnectError {
		return new ConnectError(`${this._typeName}: ${msg}`, code);
	}

	// Generated subclasses override these. They are stubs rather than abstract
	// so that TableBase can be instantiated generically, which is what the
	// consuming cache does when it builds one table per entity from a map.
	protected _dehydrate(_v: V_<Desc>): [Key, unknown] {
		throw new Error("virtual function not implemented");
	}

	protected _hydrate(_v: unknown): V_<Desc> {
		throw new Error("virtual function not implemented");
	}

	protected _versioned(): boolean {
		return false;
	}

	/**
	 * Compare two values by their version, not by their contents. It assumes
	 * the two name the same row.
	 */
	_compare(_a: V_<Desc> | E_<Desc>, _b: V_<Desc> | E_<Desc>): number {
		return -1;
	}

	/**
	 * Copy a value before dehydrating it, so dehydration cannot mutate the
	 * caller's message — `_dehydrate` writes through `w`, which is the same
	 * object.
	 *
	 * It is a method rather than a call so a test can supply a copier for a
	 * descriptor it made up. The behaviour is unchanged: the real path still
	 * goes through `clone`.
	 */
	protected _clone(v: V_<Desc>): V_<Desc> {
		return clone(this._schema, v);
	}

	private _makeDehydrated(v: V_<Desc>): [Key, unknown] {
		const copy = this._clone(v);
		const res = this._dehydrate(copy);
		unsafe.rm(res[1] as Record<string, unknown>, "$typeName");
		unsafe.rm(res[1] as Record<string, unknown>, "$unknown");
		return res;
	}

	_query(q: (t: T_<Desc>) => Promise<E_<Desc> | undefined>): Promise<V_<Desc>> {
		return q(this._table).then((v) => {
			if (v === undefined) throw this._err("not found", Code.NotFound);
			return this._hydrate(v);
		});
	}

	async _insert(v: V_<Desc>): Promise<Key> {
		const [k, data] = this._makeDehydrated(v);
		return this._table.add(data as E_<Desc>, k);
	}

	/**
	 * Write a value, keeping whichever of the two is newer, and report which
	 * one that was.
	 *
	 * `true` means this value is now what is stored; `false` means the stored
	 * value was newer or the same and nothing was written. A caller that skips
	 * work on `false` can rely on that.
	 *
	 * It did not used to. Until v0.2.0 this method resolved `true` no matter
	 * what: the `false` returned inside the transaction was the callback's
	 * result rather than the method's, and it was discarded. Reproducing that
	 * was deliberate while the point was to be a drop-in — see
	 * docs/conformance.md item 5 — but a caller writing
	 *
	 *     const ok = await table._reconcile(v);
	 *     if (!ok) continue;
	 *
	 * had a branch that never ran, and now does. That is the whole reason this
	 * is a minor bump rather than a patch.
	 *
	 * The comparison and the write are in one transaction, so the answer is not
	 * stale by the time it is returned.
	 */
	async _reconcile(v: V_<Desc>): Promise<boolean> {
		const [k, data] = this._makeDehydrated(v);

		if (!this._versioned()) {
			await this._table.put(data as E_<Desc>, k);
			return true;
		}

		return this._db.transaction("rw", this._table, async () => {
			const u = await this._table.get(k);
			if (u === undefined) {
				await this._table.put(data as E_<Desc>, k);
				return true;
			}
			if (this._compare(u, v) >= 0) {
				return false;
			}
			await this._table.put(data as E_<Desc>, k);
			return true;
		});
	}
}
