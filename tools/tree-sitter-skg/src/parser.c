#include <tree_sitter/parser.h>

#if defined(__GNUC__) || defined(__clang__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wmissing-field-initializers"
#endif

#define LANGUAGE_VERSION 14
#define STATE_COUNT 68
#define LARGE_STATE_COUNT 2
#define SYMBOL_COUNT 38
#define ALIAS_COUNT 0
#define TOKEN_COUNT 20
#define EXTERNAL_TOKEN_COUNT 0
#define FIELD_COUNT 3
#define MAX_ALIAS_SEQUENCE_LENGTH 6
#define PRODUCTION_ID_COUNT 6

enum {
  sym_identifier = 1,
  anon_sym_AT = 2,
  anon_sym_delete = 3,
  anon_sym_replace = 4,
  anon_sym_import = 5,
  anon_sym_LBRACK = 6,
  anon_sym_COMMA = 7,
  anon_sym_RBRACK = 8,
  anon_sym_LBRACE = 9,
  anon_sym_RBRACE = 10,
  anon_sym_COLON = 11,
  sym_multiline_string = 12,
  sym_string = 13,
  sym_float = 14,
  sym_integer = 15,
  anon_sym_true = 16,
  anon_sym_false = 17,
  sym_null = 18,
  sym_comment = 19,
  sym_document = 20,
  sym__statement = 21,
  sym_overlay_operation = 22,
  sym_import = 23,
  sym_block = 24,
  sym_block_array = 25,
  sym_block_array_item = 26,
  sym_scalar_array_field = 27,
  sym_pair = 28,
  sym__key = 29,
  sym__value = 30,
  sym_object = 31,
  sym_array = 32,
  sym_boolean = 33,
  aux_sym_document_repeat1 = 34,
  aux_sym_import_repeat1 = 35,
  aux_sym_block_array_repeat1 = 36,
  aux_sym_array_repeat1 = 37,
};

static const char * const ts_symbol_names[] = {
  [ts_builtin_sym_end] = "end",
  [sym_identifier] = "identifier",
  [anon_sym_AT] = "@",
  [anon_sym_delete] = "delete",
  [anon_sym_replace] = "replace",
  [anon_sym_import] = "import",
  [anon_sym_LBRACK] = "[",
  [anon_sym_COMMA] = ",",
  [anon_sym_RBRACK] = "]",
  [anon_sym_LBRACE] = "{",
  [anon_sym_RBRACE] = "}",
  [anon_sym_COLON] = ":",
  [sym_multiline_string] = "multiline_string",
  [sym_string] = "string",
  [sym_float] = "float",
  [sym_integer] = "integer",
  [anon_sym_true] = "true",
  [anon_sym_false] = "false",
  [sym_null] = "null",
  [sym_comment] = "comment",
  [sym_document] = "document",
  [sym__statement] = "_statement",
  [sym_overlay_operation] = "overlay_operation",
  [sym_import] = "import",
  [sym_block] = "block",
  [sym_block_array] = "block_array",
  [sym_block_array_item] = "block_array_item",
  [sym_scalar_array_field] = "scalar_array_field",
  [sym_pair] = "pair",
  [sym__key] = "_key",
  [sym__value] = "_value",
  [sym_object] = "object",
  [sym_array] = "array",
  [sym_boolean] = "boolean",
  [aux_sym_document_repeat1] = "document_repeat1",
  [aux_sym_import_repeat1] = "import_repeat1",
  [aux_sym_block_array_repeat1] = "block_array_repeat1",
  [aux_sym_array_repeat1] = "array_repeat1",
};

static const TSSymbol ts_symbol_map[] = {
  [ts_builtin_sym_end] = ts_builtin_sym_end,
  [sym_identifier] = sym_identifier,
  [anon_sym_AT] = anon_sym_AT,
  [anon_sym_delete] = anon_sym_delete,
  [anon_sym_replace] = anon_sym_replace,
  [anon_sym_import] = anon_sym_import,
  [anon_sym_LBRACK] = anon_sym_LBRACK,
  [anon_sym_COMMA] = anon_sym_COMMA,
  [anon_sym_RBRACK] = anon_sym_RBRACK,
  [anon_sym_LBRACE] = anon_sym_LBRACE,
  [anon_sym_RBRACE] = anon_sym_RBRACE,
  [anon_sym_COLON] = anon_sym_COLON,
  [sym_multiline_string] = sym_multiline_string,
  [sym_string] = sym_string,
  [sym_float] = sym_float,
  [sym_integer] = sym_integer,
  [anon_sym_true] = anon_sym_true,
  [anon_sym_false] = anon_sym_false,
  [sym_null] = sym_null,
  [sym_comment] = sym_comment,
  [sym_document] = sym_document,
  [sym__statement] = sym__statement,
  [sym_overlay_operation] = sym_overlay_operation,
  [sym_import] = sym_import,
  [sym_block] = sym_block,
  [sym_block_array] = sym_block_array,
  [sym_block_array_item] = sym_block_array_item,
  [sym_scalar_array_field] = sym_scalar_array_field,
  [sym_pair] = sym_pair,
  [sym__key] = sym__key,
  [sym__value] = sym__value,
  [sym_object] = sym_object,
  [sym_array] = sym_array,
  [sym_boolean] = sym_boolean,
  [aux_sym_document_repeat1] = aux_sym_document_repeat1,
  [aux_sym_import_repeat1] = aux_sym_import_repeat1,
  [aux_sym_block_array_repeat1] = aux_sym_block_array_repeat1,
  [aux_sym_array_repeat1] = aux_sym_array_repeat1,
};

static const TSSymbolMetadata ts_symbol_metadata[] = {
  [ts_builtin_sym_end] = {
    .visible = false,
    .named = true,
  },
  [sym_identifier] = {
    .visible = true,
    .named = true,
  },
  [anon_sym_AT] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_delete] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_replace] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_import] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_LBRACK] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_COMMA] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_RBRACK] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_LBRACE] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_RBRACE] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_COLON] = {
    .visible = true,
    .named = false,
  },
  [sym_multiline_string] = {
    .visible = true,
    .named = true,
  },
  [sym_string] = {
    .visible = true,
    .named = true,
  },
  [sym_float] = {
    .visible = true,
    .named = true,
  },
  [sym_integer] = {
    .visible = true,
    .named = true,
  },
  [anon_sym_true] = {
    .visible = true,
    .named = false,
  },
  [anon_sym_false] = {
    .visible = true,
    .named = false,
  },
  [sym_null] = {
    .visible = true,
    .named = true,
  },
  [sym_comment] = {
    .visible = true,
    .named = true,
  },
  [sym_document] = {
    .visible = true,
    .named = true,
  },
  [sym__statement] = {
    .visible = false,
    .named = true,
  },
  [sym_overlay_operation] = {
    .visible = true,
    .named = true,
  },
  [sym_import] = {
    .visible = true,
    .named = true,
  },
  [sym_block] = {
    .visible = true,
    .named = true,
  },
  [sym_block_array] = {
    .visible = true,
    .named = true,
  },
  [sym_block_array_item] = {
    .visible = true,
    .named = true,
  },
  [sym_scalar_array_field] = {
    .visible = true,
    .named = true,
  },
  [sym_pair] = {
    .visible = true,
    .named = true,
  },
  [sym__key] = {
    .visible = false,
    .named = true,
  },
  [sym__value] = {
    .visible = false,
    .named = true,
  },
  [sym_object] = {
    .visible = true,
    .named = true,
  },
  [sym_array] = {
    .visible = true,
    .named = true,
  },
  [sym_boolean] = {
    .visible = true,
    .named = true,
  },
  [aux_sym_document_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_import_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_block_array_repeat1] = {
    .visible = false,
    .named = false,
  },
  [aux_sym_array_repeat1] = {
    .visible = false,
    .named = false,
  },
};

enum {
  field_key = 1,
  field_name = 2,
  field_value = 3,
};

static const char * const ts_field_names[] = {
  [0] = NULL,
  [field_key] = "key",
  [field_name] = "name",
  [field_value] = "value",
};

static const TSFieldMapSlice ts_field_map_slices[PRODUCTION_ID_COUNT] = {
  [1] = {.index = 0, .length = 2},
  [2] = {.index = 2, .length = 1},
  [3] = {.index = 3, .length = 1},
  [4] = {.index = 4, .length = 2},
  [5] = {.index = 6, .length = 2},
};

static const TSFieldMapEntry ts_field_map_entries[] = {
  [0] =
    {field_key, 0},
    {field_value, 1},
  [2] =
    {field_key, 2},
  [3] =
    {field_name, 0},
  [4] =
    {field_key, 0},
    {field_value, 2},
  [6] =
    {field_key, 2},
    {field_value, 3},
};

static const TSSymbol ts_alias_sequences[PRODUCTION_ID_COUNT][MAX_ALIAS_SEQUENCE_LENGTH] = {
  [0] = {0},
};

static const uint16_t ts_non_terminal_alias_map[] = {
  0,
};

static const TSStateId ts_primary_state_ids[STATE_COUNT] = {
  [0] = 0,
  [1] = 1,
  [2] = 2,
  [3] = 3,
  [4] = 4,
  [5] = 5,
  [6] = 6,
  [7] = 7,
  [8] = 8,
  [9] = 9,
  [10] = 10,
  [11] = 11,
  [12] = 12,
  [13] = 13,
  [14] = 14,
  [15] = 15,
  [16] = 16,
  [17] = 17,
  [18] = 18,
  [19] = 19,
  [20] = 20,
  [21] = 21,
  [22] = 22,
  [23] = 23,
  [24] = 24,
  [25] = 25,
  [26] = 26,
  [27] = 27,
  [28] = 28,
  [29] = 29,
  [30] = 30,
  [31] = 31,
  [32] = 32,
  [33] = 33,
  [34] = 34,
  [35] = 35,
  [36] = 36,
  [37] = 37,
  [38] = 38,
  [39] = 39,
  [40] = 40,
  [41] = 41,
  [42] = 42,
  [43] = 43,
  [44] = 44,
  [45] = 45,
  [46] = 46,
  [47] = 47,
  [48] = 48,
  [49] = 49,
  [50] = 50,
  [51] = 51,
  [52] = 52,
  [53] = 53,
  [54] = 54,
  [55] = 55,
  [56] = 56,
  [57] = 57,
  [58] = 58,
  [59] = 59,
  [60] = 60,
  [61] = 61,
  [62] = 62,
  [63] = 63,
  [64] = 64,
  [65] = 65,
  [66] = 66,
  [67] = 67,
};

static bool ts_lex(TSLexer *lexer, TSStateId state) {
  START_LEXER();
  eof = lexer->eof(lexer);
  switch (state) {
    case 0:
      if (eof) ADVANCE(10);
      if (lookahead == '"') ADVANCE(1);
      if (lookahead == '#') ADVANCE(24);
      if (lookahead == ',') ADVANCE(13);
      if (lookahead == '-') ADVANCE(7);
      if (lookahead == ':') ADVANCE(17);
      if (lookahead == '@') ADVANCE(11);
      if (lookahead == '[') ADVANCE(12);
      if (lookahead == ']') ADVANCE(14);
      if (lookahead == '{') ADVANCE(15);
      if (lookahead == '}') ADVANCE(16);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') SKIP(0)
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(22);
      if (('A' <= lookahead && lookahead <= 'Z') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z')) ADVANCE(23);
      END_STATE();
    case 1:
      if (lookahead == '"') ADVANCE(20);
      if (lookahead == '\\') ADVANCE(6);
      if (lookahead != 0 &&
          lookahead != '\n') ADVANCE(2);
      END_STATE();
    case 2:
      if (lookahead == '"') ADVANCE(19);
      if (lookahead == '\\') ADVANCE(6);
      if (lookahead != 0 &&
          lookahead != '\n') ADVANCE(2);
      END_STATE();
    case 3:
      if (lookahead == '"') ADVANCE(18);
      if (lookahead != 0) ADVANCE(5);
      END_STATE();
    case 4:
      if (lookahead == '"') ADVANCE(3);
      if (lookahead != 0) ADVANCE(5);
      END_STATE();
    case 5:
      if (lookahead == '"') ADVANCE(4);
      if (lookahead != 0) ADVANCE(5);
      END_STATE();
    case 6:
      if (lookahead == '"' ||
          lookahead == '\\' ||
          lookahead == 'n' ||
          lookahead == 't') ADVANCE(2);
      END_STATE();
    case 7:
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(22);
      END_STATE();
    case 8:
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(21);
      END_STATE();
    case 9:
      if (eof) ADVANCE(10);
      if (lookahead == '"') ADVANCE(2);
      if (lookahead == '#') ADVANCE(24);
      if (lookahead == ',') ADVANCE(13);
      if (lookahead == '@') ADVANCE(11);
      if (lookahead == '[') ADVANCE(12);
      if (lookahead == ']') ADVANCE(14);
      if (lookahead == '}') ADVANCE(16);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') SKIP(9)
      if (('A' <= lookahead && lookahead <= 'Z') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z')) ADVANCE(23);
      END_STATE();
    case 10:
      ACCEPT_TOKEN(ts_builtin_sym_end);
      END_STATE();
    case 11:
      ACCEPT_TOKEN(anon_sym_AT);
      END_STATE();
    case 12:
      ACCEPT_TOKEN(anon_sym_LBRACK);
      END_STATE();
    case 13:
      ACCEPT_TOKEN(anon_sym_COMMA);
      END_STATE();
    case 14:
      ACCEPT_TOKEN(anon_sym_RBRACK);
      END_STATE();
    case 15:
      ACCEPT_TOKEN(anon_sym_LBRACE);
      END_STATE();
    case 16:
      ACCEPT_TOKEN(anon_sym_RBRACE);
      END_STATE();
    case 17:
      ACCEPT_TOKEN(anon_sym_COLON);
      END_STATE();
    case 18:
      ACCEPT_TOKEN(sym_multiline_string);
      END_STATE();
    case 19:
      ACCEPT_TOKEN(sym_string);
      END_STATE();
    case 20:
      ACCEPT_TOKEN(sym_string);
      if (lookahead == '"') ADVANCE(5);
      END_STATE();
    case 21:
      ACCEPT_TOKEN(sym_float);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(21);
      END_STATE();
    case 22:
      ACCEPT_TOKEN(sym_integer);
      if (lookahead == '.') ADVANCE(8);
      if (('0' <= lookahead && lookahead <= '9')) ADVANCE(22);
      END_STATE();
    case 23:
      ACCEPT_TOKEN(sym_identifier);
      if (('0' <= lookahead && lookahead <= '9') ||
          ('A' <= lookahead && lookahead <= 'Z') ||
          lookahead == '_' ||
          ('a' <= lookahead && lookahead <= 'z')) ADVANCE(23);
      END_STATE();
    case 24:
      ACCEPT_TOKEN(sym_comment);
      if (lookahead != 0 &&
          lookahead != '\n') ADVANCE(24);
      END_STATE();
    default:
      return false;
  }
}

static bool ts_lex_keywords(TSLexer *lexer, TSStateId state) {
  START_LEXER();
  eof = lexer->eof(lexer);
  switch (state) {
    case 0:
      if (lookahead == 'd') ADVANCE(1);
      if (lookahead == 'f') ADVANCE(2);
      if (lookahead == 'i') ADVANCE(3);
      if (lookahead == 'n') ADVANCE(4);
      if (lookahead == 'r') ADVANCE(5);
      if (lookahead == 't') ADVANCE(6);
      if (lookahead == '\t' ||
          lookahead == '\n' ||
          lookahead == '\r' ||
          lookahead == ' ') SKIP(0)
      END_STATE();
    case 1:
      if (lookahead == 'e') ADVANCE(7);
      END_STATE();
    case 2:
      if (lookahead == 'a') ADVANCE(8);
      END_STATE();
    case 3:
      if (lookahead == 'm') ADVANCE(9);
      END_STATE();
    case 4:
      if (lookahead == 'u') ADVANCE(10);
      END_STATE();
    case 5:
      if (lookahead == 'e') ADVANCE(11);
      END_STATE();
    case 6:
      if (lookahead == 'r') ADVANCE(12);
      END_STATE();
    case 7:
      if (lookahead == 'l') ADVANCE(13);
      END_STATE();
    case 8:
      if (lookahead == 'l') ADVANCE(14);
      END_STATE();
    case 9:
      if (lookahead == 'p') ADVANCE(15);
      END_STATE();
    case 10:
      if (lookahead == 'l') ADVANCE(16);
      END_STATE();
    case 11:
      if (lookahead == 'p') ADVANCE(17);
      END_STATE();
    case 12:
      if (lookahead == 'u') ADVANCE(18);
      END_STATE();
    case 13:
      if (lookahead == 'e') ADVANCE(19);
      END_STATE();
    case 14:
      if (lookahead == 's') ADVANCE(20);
      END_STATE();
    case 15:
      if (lookahead == 'o') ADVANCE(21);
      END_STATE();
    case 16:
      if (lookahead == 'l') ADVANCE(22);
      END_STATE();
    case 17:
      if (lookahead == 'l') ADVANCE(23);
      END_STATE();
    case 18:
      if (lookahead == 'e') ADVANCE(24);
      END_STATE();
    case 19:
      if (lookahead == 't') ADVANCE(25);
      END_STATE();
    case 20:
      if (lookahead == 'e') ADVANCE(26);
      END_STATE();
    case 21:
      if (lookahead == 'r') ADVANCE(27);
      END_STATE();
    case 22:
      ACCEPT_TOKEN(sym_null);
      END_STATE();
    case 23:
      if (lookahead == 'a') ADVANCE(28);
      END_STATE();
    case 24:
      ACCEPT_TOKEN(anon_sym_true);
      END_STATE();
    case 25:
      if (lookahead == 'e') ADVANCE(29);
      END_STATE();
    case 26:
      ACCEPT_TOKEN(anon_sym_false);
      END_STATE();
    case 27:
      if (lookahead == 't') ADVANCE(30);
      END_STATE();
    case 28:
      if (lookahead == 'c') ADVANCE(31);
      END_STATE();
    case 29:
      ACCEPT_TOKEN(anon_sym_delete);
      END_STATE();
    case 30:
      ACCEPT_TOKEN(anon_sym_import);
      END_STATE();
    case 31:
      if (lookahead == 'e') ADVANCE(32);
      END_STATE();
    case 32:
      ACCEPT_TOKEN(anon_sym_replace);
      END_STATE();
    default:
      return false;
  }
}

static const TSLexMode ts_lex_modes[STATE_COUNT] = {
  [0] = {.lex_state = 0},
  [1] = {.lex_state = 9},
  [2] = {.lex_state = 0},
  [3] = {.lex_state = 9},
  [4] = {.lex_state = 9},
  [5] = {.lex_state = 9},
  [6] = {.lex_state = 9},
  [7] = {.lex_state = 0},
  [8] = {.lex_state = 9},
  [9] = {.lex_state = 9},
  [10] = {.lex_state = 9},
  [11] = {.lex_state = 0},
  [12] = {.lex_state = 9},
  [13] = {.lex_state = 0},
  [14] = {.lex_state = 9},
  [15] = {.lex_state = 9},
  [16] = {.lex_state = 0},
  [17] = {.lex_state = 0},
  [18] = {.lex_state = 9},
  [19] = {.lex_state = 9},
  [20] = {.lex_state = 9},
  [21] = {.lex_state = 9},
  [22] = {.lex_state = 9},
  [23] = {.lex_state = 9},
  [24] = {.lex_state = 9},
  [25] = {.lex_state = 9},
  [26] = {.lex_state = 9},
  [27] = {.lex_state = 9},
  [28] = {.lex_state = 9},
  [29] = {.lex_state = 9},
  [30] = {.lex_state = 9},
  [31] = {.lex_state = 9},
  [32] = {.lex_state = 9},
  [33] = {.lex_state = 9},
  [34] = {.lex_state = 9},
  [35] = {.lex_state = 9},
  [36] = {.lex_state = 9},
  [37] = {.lex_state = 9},
  [38] = {.lex_state = 0},
  [39] = {.lex_state = 0},
  [40] = {.lex_state = 0},
  [41] = {.lex_state = 0},
  [42] = {.lex_state = 0},
  [43] = {.lex_state = 0},
  [44] = {.lex_state = 0},
  [45] = {.lex_state = 0},
  [46] = {.lex_state = 0},
  [47] = {.lex_state = 0},
  [48] = {.lex_state = 0},
  [49] = {.lex_state = 0},
  [50] = {.lex_state = 0},
  [51] = {.lex_state = 9},
  [52] = {.lex_state = 0},
  [53] = {.lex_state = 9},
  [54] = {.lex_state = 0},
  [55] = {.lex_state = 0},
  [56] = {.lex_state = 0},
  [57] = {.lex_state = 0},
  [58] = {.lex_state = 0},
  [59] = {.lex_state = 9},
  [60] = {.lex_state = 0},
  [61] = {.lex_state = 9},
  [62] = {.lex_state = 9},
  [63] = {.lex_state = 0},
  [64] = {.lex_state = 0},
  [65] = {.lex_state = 9},
  [66] = {.lex_state = 9},
  [67] = {.lex_state = 0},
};

static const uint16_t ts_parse_table[LARGE_STATE_COUNT][SYMBOL_COUNT] = {
  [0] = {
    [ts_builtin_sym_end] = ACTIONS(1),
    [sym_identifier] = ACTIONS(1),
    [anon_sym_AT] = ACTIONS(1),
    [anon_sym_delete] = ACTIONS(1),
    [anon_sym_replace] = ACTIONS(1),
    [anon_sym_import] = ACTIONS(1),
    [anon_sym_LBRACK] = ACTIONS(1),
    [anon_sym_COMMA] = ACTIONS(1),
    [anon_sym_RBRACK] = ACTIONS(1),
    [anon_sym_LBRACE] = ACTIONS(1),
    [anon_sym_RBRACE] = ACTIONS(1),
    [anon_sym_COLON] = ACTIONS(1),
    [sym_multiline_string] = ACTIONS(1),
    [sym_string] = ACTIONS(1),
    [sym_float] = ACTIONS(1),
    [sym_integer] = ACTIONS(1),
    [anon_sym_true] = ACTIONS(1),
    [anon_sym_false] = ACTIONS(1),
    [sym_null] = ACTIONS(1),
    [sym_comment] = ACTIONS(3),
  },
  [1] = {
    [sym_document] = STATE(67),
    [sym__statement] = STATE(6),
    [sym_overlay_operation] = STATE(6),
    [sym_import] = STATE(6),
    [sym_block] = STATE(6),
    [sym_block_array] = STATE(6),
    [sym_scalar_array_field] = STATE(6),
    [sym_pair] = STATE(6),
    [sym__key] = STATE(43),
    [aux_sym_document_repeat1] = STATE(6),
    [ts_builtin_sym_end] = ACTIONS(5),
    [sym_identifier] = ACTIONS(7),
    [anon_sym_AT] = ACTIONS(9),
    [anon_sym_import] = ACTIONS(11),
    [sym_string] = ACTIONS(13),
    [sym_comment] = ACTIONS(3),
  },
};

static const uint16_t ts_small_parse_table[] = {
  [0] = 10,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(17), 1,
      anon_sym_RBRACK,
    ACTIONS(19), 1,
      anon_sym_LBRACE,
    ACTIONS(27), 1,
      sym_null,
    ACTIONS(21), 2,
      sym_multiline_string,
      sym_float,
    ACTIONS(23), 2,
      sym_string,
      sym_integer,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    STATE(38), 2,
      sym_block_array_item,
      aux_sym_block_array_repeat1,
    STATE(55), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [38] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(31), 1,
      sym_identifier,
    ACTIONS(34), 1,
      anon_sym_AT,
    ACTIONS(37), 1,
      anon_sym_import,
    ACTIONS(40), 1,
      sym_string,
    STATE(43), 1,
      sym__key,
    ACTIONS(29), 2,
      ts_builtin_sym_end,
      anon_sym_RBRACE,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [71] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(43), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(9), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [103] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(45), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [135] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(47), 1,
      ts_builtin_sym_end,
    STATE(43), 1,
      sym__key,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [167] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(49), 1,
      anon_sym_RBRACK,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    ACTIONS(55), 2,
      sym_string,
      sym_integer,
    ACTIONS(53), 3,
      sym_multiline_string,
      sym_float,
      sym_null,
    STATE(63), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [199] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(57), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [231] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(59), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [263] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(61), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(5), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [295] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    ACTIONS(63), 1,
      anon_sym_RBRACK,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    ACTIONS(55), 2,
      sym_string,
      sym_integer,
    ACTIONS(53), 3,
      sym_multiline_string,
      sym_float,
      sym_null,
    STATE(63), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [327] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(65), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(3), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [359] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    ACTIONS(67), 1,
      anon_sym_RBRACK,
    ACTIONS(23), 2,
      sym_string,
      sym_integer,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    ACTIONS(21), 3,
      sym_multiline_string,
      sym_float,
      sym_null,
    STATE(55), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [391] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(69), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(8), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [423] = 8,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(7), 1,
      sym_identifier,
    ACTIONS(9), 1,
      anon_sym_AT,
    ACTIONS(11), 1,
      anon_sym_import,
    ACTIONS(13), 1,
      sym_string,
    ACTIONS(71), 1,
      anon_sym_RBRACE,
    STATE(43), 1,
      sym__key,
    STATE(12), 8,
      sym__statement,
      sym_overlay_operation,
      sym_import,
      sym_block,
      sym_block_array,
      sym_scalar_array_field,
      sym_pair,
      aux_sym_document_repeat1,
  [455] = 7,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    ACTIONS(75), 2,
      sym_string,
      sym_integer,
    ACTIONS(73), 3,
      sym_multiline_string,
      sym_float,
      sym_null,
    STATE(30), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [484] = 7,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(15), 1,
      anon_sym_LBRACK,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    ACTIONS(25), 2,
      anon_sym_true,
      anon_sym_false,
    ACTIONS(55), 2,
      sym_string,
      sym_integer,
    ACTIONS(53), 3,
      sym_multiline_string,
      sym_float,
      sym_null,
    STATE(63), 4,
      sym__value,
      sym_object,
      sym_array,
      sym_boolean,
  [513] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(79), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(77), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [529] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(83), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(81), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [545] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(87), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(85), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [561] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(91), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(89), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [577] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(95), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(93), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [593] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(99), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(97), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [609] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(103), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(101), 6,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_COMMA,
      anon_sym_RBRACK,
      anon_sym_RBRACE,
      sym_string,
  [625] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(107), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(105), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [639] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(111), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(109), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [653] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(115), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(113), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [667] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(119), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(117), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [681] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(123), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(121), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [695] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(127), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(125), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [709] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(131), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(129), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [723] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(135), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(133), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [737] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(139), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(137), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [751] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(143), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(141), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [765] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(147), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(145), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [779] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(151), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(149), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [793] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(155), 2,
      anon_sym_import,
      sym_identifier,
    ACTIONS(153), 4,
      ts_builtin_sym_end,
      anon_sym_AT,
      anon_sym_RBRACE,
      sym_string,
  [807] = 5,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(157), 1,
      anon_sym_RBRACK,
    ACTIONS(159), 1,
      anon_sym_LBRACE,
    ACTIONS(161), 1,
      sym_null,
    STATE(39), 2,
      sym_block_array_item,
      aux_sym_block_array_repeat1,
  [824] = 5,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(163), 1,
      anon_sym_RBRACK,
    ACTIONS(165), 1,
      anon_sym_LBRACE,
    ACTIONS(168), 1,
      sym_null,
    STATE(39), 2,
      sym_block_array_item,
      aux_sym_block_array_repeat1,
  [841] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(171), 1,
      anon_sym_COMMA,
    ACTIONS(174), 1,
      anon_sym_RBRACK,
    ACTIONS(177), 2,
      anon_sym_LBRACE,
      sym_null,
  [855] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(179), 1,
      anon_sym_COMMA,
    ACTIONS(182), 1,
      anon_sym_RBRACK,
    ACTIONS(185), 2,
      anon_sym_LBRACE,
      sym_null,
  [869] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(187), 1,
      anon_sym_COMMA,
    ACTIONS(185), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [881] = 5,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(189), 1,
      anon_sym_LBRACK,
    ACTIONS(191), 1,
      anon_sym_LBRACE,
    ACTIONS(193), 1,
      anon_sym_COLON,
    STATE(33), 1,
      sym_array,
  [897] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(195), 1,
      anon_sym_COMMA,
    ACTIONS(197), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [909] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(199), 1,
      anon_sym_COMMA,
    ACTIONS(202), 1,
      anon_sym_RBRACK,
    ACTIONS(197), 2,
      anon_sym_LBRACE,
      sym_null,
  [923] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(205), 1,
      anon_sym_COMMA,
    ACTIONS(177), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [935] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(185), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [944] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(207), 1,
      anon_sym_COMMA,
    ACTIONS(209), 1,
      anon_sym_RBRACK,
    STATE(49), 1,
      aux_sym_import_repeat1,
  [957] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(211), 1,
      anon_sym_COMMA,
    ACTIONS(214), 1,
      anon_sym_RBRACK,
    STATE(49), 1,
      aux_sym_import_repeat1,
  [970] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(163), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [979] = 3,
    ACTIONS(3), 1,
      sym_comment,
    STATE(64), 1,
      sym__key,
    ACTIONS(216), 2,
      sym_string,
      sym_identifier,
  [990] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(218), 1,
      anon_sym_COMMA,
    ACTIONS(221), 1,
      anon_sym_RBRACK,
    STATE(52), 1,
      aux_sym_array_repeat1,
  [1003] = 3,
    ACTIONS(3), 1,
      sym_comment,
    STATE(36), 1,
      sym__key,
    ACTIONS(223), 2,
      sym_string,
      sym_identifier,
  [1014] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(225), 1,
      anon_sym_COMMA,
    ACTIONS(227), 1,
      anon_sym_RBRACK,
    STATE(48), 1,
      aux_sym_import_repeat1,
  [1027] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(229), 1,
      anon_sym_COMMA,
    ACTIONS(231), 1,
      anon_sym_RBRACK,
    STATE(57), 1,
      aux_sym_array_repeat1,
  [1040] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(233), 3,
      anon_sym_RBRACK,
      anon_sym_LBRACE,
      sym_null,
  [1049] = 4,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(63), 1,
      anon_sym_RBRACK,
    ACTIONS(235), 1,
      anon_sym_COMMA,
    STATE(52), 1,
      aux_sym_array_repeat1,
  [1062] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(237), 1,
      anon_sym_delete,
    ACTIONS(239), 1,
      anon_sym_replace,
  [1072] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(209), 1,
      anon_sym_RBRACK,
    ACTIONS(241), 1,
      sym_string,
  [1082] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(214), 2,
      anon_sym_COMMA,
      anon_sym_RBRACK,
  [1090] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(241), 1,
      sym_string,
    ACTIONS(243), 1,
      anon_sym_RBRACK,
  [1100] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(245), 1,
      anon_sym_RBRACK,
    ACTIONS(247), 1,
      sym_string,
  [1110] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(221), 2,
      anon_sym_COMMA,
      anon_sym_RBRACK,
  [1118] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(51), 1,
      anon_sym_LBRACE,
    STATE(31), 1,
      sym_object,
  [1128] = 3,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(249), 1,
      anon_sym_LBRACK,
    ACTIONS(251), 1,
      sym_string,
  [1138] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(241), 1,
      sym_string,
  [1145] = 2,
    ACTIONS(3), 1,
      sym_comment,
    ACTIONS(253), 1,
      ts_builtin_sym_end,
};

static const uint32_t ts_small_parse_table_map[] = {
  [SMALL_STATE(2)] = 0,
  [SMALL_STATE(3)] = 38,
  [SMALL_STATE(4)] = 71,
  [SMALL_STATE(5)] = 103,
  [SMALL_STATE(6)] = 135,
  [SMALL_STATE(7)] = 167,
  [SMALL_STATE(8)] = 199,
  [SMALL_STATE(9)] = 231,
  [SMALL_STATE(10)] = 263,
  [SMALL_STATE(11)] = 295,
  [SMALL_STATE(12)] = 327,
  [SMALL_STATE(13)] = 359,
  [SMALL_STATE(14)] = 391,
  [SMALL_STATE(15)] = 423,
  [SMALL_STATE(16)] = 455,
  [SMALL_STATE(17)] = 484,
  [SMALL_STATE(18)] = 513,
  [SMALL_STATE(19)] = 529,
  [SMALL_STATE(20)] = 545,
  [SMALL_STATE(21)] = 561,
  [SMALL_STATE(22)] = 577,
  [SMALL_STATE(23)] = 593,
  [SMALL_STATE(24)] = 609,
  [SMALL_STATE(25)] = 625,
  [SMALL_STATE(26)] = 639,
  [SMALL_STATE(27)] = 653,
  [SMALL_STATE(28)] = 667,
  [SMALL_STATE(29)] = 681,
  [SMALL_STATE(30)] = 695,
  [SMALL_STATE(31)] = 709,
  [SMALL_STATE(32)] = 723,
  [SMALL_STATE(33)] = 737,
  [SMALL_STATE(34)] = 751,
  [SMALL_STATE(35)] = 765,
  [SMALL_STATE(36)] = 779,
  [SMALL_STATE(37)] = 793,
  [SMALL_STATE(38)] = 807,
  [SMALL_STATE(39)] = 824,
  [SMALL_STATE(40)] = 841,
  [SMALL_STATE(41)] = 855,
  [SMALL_STATE(42)] = 869,
  [SMALL_STATE(43)] = 881,
  [SMALL_STATE(44)] = 897,
  [SMALL_STATE(45)] = 909,
  [SMALL_STATE(46)] = 923,
  [SMALL_STATE(47)] = 935,
  [SMALL_STATE(48)] = 944,
  [SMALL_STATE(49)] = 957,
  [SMALL_STATE(50)] = 970,
  [SMALL_STATE(51)] = 979,
  [SMALL_STATE(52)] = 990,
  [SMALL_STATE(53)] = 1003,
  [SMALL_STATE(54)] = 1014,
  [SMALL_STATE(55)] = 1027,
  [SMALL_STATE(56)] = 1040,
  [SMALL_STATE(57)] = 1049,
  [SMALL_STATE(58)] = 1062,
  [SMALL_STATE(59)] = 1072,
  [SMALL_STATE(60)] = 1082,
  [SMALL_STATE(61)] = 1090,
  [SMALL_STATE(62)] = 1100,
  [SMALL_STATE(63)] = 1110,
  [SMALL_STATE(64)] = 1118,
  [SMALL_STATE(65)] = 1128,
  [SMALL_STATE(66)] = 1138,
  [SMALL_STATE(67)] = 1145,
};

static const TSParseActionEntry ts_parse_actions[] = {
  [0] = {.entry = {.count = 0, .reusable = false}},
  [1] = {.entry = {.count = 1, .reusable = false}}, RECOVER(),
  [3] = {.entry = {.count = 1, .reusable = true}}, SHIFT_EXTRA(),
  [5] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_document, 0),
  [7] = {.entry = {.count = 1, .reusable = false}}, SHIFT(43),
  [9] = {.entry = {.count = 1, .reusable = true}}, SHIFT(58),
  [11] = {.entry = {.count = 1, .reusable = false}}, SHIFT(65),
  [13] = {.entry = {.count = 1, .reusable = true}}, SHIFT(43),
  [15] = {.entry = {.count = 1, .reusable = true}}, SHIFT(13),
  [17] = {.entry = {.count = 1, .reusable = true}}, SHIFT(35),
  [19] = {.entry = {.count = 1, .reusable = true}}, SHIFT(15),
  [21] = {.entry = {.count = 1, .reusable = true}}, SHIFT(55),
  [23] = {.entry = {.count = 1, .reusable = false}}, SHIFT(55),
  [25] = {.entry = {.count = 1, .reusable = true}}, SHIFT(23),
  [27] = {.entry = {.count = 1, .reusable = true}}, SHIFT(45),
  [29] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_document_repeat1, 2),
  [31] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_document_repeat1, 2), SHIFT_REPEAT(43),
  [34] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_document_repeat1, 2), SHIFT_REPEAT(58),
  [37] = {.entry = {.count = 2, .reusable = false}}, REDUCE(aux_sym_document_repeat1, 2), SHIFT_REPEAT(65),
  [40] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_document_repeat1, 2), SHIFT_REPEAT(43),
  [43] = {.entry = {.count = 1, .reusable = true}}, SHIFT(29),
  [45] = {.entry = {.count = 1, .reusable = true}}, SHIFT(42),
  [47] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_document, 1),
  [49] = {.entry = {.count = 1, .reusable = true}}, SHIFT(24),
  [51] = {.entry = {.count = 1, .reusable = true}}, SHIFT(14),
  [53] = {.entry = {.count = 1, .reusable = true}}, SHIFT(63),
  [55] = {.entry = {.count = 1, .reusable = false}}, SHIFT(63),
  [57] = {.entry = {.count = 1, .reusable = true}}, SHIFT(18),
  [59] = {.entry = {.count = 1, .reusable = true}}, SHIFT(34),
  [61] = {.entry = {.count = 1, .reusable = true}}, SHIFT(46),
  [63] = {.entry = {.count = 1, .reusable = true}}, SHIFT(19),
  [65] = {.entry = {.count = 1, .reusable = true}}, SHIFT(41),
  [67] = {.entry = {.count = 1, .reusable = true}}, SHIFT(22),
  [69] = {.entry = {.count = 1, .reusable = true}}, SHIFT(20),
  [71] = {.entry = {.count = 1, .reusable = true}}, SHIFT(40),
  [73] = {.entry = {.count = 1, .reusable = true}}, SHIFT(30),
  [75] = {.entry = {.count = 1, .reusable = false}}, SHIFT(30),
  [77] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_object, 3),
  [79] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_object, 3),
  [81] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_array, 4),
  [83] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_array, 4),
  [85] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_object, 2),
  [87] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_object, 2),
  [89] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_array, 3),
  [91] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_array, 3),
  [93] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_array, 2),
  [95] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_array, 2),
  [97] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_boolean, 1),
  [99] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_boolean, 1),
  [101] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_array, 5),
  [103] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_array, 5),
  [105] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_import, 4),
  [107] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_import, 4),
  [109] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_array, 4, .production_id = 3),
  [111] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block_array, 4, .production_id = 3),
  [113] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_import, 6),
  [115] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_import, 6),
  [117] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_import, 5),
  [119] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_import, 5),
  [121] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block, 3, .production_id = 3),
  [123] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block, 3, .production_id = 3),
  [125] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_pair, 3, .production_id = 4),
  [127] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_pair, 3, .production_id = 4),
  [129] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_overlay_operation, 4, .production_id = 5),
  [131] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_overlay_operation, 4, .production_id = 5),
  [133] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_import, 2),
  [135] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_import, 2),
  [137] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_scalar_array_field, 2, .production_id = 1),
  [139] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_scalar_array_field, 2, .production_id = 1),
  [141] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block, 4, .production_id = 3),
  [143] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block, 4, .production_id = 3),
  [145] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_array, 3, .production_id = 3),
  [147] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_block_array, 3, .production_id = 3),
  [149] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_overlay_operation, 3, .production_id = 2),
  [151] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_overlay_operation, 3, .production_id = 2),
  [153] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_import, 3),
  [155] = {.entry = {.count = 1, .reusable = false}}, REDUCE(sym_import, 3),
  [157] = {.entry = {.count = 1, .reusable = true}}, SHIFT(26),
  [159] = {.entry = {.count = 1, .reusable = true}}, SHIFT(10),
  [161] = {.entry = {.count = 1, .reusable = true}}, SHIFT(44),
  [163] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_block_array_repeat1, 2),
  [165] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_block_array_repeat1, 2), SHIFT_REPEAT(10),
  [168] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_block_array_repeat1, 2), SHIFT_REPEAT(44),
  [171] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym_object, 2), SHIFT(47),
  [174] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym_block_array_item, 2), REDUCE(sym_object, 2),
  [177] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_array_item, 2),
  [179] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym_object, 3), SHIFT(56),
  [182] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym_block_array_item, 3), REDUCE(sym_object, 3),
  [185] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_array_item, 3),
  [187] = {.entry = {.count = 1, .reusable = true}}, SHIFT(56),
  [189] = {.entry = {.count = 1, .reusable = true}}, SHIFT(2),
  [191] = {.entry = {.count = 1, .reusable = true}}, SHIFT(4),
  [193] = {.entry = {.count = 1, .reusable = true}}, SHIFT(16),
  [195] = {.entry = {.count = 1, .reusable = true}}, SHIFT(50),
  [197] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_block_array_repeat1, 1),
  [199] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym__value, 1), SHIFT(50),
  [202] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym__value, 1), REDUCE(aux_sym_block_array_repeat1, 1),
  [205] = {.entry = {.count = 1, .reusable = true}}, SHIFT(47),
  [207] = {.entry = {.count = 1, .reusable = true}}, SHIFT(61),
  [209] = {.entry = {.count = 1, .reusable = true}}, SHIFT(28),
  [211] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_import_repeat1, 2), SHIFT_REPEAT(66),
  [214] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_import_repeat1, 2),
  [216] = {.entry = {.count = 1, .reusable = true}}, SHIFT(64),
  [218] = {.entry = {.count = 2, .reusable = true}}, REDUCE(aux_sym_array_repeat1, 2), SHIFT_REPEAT(17),
  [221] = {.entry = {.count = 1, .reusable = true}}, REDUCE(aux_sym_array_repeat1, 2),
  [223] = {.entry = {.count = 1, .reusable = true}}, SHIFT(36),
  [225] = {.entry = {.count = 1, .reusable = true}}, SHIFT(59),
  [227] = {.entry = {.count = 1, .reusable = true}}, SHIFT(25),
  [229] = {.entry = {.count = 1, .reusable = true}}, SHIFT(11),
  [231] = {.entry = {.count = 1, .reusable = true}}, SHIFT(21),
  [233] = {.entry = {.count = 1, .reusable = true}}, REDUCE(sym_block_array_item, 4),
  [235] = {.entry = {.count = 1, .reusable = true}}, SHIFT(7),
  [237] = {.entry = {.count = 1, .reusable = true}}, SHIFT(53),
  [239] = {.entry = {.count = 1, .reusable = true}}, SHIFT(51),
  [241] = {.entry = {.count = 1, .reusable = true}}, SHIFT(60),
  [243] = {.entry = {.count = 1, .reusable = true}}, SHIFT(27),
  [245] = {.entry = {.count = 1, .reusable = true}}, SHIFT(37),
  [247] = {.entry = {.count = 1, .reusable = true}}, SHIFT(54),
  [249] = {.entry = {.count = 1, .reusable = true}}, SHIFT(62),
  [251] = {.entry = {.count = 1, .reusable = true}}, SHIFT(32),
  [253] = {.entry = {.count = 1, .reusable = true}},  ACCEPT_INPUT(),
};

#ifdef __cplusplus
extern "C" {
#endif
#ifdef _WIN32
#define extern __declspec(dllexport)
#endif

extern const TSLanguage *tree_sitter_skg(void) {
  static const TSLanguage language = {
    .version = LANGUAGE_VERSION,
    .symbol_count = SYMBOL_COUNT,
    .alias_count = ALIAS_COUNT,
    .token_count = TOKEN_COUNT,
    .external_token_count = EXTERNAL_TOKEN_COUNT,
    .state_count = STATE_COUNT,
    .large_state_count = LARGE_STATE_COUNT,
    .production_id_count = PRODUCTION_ID_COUNT,
    .field_count = FIELD_COUNT,
    .max_alias_sequence_length = MAX_ALIAS_SEQUENCE_LENGTH,
    .parse_table = &ts_parse_table[0][0],
    .small_parse_table = ts_small_parse_table,
    .small_parse_table_map = ts_small_parse_table_map,
    .parse_actions = ts_parse_actions,
    .symbol_names = ts_symbol_names,
    .field_names = ts_field_names,
    .field_map_slices = ts_field_map_slices,
    .field_map_entries = ts_field_map_entries,
    .symbol_metadata = ts_symbol_metadata,
    .public_symbol_map = ts_symbol_map,
    .alias_map = ts_non_terminal_alias_map,
    .alias_sequences = &ts_alias_sequences[0][0],
    .lex_modes = ts_lex_modes,
    .lex_fn = ts_lex,
    .keyword_lex_fn = ts_lex_keywords,
    .keyword_capture_token = sym_identifier,
    .primary_state_ids = ts_primary_state_ids,
  };
  return &language;
}
#ifdef __cplusplus
}
#endif
