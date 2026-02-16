package tui

import "github.com/charmbracelet/glamour/ansi"

func localclawMarkdownStyles() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#9d7cd8"), Bold: boolPtr(true)},
		},
		Text: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
		Strong: ansi.StylePrimitive{
			Color: strPtr("#f5a742"),
			Bold:  boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Color:  strPtr("#e5c07b"),
			Italic: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color: strPtr("#808080"),
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
			},
			LevelIndent: 2,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
			Color:       strPtr("#fab283"),
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
			Color:       strPtr("#56b6c2"),
		},
		Link: ansi.StylePrimitive{
			Color:     strPtr("#fab283"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr("#56b6c2"),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#7fd88f")},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color:           strPtr("#eeeeee"),
					BackgroundColor: strPtr("#1e1e1e"),
				},
			},
			Theme: "dracula",
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: strPtr("#e5c07b")},
			Indent:         uintPtr(1),
			IndentToken:    strPtr("┃ "),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: strPtr("#eeeeee")},
			},
			CenterSeparator: strPtr("│"),
			ColumnSeparator: strPtr("│"),
			RowSeparator:    strPtr("─"),
		},
	}
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func uintPtr(v uint) *uint {
	return &v
}
