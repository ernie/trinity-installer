package steam

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	vdfMap    = 0x00
	vdfString = 0x01
	vdfInt32  = 0x02
	vdfEnd    = 0x08
)

// ParseBinaryVDF decodes Steam's binary key-value format into nested maps.
func ParseBinaryVDF(b []byte) (map[string]any, error) {
	m, rest, err := parseMap(b)
	if err != nil {
		return nil, err
	}
	_ = rest
	return m, nil
}

func parseMap(b []byte) (map[string]any, []byte, error) {
	m := map[string]any{}
	for {
		if len(b) == 0 {
			return m, b, nil
		}
		typ := b[0]
		b = b[1:]
		if typ == vdfEnd {
			return m, b, nil
		}
		key, rest, err := cstring(b)
		if err != nil {
			return nil, nil, err
		}
		b = rest
		switch typ {
		case vdfMap:
			child, rest, err := parseMap(b)
			if err != nil {
				return nil, nil, err
			}
			m[key] = child
			b = rest
		case vdfString:
			val, rest, err := cstring(b)
			if err != nil {
				return nil, nil, err
			}
			m[key] = val
			b = rest
		case vdfInt32:
			if len(b) < 4 {
				return nil, nil, errors.New("truncated int32")
			}
			m[key] = int32(binary.LittleEndian.Uint32(b))
			b = b[4:]
		default:
			return nil, nil, fmt.Errorf("unknown VDF type 0x%02x for %q", typ, key)
		}
	}
}

func cstring(b []byte) (string, []byte, error) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return "", nil, errors.New("unterminated string")
	}
	return string(b[:i]), b[i+1:], nil
}
