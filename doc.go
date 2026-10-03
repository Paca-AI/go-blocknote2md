// Package blocknote2md converts BlockNote documents to Markdown, producing
// exactly what BlockNote's own blocksToMarkdownLossy produces.
//
// BlockNote (https://www.blocknotejs.org) stores a document as a JSON array of
// blocks. Its Markdown export is not a tree walk: it renders the blocks to
// HTML and runs that through the unified ecosystem (rehype-remark, remark-gfm,
// remark-stringify). This package has no JavaScript runtime to lean on, so it
// ports that pipeline stage by stage, with each library's exact rules:
//
//	blocks -> external HTML          blocks.go    (BlockNote's external exporter)
//	       -> hast -> mdast          mdast.go     (hast-util-to-mdast, BlockNote's
//	                                              rehype plugins, hast.go's
//	                                              whitespace minifier)
//	       -> markdown               stringify.go (mdast-util-to-markdown + gfm)
//
// Matching the library byte for byte is the point, so its quirks are kept on
// purpose: ordinary text is never escaped, bullets are "*", lists whose items
// hold paragraphs are loose, a table without header rows/columns gets an empty
// header row, an image caption becomes its own paragraph, a mention exports as
// its plain name, and characters next to emphasis markers may be written as
// character references.
//
// # Supported content
//
// All of BlockNote's default blocks (paragraph, heading, quote, code block,
// divider, bullet/numbered/check/toggle list items, table, image, video, audio
// and file) with their inline content (styles, links, hard breaks), plus three
// extensions Paca registers:
//
//   - the "mermaid" block, exported as a fenced code block;
//   - the "annotationCard" block, exported as a link built on [WithOrigin];
//   - the "teamMention", "taskReference" and "docReference" inline content,
//     exported as their name or title.
//
// A block type outside these is rejected by BlockNote itself; here it falls
// back to its inline text so a conversion never silently drops content.
//
// # Verification
//
// testdata/blocknote_fixtures.json is run through the real library by the
// checker in js/, which writes testdata/blocknote_golden.json; the Go tests
// assert byte equality with that file. See the README for regenerating it.
package blocknote2md
