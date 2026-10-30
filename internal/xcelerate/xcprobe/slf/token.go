// Package slf is a Go port of XCLogParser's Swift SLF parser for Xcode's
// gzip-compressed `.xcactivitylog` files.
//
// The format is reverse-engineered from Apple's private
// IDEFoundation.framework class layouts. XCLogParser (Apache-2) is the
// upstream reference: https://github.com/MobileNativeFoundation/XCLogParser.
package slf

import "fmt"

// TokenType is one of the eight single-char SLF delimiters. Values match the
// delimiter byte so a lexer can route on the raw character directly.
type TokenType byte

const (
	TokenInt          TokenType = '#'
	TokenClassName    TokenType = '%'
	TokenClassNameRef TokenType = '@'
	TokenString       TokenType = '"'
	TokenDouble       TokenType = '^'
	TokenNull         TokenType = '-'
	TokenList         TokenType = '('
	TokenJSON         TokenType = '*'
)

// Token carries one decoded value. Exactly one of IntValue / DoubleValue /
// StringValue / ListCount is populated per Type; Null carries nothing.
// ClassName (first occurrence) and ClassNameRef (back-reference) both carry
// the resolved class name in StringValue.
type Token struct {
	Type        TokenType
	IntValue    uint64
	DoubleValue float64
	StringValue string
	ListCount   int
}

func (t Token) String() string {
	switch t.Type {
	case TokenInt:
		return fmt.Sprintf("int(%d)", t.IntValue)
	case TokenClassName:
		return fmt.Sprintf("className(%q)", t.StringValue)
	case TokenClassNameRef:
		return fmt.Sprintf("classNameRef(%q)", t.StringValue)
	case TokenString:
		return fmt.Sprintf("string(%q)", t.StringValue)
	case TokenDouble:
		return fmt.Sprintf("double(%g)", t.DoubleValue)
	case TokenNull:
		return "null"
	case TokenList:
		return fmt.Sprintf("list(%d)", t.ListCount)
	case TokenJSON:
		return fmt.Sprintf("json(%q)", t.StringValue)
	default:
		return fmt.Sprintf("unknown(%c)", byte(t.Type))
	}
}
