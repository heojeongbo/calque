import type {
	DescMethod,
	DescService,
	Message,
	MessageInitShape,
	MessageShape,
} from "@bufbuild/protobuf";

export type InputOf<
	Desc extends DescService,
	Rpc extends keyof Desc["method"],
> = MessageShape<Desc["method"][Rpc]["input"]>;

export type OutputOf<
	Desc extends DescService,
	Rpc extends keyof Desc["method"],
> = MessageShape<Desc["method"][Rpc]["output"]>;

export type RefOf<
	Desc extends DescService,
	T = MessageInitShape<Desc["method"]["get"]["input"]>,
> = T extends { ref?: unknown } ? T["ref"] : undefined;

/**
 * What a cache needs to know about one service.
 *
 * `pick` is an entity's canonical ref, `refs` is every ref it can be found by,
 * and `rpc[m].extract` pulls entities out of an arbitrary response. Together
 * they are enough to harvest entities from any reply and to know every key each
 * one is cached under, so a write can invalidate all of them.
 *
 * This is the one type in the generated corpus that `tsc` genuinely checks: a
 * generated `queries` table ends each entry with `satisfies QueryDescOf<typeof
 * XService>`, so `pick`, `refs` and every `extract` are verified against the
 * real service descriptor.
 */
export type QueryDescOf<Desc extends DescService = DescService> = {
	pick: (v: OutputOf<Desc, "get">) => RefOf<Desc>;
	refs: (v: OutputOf<Desc, "get">) => RefOf<Desc>[];
	rpc: {
		[K in keyof Desc["method"]]: {
			desc: Desc["method"][K];
			extract: (
				v: OutputOf<Desc, K>,
			) => OutputOf<Desc, "get"> | OutputOf<Desc, "get">[] | undefined;
		};
	};
};

/** QueryDescOf with the per-service typing erased, for iterating generically. */
export type QueryDesc = {
	pick: (v: Message) => { [x: string]: unknown } | undefined;
	refs: (v: Message) => { [x: string]: unknown }[];
	rpc: Record<
		string,
		| undefined
		| {
				desc: DescMethod;
				extract: (v: Message) => Message | Message[] | undefined;
		  }
	>;
};
