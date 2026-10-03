# go-blocknote2md

Convert [BlockNote](https://www.blocknotejs.org) documents to Markdown in Go,
producing **exactly** what BlockNote's own `blocksToMarkdownLossy` produces.

BlockNote stores a document as a JSON array of blocks. Its Markdown export isn't
a tree walk: it renders the blocks to HTML and runs that through the unified
ecosystem (`rehype-remark`, `remark-gfm`, `remark-stringify`). This module ports
that pipeline to Go, stage by stage, so a server can export BlockNote content
without a JavaScript runtime — and so the Markdown matches what users get from
the editor.

It is a library with no dependencies beyond the Go standard library. It is used
by [Paca](https://github.com/Paca-AI/paca) for its project export.

## Usage

```go
import "github.com/Paca-AI/go-blocknote2md"

md, err := blocknote2md.Convert(doc) // doc is the JSON array of blocks
```

```go
// Annotation cards export as a link to the app; give it the app's origin
// (BlockNote itself uses window.location.origin).
md, err := blocknote2md.Convert(doc, blocknote2md.WithOrigin("https://paca.example.com"))
```

`Convert` returns `""` for an empty document (`""`, `null`, `[]`) and an error
only when the input isn't a JSON array. `ConvertBlocks` takes a document you have
already decoded with `encoding/json`.

## What it does (on purpose)

Because the goal is BlockNote's exact output, BlockNote's quirks are reproduced:

- ordinary text is **never escaped** (`a*b_c [x]` stays as is);
- bullets are `*`, and a list whose items hold paragraphs is loose (blank lines
  between items);
- a table gets an **empty header row** unless it has header rows/columns;
- an image's caption becomes its own paragraph, and its alt text is the file name;
- a mention exports as its plain name (no `@`);
- characters next to emphasis markers may be written as character references
  (`&#x20;`), and a link inside inline code keeps only the code;
- a heading containing a line break becomes a setext heading.

All of BlockNote's default blocks are supported, plus the three extensions Paca
registers on top (`mermaid`, `annotationCard`, and the `teamMention` /
`taskReference` / `docReference` inline content). A block type outside those is
rejected by BlockNote itself; here it falls back to its inline text so a
conversion never silently drops content.

## How it is verified

`testdata/blocknote_fixtures.json` holds ~300 block documents, covering every
block, style, nesting and whitespace edge case found so far. `js/` runs them
through the **real** BlockNote (with Paca's three extensions registered) and
writes `testdata/blocknote_golden.json`; `go test` asserts that `Convert` output
is byte-identical to it. CI also re-runs the JavaScript side and fails if the
golden file has drifted, so the Go tests can't pass against stale expectations.

In addition, the port was fuzzed against BlockNote with ~24,000 randomly
generated documents (nested lists, odd whitespace, emoji, punctuation, table
spans, every media variant) with no differences.

```sh
go test ./...                 # Go converter vs the golden file

cd js && npm ci
npm run golden                # real BlockNote vs the golden file (fails on drift)
npm run golden:update         # rewrite the golden file, then fix the Go side
```

### Pinned versions

BlockNote's Markdown comes from several transitive packages, and their output
differs between versions (for example `mdast-util-to-markdown` 2.1.3 encodes
characters next to emphasis differently from 2.1.2). `js/package.json` therefore
pins `@blocknote/core` and, through `overrides`, the three packages that otherwise
float, to the versions [Paca](https://github.com/Paca-AI/paca) locks — so this
library matches what Paca's editor produces. When Paca upgrades BlockNote or any
of those, bump them here together, run `npm run golden:update`, and adjust the Go
code until `go test` passes again.

## Layout

| File | Stage |
| --- | --- |
| `blocks.go` | blocks → BlockNote's external HTML (list grouping, per-block markup, inline marks, ProseMirror's table serializer) |
| `mdast.go` | BlockNote's rehype plugins and `hast-util-to-mdast` |
| `hast.go` | the HTML tree, `hast-util-minify-whitespace`, UTF-16 string helpers (JavaScript indexes strings by UTF-16 unit, which the emphasis encoding depends on) |
| `stringify.go` | `mdast-util-to-markdown` with the `remark-gfm` extensions, `safe()` and `markdown-table` |

## License

Apache License 2.0 — see [LICENSE](LICENSE).
