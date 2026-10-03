import fs from "node:fs";
import path from "node:path";
import { BlockNoteEditor } from "@blocknote/core";
import { describe, expect, it } from "vitest";
import { schema } from "./schema";

// Runs every fixture through the real BlockNote and compares with
// ../testdata/blocknote_golden.json, which the Go tests assert against.
//
//	npm run golden          fails if BlockNote's output differs from the golden file
//	npm run golden:update   rewrites the golden file (after a BlockNote upgrade or
//	                        when fixtures were added); then make the Go code match
//
// A fixture BlockNote itself rejects is recorded as null and skipped by Go.

const testdata = path.resolve(import.meta.dirname, "../testdata");
const goldenPath = path.join(testdata, "blocknote_golden.json");
const fixtures = JSON.parse(
	fs.readFileSync(path.join(testdata, "blocknote_fixtures.json"), "utf8"),
) as Record<string, unknown[]>;

async function render(blocks: unknown[]): Promise<string | null> {
	const editor = BlockNoteEditor.create({ schema });
	try {
		// biome-ignore lint/suspicious/noExplicitAny: fixtures are raw block JSON
		return await editor.blocksToMarkdownLossy(blocks as any);
	} catch {
		return null;
	}
}

describe("BlockNote markdown golden output", () => {
	it("matches testdata/blocknote_golden.json", async () => {
		const actual: Record<string, string | null> = {};
		for (const name of Object.keys(fixtures).sort()) {
			actual[name] = await render(fixtures[name]);
		}
		// U+2028, U+2029 and U+FEFF are valid raw in JSON but editors and tools
		// strip or normalise them, so write them as escapes.
		const json = JSON.stringify(actual, null, 1)
			.replaceAll("\u2028", "\\u2028")
			.replaceAll("\u2029", "\\u2029")
			.replaceAll("\uFEFF", "\\uFEFF");
		await expect(`${json}\n`).toMatchFileSnapshot(goldenPath);
	});
});
