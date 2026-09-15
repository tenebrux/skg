; SKG highlight queries
; Maps tree-sitter node types to standard capture names.
; Works with Neovim (nvim-treesitter), Helix, and Zed.

; Comments
(comment) @comment.line

; Block names  (panel_000 { ... } -> "panel_000" is entity.name)
(block name: (identifier) @module)
(block_array name: (identifier) @module)

; Keys  (key: value -> "key" is property)
(pair key: (identifier) @variable.member)
(scalar_array_field key: (identifier) @variable.member)

; Imports
"import" @keyword.import

; Strings
(string) @string
(multiline_string) @string

; Numbers
(float) @number.float
(integer) @number

; Booleans / null
(boolean) @boolean
(null) @constant.builtin

; Punctuation
":" @punctuation.delimiter
"{" @punctuation.bracket
"}" @punctuation.bracket
"[" @punctuation.bracket
"]" @punctuation.bracket
"," @punctuation.delimiter

; Quoted keys retain their structural role.
(block name: (string) @module)
(block_array name: (string) @module)
(pair key: (string) @variable.member)
(scalar_array_field key: (string) @variable.member)

; Explicit overlay operations do not reserve ordinary data keys.
(overlay_operation "@" @keyword)
(overlay_operation "delete" @keyword)
(overlay_operation "replace" @keyword)
(overlay_operation key: (_) @variable.member)
