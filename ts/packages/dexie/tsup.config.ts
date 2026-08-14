import { defineConfig } from "tsup";

export default defineConfig({
	entry: { index: "src/index.ts" },
	format: ["esm"],
	dts: true,
	sourcemap: true,
	clean: true,
	// The runtime is not bundled into the adapter: one copy of uuid at run time,
	// and one nominal identity for every shared type.
	external: [
		"@bufbuild/protobuf",
		"@connectrpc/connect",
		"@heojeongbo/calque-runtime",
		"dexie",
	],
});
