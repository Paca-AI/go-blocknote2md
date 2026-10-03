package blocknote2md_test

import (
	"fmt"

	"github.com/Paca-AI/go-blocknote2md"
)

func ExampleConvert() {
	doc := `[
	  {"type":"heading","props":{"level":1},"content":[{"type":"text","text":"Release notes","styles":{}}]},
	  {"type":"checkListItem","props":{"checked":true},"content":[{"type":"text","text":"Ship it","styles":{}}]}
	]`
	md, err := blocknote2md.Convert([]byte(doc))
	if err != nil {
		panic(err)
	}
	fmt.Print(md)
	// Output:
	// # Release notes
	//
	// * [x] Ship it
}
