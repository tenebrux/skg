package skg

import (
	"testing"
)

func TestParseSimple(t *testing.T) {
	src := []byte(`name: "hello"`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(f.Children))
	}
	if f.Children[0].Field == nil {
		t.Fatal("expected field node")
	}
	if f.Children[0].Field.Key != "name" {
		t.Errorf("expected key 'name', got %q", f.Children[0].Field.Key)
	}
	if f.Children[0].Field.Value.Str != "hello" {
		t.Errorf("expected value 'hello', got %q", f.Children[0].Field.Value.Str)
	}
}

func TestParseAllTypes(t *testing.T) {
	src := []byte(`
str: "hello"
num: 42
neg: -7
pi: 3.14
yes: true
no: false
empty: null
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 7 {
		t.Fatalf("expected 7 children, got %d", len(f.Children))
	}

	check := func(idx int, key string, typ ValueType) {
		t.Helper()
		n := f.Children[idx]
		if n.Field == nil {
			t.Fatalf("child %d: expected field", idx)
		}
		if n.Field.Key != key {
			t.Errorf("child %d: expected key %q, got %q", idx, key, n.Field.Key)
		}
		if n.Field.Value.Type != typ {
			t.Errorf("child %d: expected type %v, got %v", idx, typ, n.Field.Value.Type)
		}
	}

	check(0, "str", TypeString)
	check(1, "num", TypeInt)
	check(2, "neg", TypeInt)
	check(3, "pi", TypeFloat)
	check(4, "yes", TypeBool)
	check(5, "no", TypeBool)
	check(6, "empty", TypeNull)
}

func TestParseBlock(t *testing.T) {
	src := []byte(`theme { accent: "green" }`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 || f.Children[0].Block == nil {
		t.Fatal("expected block node")
	}
	b := f.Children[0].Block
	if b.Name != "theme" {
		t.Errorf("expected block name 'theme', got %q", b.Name)
	}
	if len(b.Children) != 1 || b.Children[0].Field == nil {
		t.Fatal("expected one field child")
	}
}

func TestParseArray(t *testing.T) {
	src := []byte(`tags: ["a", "b", "c"]`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	v := f.Children[0].Field.Value
	if v.Type != TypeArray {
		t.Fatalf("expected array, got %v", v.Type)
	}
	if len(v.Array.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(v.Array.Items))
	}
	if v.Array.ElementType != TypeString {
		t.Errorf("expected string element type, got %v", v.Array.ElementType)
	}
}

func TestParseMixedArrayError(t *testing.T) {
	src := []byte(`bad: [1, "two"]`)
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected error for mixed array")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected ParseError, got %T", err)
	}
	if pe.Diag.Message != "mixed types in array" {
		t.Errorf("unexpected message: %s", pe.Diag.Message)
	}
}

func TestParseVersions(t *testing.T) {
	src := []byte(`skg_version: "1.0"
schema_version: "2.0"
name: "test"
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.SKGVersion == nil || *f.SKGVersion != "1.0" {
		t.Errorf("expected skg_version 1.0")
	}
	if f.SchemaVersion == nil || *f.SchemaVersion != "2.0" {
		t.Errorf("expected schema_version 2.0")
	}
}

func TestParseImports(t *testing.T) {
	src := []byte(`import ["./a.skg", "./b.skg"]
name: "test"
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ImportPaths) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(f.ImportPaths))
	}
	if f.ImportPaths[0] != "./a.skg" || f.ImportPaths[1] != "./b.skg" {
		t.Errorf("unexpected imports: %v", f.ImportPaths)
	}
}

func TestParseSingleImport(t *testing.T) {
	src := []byte(`import "./base.skg"`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ImportPaths) != 1 || f.ImportPaths[0] != "./base.skg" {
		t.Errorf("unexpected imports: %v", f.ImportPaths)
	}
}

func TestParseMultilineString(t *testing.T) {
	src := []byte(`desc: """hello
world"""`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Children[0].Field.Value.Str != "hello\nworld" {
		t.Errorf("unexpected value: %q", f.Children[0].Field.Value.Str)
	}
}

func TestParseEscapedString(t *testing.T) {
	src := []byte(`msg: "hello\nworld"`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Children[0].Field.Value.Str != "hello\nworld" {
		t.Errorf("unexpected value: %q", f.Children[0].Field.Value.Str)
	}
}

func TestParseComments(t *testing.T) {
	src := []byte(`# this is a comment
name: "test"
# another comment
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(f.Children))
	}
}

func TestParseDuplicateLastWins(t *testing.T) {
	src := []byte(`name: "first"
name: "second"
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 {
		t.Fatalf("expected 1 child after dedup, got %d", len(f.Children))
	}
	if f.Children[0].Field.Value.Str != "second" {
		t.Errorf("expected last-wins 'second', got %q", f.Children[0].Field.Value.Str)
	}
}

func TestMergeNodes(t *testing.T) {
	base := []Node{
		{Field: &Field{Key: "a", Value: Value{Type: TypeString, Str: "1"}}},
		{Field: &Field{Key: "b", Value: Value{Type: TypeString, Str: "2"}}},
	}
	overlay := []Node{
		{Field: &Field{Key: "b", Value: Value{Type: TypeString, Str: "3"}}},
		{Field: &Field{Key: "c", Value: Value{Type: TypeString, Str: "4"}}},
	}
	result := MergeNodes(base, overlay)
	if len(result) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result))
	}
	if result[0].Field.Value.Str != "1" {
		t.Error("a should be unchanged")
	}
	if result[1].Field.Value.Str != "3" {
		t.Error("b should be overwritten to 3")
	}
	if result[2].Field.Value.Str != "4" {
		t.Error("c should be appended")
	}
}

func TestMergeBlocksRecursive(t *testing.T) {
	base := []Node{
		{Block: &Block{Name: "theme", Children: []Node{
			{Field: &Field{Key: "a", Value: Value{Type: TypeString, Str: "1"}}},
		}}},
	}
	overlay := []Node{
		{Block: &Block{Name: "theme", Children: []Node{
			{Field: &Field{Key: "b", Value: Value{Type: TypeString, Str: "2"}}},
		}}},
	}
	result := MergeNodes(base, overlay)
	if len(result) != 1 || result[0].Block == nil {
		t.Fatal("expected 1 block")
	}
	if len(result[0].Block.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(result[0].Block.Children))
	}
}

func TestMergeNodesEmptyNode(t *testing.T) {
	// A hand-built Node with no variant set must not panic the merge.
	base := []Node{{}, {Field: &Field{Key: "a", Value: Value{Type: TypeInt, Int: 1}}}}
	overlay := []Node{{}, {Field: &Field{Key: "a", Value: Value{Type: TypeInt, Int: 2}}}}

	result := MergeNodes(base, overlay)
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].Field == nil || result[0].Field.Value.Int != 2 {
		t.Errorf("expected overlay to win, got %+v", result[0])
	}
}

func TestEmitRoundTrip(t *testing.T) {
	src := `name: "hello"
count: 42
pi: 3.14
flag: true
empty: null
tags: ["a", "b"]
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := string(Emit(f))
	f2, err := Parse([]byte(out))
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(f2.Children) != len(f.Children) {
		t.Errorf("round-trip changed child count: %d → %d", len(f.Children), len(f2.Children))
	}
}

func TestEmitBlockIndent(t *testing.T) {
	src := `theme {
  accent: "green"
}
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := string(Emit(f))
	if out != src {
		t.Errorf("expected:\n%s\ngot:\n%s", src, out)
	}
}

func TestUnmarshal(t *testing.T) {
	type Theme struct {
		Accent string `skg:"accent"`
		Size   int64  `skg:"size"`
	}
	type Config struct {
		Name  string   `skg:"name"`
		Theme Theme    `skg:"theme"`
		Tags  []string `skg:"tags"`
	}

	src := []byte(`
name: "test"
theme {
  accent: "blue"
  size: 14
}
tags: ["a", "b"]
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "test" {
		t.Errorf("expected name 'test', got %q", cfg.Name)
	}
	if cfg.Theme.Accent != "blue" {
		t.Errorf("expected accent 'blue', got %q", cfg.Theme.Accent)
	}
	if cfg.Theme.Size != 14 {
		t.Errorf("expected size 14, got %d", cfg.Theme.Size)
	}
	if len(cfg.Tags) != 2 || cfg.Tags[0] != "a" {
		t.Errorf("unexpected tags: %v", cfg.Tags)
	}
}

func TestUnmarshalNullable(t *testing.T) {
	type Config struct {
		Name  *string `skg:"name"`
		Value *string `skg:"value"`
	}
	src := []byte(`name: "test"
value: null
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Name == nil || *cfg.Name != "test" {
		t.Errorf("expected name 'test'")
	}
	if cfg.Value != nil {
		t.Errorf("expected value nil, got %v", *cfg.Value)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	type Config struct {
		Name  string   `skg:"name"`
		Count int64    `skg:"count"`
		Tags  []string `skg:"tags"`
	}

	orig := Config{Name: "test", Count: 42, Tags: []string{"a", "b"}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}

	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != orig.Name || got.Count != orig.Count {
		t.Errorf("round-trip mismatch: %+v vs %+v", orig, got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("tags mismatch: %v", got.Tags)
	}
}

func TestMarshalNested(t *testing.T) {
	type Inner struct {
		Color string `skg:"color"`
	}
	type Config struct {
		Theme Inner `skg:"theme"`
	}
	data, err := Marshal(Config{Theme: Inner{Color: "red"}})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	expected := "theme {\n  color: \"red\"\n}\n"
	if out != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, out)
	}
}

func TestUnmarshalMapStringSlice(t *testing.T) {
	// map[string][]string - field keys are map keys, arrays are values
	type Config struct {
		Addons map[string][]string `skg:"addons"`
	}
	src := []byte(`
addons {
  audio: ["ardour", "jack2"]
  gaming: ["steam", "lutris"]
}
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Addons) != 2 {
		t.Fatalf("expected 2 addon groups, got %d", len(cfg.Addons))
	}
	if len(cfg.Addons["audio"]) != 2 || cfg.Addons["audio"][0] != "ardour" {
		t.Errorf("unexpected audio addons: %v", cfg.Addons["audio"])
	}
	if len(cfg.Addons["gaming"]) != 2 || cfg.Addons["gaming"][1] != "lutris" {
		t.Errorf("unexpected gaming addons: %v", cfg.Addons["gaming"])
	}
}

func TestUnmarshalMapStringString(t *testing.T) {
	// map[string]string - flat key-value pairs
	type Config struct {
		Env map[string]string `skg:"env"`
	}
	src := []byte(`
env {
  HOME: "/home/user"
  SHELL: "/bin/zsh"
}
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Env["HOME"] != "/home/user" {
		t.Errorf("expected HOME=/home/user, got %q", cfg.Env["HOME"])
	}
	if cfg.Env["SHELL"] != "/bin/zsh" {
		t.Errorf("expected SHELL=/bin/zsh, got %q", cfg.Env["SHELL"])
	}
}

func TestUnmarshalMapStringAny(t *testing.T) {
	// map[string]interface{} - the Extra bag pattern
	type Config struct {
		Extra map[string]interface{} `skg:"extra"`
	}
	src := []byte(`
extra {
  retries: 3
  label: "custom"
  verbose: true
  rate: 1.5
  nothing: null
}
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Extra["retries"] != int64(3) {
		t.Errorf("expected retries=3, got %v (%T)", cfg.Extra["retries"], cfg.Extra["retries"])
	}
	if cfg.Extra["label"] != "custom" {
		t.Errorf("expected label=custom, got %v", cfg.Extra["label"])
	}
	if cfg.Extra["verbose"] != true {
		t.Errorf("expected verbose=true, got %v", cfg.Extra["verbose"])
	}
	if cfg.Extra["rate"] != 1.5 {
		t.Errorf("expected rate=1.5, got %v", cfg.Extra["rate"])
	}
	if cfg.Extra["nothing"] != nil {
		t.Errorf("expected nothing=nil, got %v", cfg.Extra["nothing"])
	}
}

func TestUnmarshalMapStruct(t *testing.T) {
	// map[string]struct - block names are map keys, children decode into struct
	type Disk struct {
		Device string `skg:"device"`
		FS     string `skg:"fs"`
		Mount  string `skg:"mount"`
	}
	type Config struct {
		Disks map[string]Disk `skg:"disks"`
	}
	src := []byte(`
disks {
  root {
    device: "/dev/sda1"
    fs: "ext4"
    mount: "/"
  }
  home {
    device: "/dev/sda2"
    fs: "btrfs"
    mount: "/home"
  }
}
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(cfg.Disks))
	}
	root := cfg.Disks["root"]
	if root.Device != "/dev/sda1" || root.FS != "ext4" || root.Mount != "/" {
		t.Errorf("unexpected root disk: %+v", root)
	}
	home := cfg.Disks["home"]
	if home.Device != "/dev/sda2" || home.FS != "btrfs" || home.Mount != "/home" {
		t.Errorf("unexpected home disk: %+v", home)
	}
}

func TestMarshalMap(t *testing.T) {
	type Config struct {
		Addons map[string][]string `skg:"addons"`
	}
	cfg := Config{
		Addons: map[string][]string{
			"audio": {"ardour", "jack2"},
		},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip it back
	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Addons["audio"]) != 2 || got.Addons["audio"][0] != "ardour" {
		t.Errorf("round-trip failed: %v", got.Addons)
	}
}

func TestMarshalMapAny(t *testing.T) {
	type Config struct {
		Extra map[string]interface{} `skg:"extra"`
	}
	cfg := Config{
		Extra: map[string]interface{}{
			"count": int64(5),
			"name":  "test",
			"ok":    true,
		},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Extra["count"] != int64(5) {
		t.Errorf("expected count=5, got %v", got.Extra["count"])
	}
	if got.Extra["name"] != "test" {
		t.Errorf("expected name=test, got %v", got.Extra["name"])
	}
}

func TestParseErrorDiagnostic(t *testing.T) {
	src := []byte(`name`)
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected ParseError, got %T", err)
	}
	if pe.Diag.Line == 0 {
		t.Error("expected non-zero line in diagnostic")
	}
	if pe.Diag.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestParseBlockArray(t *testing.T) {
	src := []byte(`
users [
  {
    name: "admin"
    sudo: true
    groups: ["wheel", "video"]
  }
  {
    name: "guest"
    sudo: false
    groups: ["users"]
  }
]
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(f.Children))
	}
	ba := f.Children[0].BlockArray
	if ba == nil {
		t.Fatal("expected block array node")
	}
	if ba.Name != "users" {
		t.Errorf("expected name 'users', got %q", ba.Name)
	}
	if len(ba.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(ba.Items))
	}
	// First item: name=levi
	if ba.Items[0][0].Field == nil || ba.Items[0][0].Field.Key != "name" || ba.Items[0][0].Field.Value.Str != "admin" {
		t.Error("first item: expected name=levi")
	}
	// Second item: name=guest
	if ba.Items[1][0].Field == nil || ba.Items[1][0].Field.Key != "name" || ba.Items[1][0].Field.Value.Str != "guest" {
		t.Error("second item: expected name=guest")
	}
}

func TestBlockArrayUnmarshal(t *testing.T) {
	type User struct {
		Name   string   `skg:"name"`
		Sudo   bool     `skg:"sudo"`
		Groups []string `skg:"groups"`
	}
	type Config struct {
		Users []User `skg:"users"`
	}

	src := []byte(`
users [
  {
    name: "admin"
    sudo: true
    groups: ["wheel", "video"]
  }
  {
    name: "guest"
    sudo: false
    groups: ["users"]
  }
]
`)
	var cfg Config
	if err := Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(cfg.Users))
	}
	if cfg.Users[0].Name != "admin" {
		t.Errorf("expected first user 'levi', got %q", cfg.Users[0].Name)
	}
	if !cfg.Users[0].Sudo {
		t.Error("expected first user sudo=true")
	}
	if len(cfg.Users[0].Groups) != 2 || cfg.Users[0].Groups[0] != "wheel" {
		t.Errorf("expected first user groups=[wheel, video], got %v", cfg.Users[0].Groups)
	}
	if cfg.Users[1].Name != "guest" {
		t.Errorf("expected second user 'guest', got %q", cfg.Users[1].Name)
	}
	if cfg.Users[1].Sudo {
		t.Error("expected second user sudo=false")
	}
}

func TestBlockArrayMarshal(t *testing.T) {
	type User struct {
		Name string `skg:"name"`
		Sudo bool   `skg:"sudo"`
	}
	type Config struct {
		Users []User `skg:"users"`
	}

	cfg := Config{
		Users: []User{
			{Name: "admin", Sudo: true},
			{Name: "guest", Sudo: false},
		},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Round-trip: unmarshal the marshaled output
	var cfg2 Config
	if err := Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v\ndata:\n%s", err, string(data))
	}
	if len(cfg2.Users) != 2 {
		t.Fatalf("round-trip: expected 2 users, got %d", len(cfg2.Users))
	}
	if cfg2.Users[0].Name != "admin" || cfg2.Users[1].Name != "guest" {
		t.Errorf("round-trip: user names don't match")
	}
}

func TestBlockArrayEmit(t *testing.T) {
	src := []byte(`users [
  {
    name: "admin"
  }
  {
    name: "guest"
  }
]
`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	out := Emit(f)
	expected := "users [\n  {\n    name: \"admin\"\n  }\n  {\n    name: \"guest\"\n  }\n]\n"
	if string(out) != expected {
		t.Errorf("emit mismatch:\ngot:\n%s\nexpected:\n%s", string(out), expected)
	}
}

func TestBlockArrayColonlessFallback(t *testing.T) {
	// name [ scalar_values ] without a colon should still work
	src := []byte(`tags ["alpha", "beta"]`)
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(f.Children))
	}
	field := f.Children[0].Field
	if field == nil {
		t.Fatal("expected field node for scalar array")
	}
	if field.Value.Type != TypeArray {
		t.Fatalf("expected array value, got %v", field.Value.Type)
	}
	if len(field.Value.Array.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(field.Value.Array.Items))
	}
}
