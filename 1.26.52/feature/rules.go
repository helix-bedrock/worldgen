package feature

import (
	"encoding/json"
	"fmt"
	"slices"
)

// RuleHash is the hashed-string hash BDS keeps with a rule's name: 64-bit
// FNV-1 (multiply, then XOR), of which the rule seed uses the low 32 bits.
func RuleHash(s string) uint32 {
	h := uint64(0xCBF29CE484222325)
	for i := 0; i < len(s); i++ {
		h = (h * 0x100000001B3) ^ uint64(s[i])
	}
	return uint32(h)
}

// RuleSeed combines the population seed with a rule's hash
// (boost::hash_combine).
func RuleSeed(pop, hash uint32) uint32 {
	return pop ^ ((pop >> 2) + (pop << 6) + hash + 0x9E3779B9)
}

// rawFilter is the object form of a biome filter.
type rawFilter struct {
	AnyOf    []json.RawMessage `json:"any_of"`
	AllOf    []json.RawMessage `json:"all_of"`
	NoneOf   []json.RawMessage `json:"none_of"`
	Test     string            `json:"test"`
	Operator string            `json:"operator"`
	Value    string            `json:"value"`
}

// FilterAccepts evaluates a biome filter against a biome's tags. A list is
// all_of; objects are any_of, all_of, none_of or a has_biome_tag test.
func FilterAccepts(raw json.RawMessage, tags []string) (bool, error) {
	if len(raw) == 0 || string(raw) == `""` {
		return false, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, e := range list {
			ok, err := FilterAccepts(e, tags)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	var obj rawFilter
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, fmt.Errorf("biome filter %s: %w", raw, err)
	}
	switch {
	case obj.AnyOf != nil:
		for _, e := range obj.AnyOf {
			ok, err := FilterAccepts(e, tags)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case obj.AllOf != nil:
		return FilterAccepts(mustList(obj.AllOf), tags)
	case obj.NoneOf != nil:
		for _, e := range obj.NoneOf {
			ok, err := FilterAccepts(e, tags)
			if err != nil || ok {
				return false, err
			}
		}
		return true, nil
	case obj.Test == "has_biome_tag":
		has := slices.Contains(tags, obj.Value)
		switch obj.Operator {
		case "", "==", "equals":
			return has, nil
		case "!=", "not":
			return !has, nil
		}
		return false, fmt.Errorf("biome filter operator %q", obj.Operator)
	}
	return false, fmt.Errorf("biome filter %s not supported", raw)
}

func mustList(l []json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(l)
	return b
}
