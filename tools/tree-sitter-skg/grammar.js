/// Tree-sitter grammar for the SKG (Static Key Group) config format.
///
/// Build:      npm install && npx tree-sitter generate
/// Test:       npx tree-sitter test
/// Install:    npx tree-sitter build --wasm   (for web/Zed)
///             npx tree-sitter build           (native, for Neovim/Helix)
///
/// Neovim:  add to nvim-treesitter as a custom parser.
/// Helix:   add to languages.toml under [[language]] with tree-sitter source.
/// Zed:     register in extensions/zed-skg (wasm build).

module.exports = grammar({
  name: 'skg',

  extras: $ => [
    /\s/,
    $.comment,
  ],

  word: $ => $.identifier,

  rules: {
    // A document is zero or more top-level statements.
    document: $ => repeat($._statement),

    _statement: $ => choice(
      $.import,
      $.block_array,
      $.scalar_array_field,
      $.block,
      $.pair,
    ),

    // import "./foo.skg"   |   import [ "./a.skg", "./b.skg" ]
    import: $ => seq(
      'import',
      choice(
        $.string,
        seq(
          '[',
          optional(seq(
            $.string,
            repeat(seq(',', $.string)),
            optional(','),
          )),
          ']',
        ),
      ),
    ),

    // block:  name { ... }
    block: $ => seq(
      field('name', $._key),
      '{',
      repeat($._statement),
      '}',
    ),

    // block_array:  name [ { ... } { ... } ]
    // Distinguished from scalar_array_field by the first token inside [ ].
    block_array: $ => prec(2, seq(
      field('name', $._key),
      '[',
      repeat($.block_array_item),
      ']',
    )),

    block_array_item: $ => seq(
      '{',
      repeat($._statement),
      '}',
      optional(','),
    ),

    // Colonless scalar array shorthand: `tags ["a", "b"]`
    // Semantically equivalent to `tags: ["a", "b"]` per spec.
    scalar_array_field: $ => prec(1, seq(
      field('key', $._key),
      field('value', $.array),
    )),

    // pair:  key: value
    pair: $ => seq(
      field('key', $._key),
      ':',
      field('value', $._value),
    ),

    _key: $ => choice($.identifier, $.string),

    _value: $ => choice(
      $.multiline_string,
      $.string,
      $.float,
      $.integer,
      $.boolean,
      $.null,
      $.array,
    ),

    // array:  [ value, value, ... ]
    array: $ => seq(
      '[',
      optional(
        seq(
          $._value,
          repeat(seq(',', $._value)),
          optional(','),
        ),
      ),
      ']',
    ),

    // Triple-quoted multiline string. No escape processing per spec -
    // content is taken literally between the delimiters. Higher precedence
    // than single-quoted string so the lexer prefers """...""".
    multiline_string: $ => token(prec(2, seq(
      '"""',
      repeat(choice(
        /[^"]/,
        /"[^"]/,
        /""[^"]/,
      )),
      '"""',
    ))),

    // single-line string with escapes limited to spec: \" \\ \n \t
    string: $ => token(prec(1, seq(
      '"',
      repeat(choice(
        /[^"\\\n]/,
        /\\["\\nt]/,
      )),
      '"',
    ))),

    // float must be tried before integer to avoid partial matches.
    float: $ => token(prec(1, /-?\d+\.\d+/)),

    integer: $ => /-?\d+/,

    boolean: $ => choice('true', 'false'),

    null: $ => 'null',

    identifier: $ => /[a-zA-Z_][a-zA-Z0-9_]*/,

    // # line comment
    comment: $ => /#.*/,
  },
});
