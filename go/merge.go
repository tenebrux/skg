package skg

// MergeNodes overlays `overlay` on top of `base`, returning a new slice.
//
// Fields with the same key: overlay wins (last-wins).
// Blocks with the same name: children merged recursively.
// New keys/blocks from overlay are appended.
// Nodes with no field, block, or block array set carry no data and are dropped.
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
			if ov.Block != nil && result[pos].Block != nil {
				merged := MergeNodes(result[pos].Block.Children, ov.Block.Children)
				result[pos] = Node{Block: &Block{
					Name:     result[pos].Block.Name,
					Children: merged,
					Line:     result[pos].Block.Line,
					Col:      result[pos].Block.Col,
				}}
			} else {
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
// three variants nil, which the parser never produces but a caller building
// nodes by hand can.
func nodeKey(n Node) (string, bool) {
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
