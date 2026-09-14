package skg

// MergeNodes composes overlay on base. Deletion and replacement markers survive
// so the result can be composed again without resurrecting earlier values.
// Call MaterializeNodes once after all overlays to obtain final values.
//
// Fields with the same key: overlay wins (last-wins).
// Blocks with the same name: children merged recursively.
// New keys/blocks from overlay are appended.
// Nodes with no variant set carry no data and are dropped.
func MergeNodes(base, overlay []Node) []Node {
	if len(overlay) == 0 {
		return base
	}

	result := make([]Node, 0, len(base)+len(overlay))
	index := make(map[string]int, len(base)+len(overlay))

	for _, n := range base {
		key, ok := nodeKey(n)
		if !ok {
			continue // empty node carries no data
		}
		index[key] = len(result)
		result = append(result, n)
	}

	for _, ov := range overlay {
		key, keyed := nodeKey(ov)
		if !keyed {
			continue
		}
		if pos, ok := index[key]; ok {
			if ov.Block != nil && !ov.Block.Replace && result[pos].Block != nil {
				merged := MergeNodes(result[pos].Block.Children, ov.Block.Children)
				result[pos] = Node{Block: &Block{
					Replace:  result[pos].Block.Replace,
					Path:     result[pos].Block.Path,
					Name:     result[pos].Block.Name,
					Children: merged,
					Line:     result[pos].Block.Line,
					Col:      result[pos].Block.Col,
				}}
			} else {
				// A scalar/null/delete followed by an object is a replacement
				// barrier too. Keep it when summarizing an imported overlay.
				if ov.Block != nil && !ov.Block.Replace {
					copy := *ov.Block
					copy.Replace = true
					ov.Block = &copy
				}
				result[pos] = ov
			}
		} else {
			index[key] = len(result)
			result = append(result, ov)
		}
	}

	return result
}

// nodeKey returns the merge key of n. It reports false for a node with all
// variants nil, which the parser never produces but a caller building
// nodes by hand can.
func nodeKey(n Node) (string, bool) {
	if n.Delete != nil {
		return n.Delete.Key, true
	}
	if n.Field != nil {
		return n.Field.Key, true
	}
	if n.Block != nil {
		return n.Block.Name, true
	}
	if n.BlockArray != nil {
		return n.BlockArray.Name, true
	}
	return "", false
}

// MaterializeNodes finishes a composed overlay against an empty base. Deletes
// disappear and replacement markers clear, including inside nested values.
// The result shares immutable scalar data but never mutates the input tree.
func MaterializeNodes(nodes []Node) []Node {
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		switch {
		case n.Delete != nil:
			continue
		case n.Field != nil:
			f := *n.Field
			f.Value = materializeValue(f.Value)
			n.Field = &f
		case n.Block != nil:
			b := *n.Block
			b.Replace = false
			b.Children = MaterializeNodes(b.Children)
			n.Block = &b
		case n.BlockArray != nil:
			b := *n.BlockArray
			b.Items = materializeItems(b.Items)
			n.BlockArray = &b
		default:
			continue
		}
		out = append(out, n)
	}
	return out
}

func materializeItems(items []Value) []Value {
	out := make([]Value, len(items))
	for i, v := range items {
		out[i] = materializeValue(v)
	}
	return out
}

func materializeValue(v Value) Value {
	switch v.Type {
	case TypeObject:
		v.Object = MaterializeNodes(v.Object)
	case TypeArray:
		if v.Array != nil {
			a := *v.Array
			a.Items = materializeItems(a.Items)
			v.Array = &a
		}
	}
	return v
}
