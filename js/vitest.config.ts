import { defineConfig } from "vitest/config";

export default defineConfig({
	esbuild: { jsx: "automatic" },
	test: {
		environment: "jsdom",
		// BlockNote reads window.location.origin for annotation cards; the Go
		// tests use the same value (goldenOrigin).
		environmentOptions: { jsdom: { url: "http://localhost:3000/" } },
		include: ["*.test.ts"],
	},
});
