package main

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Indentation alone is ordinary note text. Keep its literal content and line
// breaks, including for older notes, while explicit fenced blocks remain code.
// Transform parsed blocks so list/quote nesting and fenced content stay intact.
func renderIndentedText(document ast.Node, source []byte) {
	var blocks []*ast.CodeBlock
	ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := node.(*ast.CodeBlock); entering && ok {
			blocks = append(blocks, block)
		}
		return ast.WalkContinue, nil
	})
	for _, block := range blocks {
		paragraph := ast.NewParagraph()
		for i := 0; i < block.Lines().Len(); i++ {
			segment := block.Lines().At(i)
			line := strings.TrimSuffix(strings.TrimSuffix(string(segment.Value(source)), "\n"), "\r")
			leading := len(line) - len(strings.TrimLeft(line, " \t"))
			indent := strings.ReplaceAll(strings.ReplaceAll(line[:leading], " ", "\u00a0"), "\t", strings.Repeat("\u00a0", 4))
			literal := ast.NewString([]byte(strings.Repeat("\u00a0", 4) + indent + line[leading:]))
			literal.SetRaw(true)
			paragraph.AppendChild(paragraph, literal)
			if i+1 < block.Lines().Len() {
				br := ast.NewTextSegment(text.NewSegment(0, 0))
				br.SetHardLineBreak(true)
				paragraph.AppendChild(paragraph, br)
			}
		}
		block.Parent().ReplaceChild(block.Parent(), block, paragraph)
	}
}
