/// Token types produced by the SKG lexer.
/// Tag identifying the kind of token.
pub const Tag = enum {
    int, // 42, -3
    float, // 0.92, -13.0  (decimal point required)
    bool_true, // true
    bool_false, // false
    null_lit, // null
    string, // "..." - raw text including surrounding quotes, escapes unprocessed
    ident, // bare word: key name or block name
    colon, // :
    lbrace, // {
    rbrace, // }
    lbracket, // [
    rbracket, // ]
    comma, // ,
    at, // @
    comment, // # ...
    eof,
};

/// A single lexed token. `text` is a slice into the original source.
/// For strings, `text` includes the surrounding double quotes.
pub const Token = struct {
    tag: Tag,
    text: []const u8,
    line: u32,
    col: u32,
};
