package blocks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// NBT tag types.
const (
	tagEnd       = 0
	tagByte      = 1
	tagShort     = 2
	tagInt       = 3
	tagLong      = 4
	tagFloat     = 5
	tagDouble    = 6
	tagByteArray = 7
	tagString    = 8
	tagList      = 9
	tagCompound  = 10
	tagIntArray  = 11
	tagLongArray = 12
)

// maxDepth bounds nesting so a hostile file cannot exhaust the stack.
const maxDepth = 64

// maxLen bounds any single length field (strings, lists, arrays).
const maxLen = 1 << 24

// nbtReader reads big-endian NBT, the format Endstone writes its palette in.
type nbtReader struct {
	r   io.Reader
	buf [8]byte
}

func (d *nbtReader) read(n int) ([]byte, error) {
	if _, err := io.ReadFull(d.r, d.buf[:n]); err != nil {
		return nil, err
	}
	return d.buf[:n], nil
}

func (d *nbtReader) u8() (uint8, error) {
	b, err := d.read(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (d *nbtReader) i16() (int16, error) {
	b, err := d.read(2)
	if err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(b)), nil
}

func (d *nbtReader) i32() (int32, error) {
	b, err := d.read(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b)), nil
}

func (d *nbtReader) i64() (int64, error) {
	b, err := d.read(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}

func (d *nbtReader) length() (int, error) {
	n, err := d.i32()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > maxLen {
		return 0, fmt.Errorf("nbt: bad length %d", n)
	}
	return int(n), nil
}

func (d *nbtReader) str() (string, error) {
	n, err := d.i16()
	if err != nil {
		return "", err
	}
	l := int(uint16(n))
	b := make([]byte, l)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

// root reads the unnamed root compound.
func (d *nbtReader) root() (map[string]any, error) {
	t, err := d.u8()
	if err != nil {
		return nil, fmt.Errorf("nbt: read root type: %w", err)
	}
	if t != tagCompound {
		return nil, fmt.Errorf("nbt: root is tag %d, not a compound", t)
	}
	if _, err := d.str(); err != nil {
		return nil, fmt.Errorf("nbt: read root name: %w", err)
	}
	v, err := d.payload(tagCompound, 0)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("nbt: root is not a compound")
	}
	return m, nil
}

// payload reads one value. Bytes come back as uint8, ints as int32 and
// strings as string, the types block properties use.
func (d *nbtReader) payload(t uint8, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("nbt: nesting too deep")
	}
	switch t {
	case tagByte:
		return d.u8()
	case tagShort:
		return d.i16()
	case tagInt:
		return d.i32()
	case tagLong:
		return d.i64()
	case tagFloat:
		v, err := d.i32()
		return math.Float32frombits(uint32(v)), err
	case tagDouble:
		v, err := d.i64()
		return math.Float64frombits(uint64(v)), err
	case tagString:
		return d.str()
	case tagByteArray:
		n, err := d.length()
		if err != nil {
			return nil, err
		}
		b := make([]byte, n)
		_, err = io.ReadFull(d.r, b)
		return b, err
	case tagIntArray, tagLongArray:
		n, err := d.length()
		if err != nil {
			return nil, err
		}
		size := 4
		if t == tagLongArray {
			size = 8
		}
		_, err = io.CopyN(io.Discard, d.r, int64(n*size))
		return nil, err
	case tagList:
		et, err := d.u8()
		if err != nil {
			return nil, err
		}
		n, err := d.length()
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, min(n, 1<<16))
		for range n {
			v, err := d.payload(et, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case tagCompound:
		m := map[string]any{}
		for {
			ct, err := d.u8()
			if err != nil {
				return nil, err
			}
			if ct == tagEnd {
				return m, nil
			}
			name, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.payload(ct, depth+1)
			if err != nil {
				return nil, fmt.Errorf("nbt: read %q: %w", name, err)
			}
			m[name] = v
		}
	}
	return nil, fmt.Errorf("nbt: unknown tag type %d", t)
}

// ReadNBT reads a big-endian NBT root compound (Java Edition files such as
// structure templates). Bytes come back as uint8, shorts as int16, ints as
// int32, longs as int64, floats as float32, doubles as float64, strings as
// string, lists as []any and compounds as map[string]any; int and long
// arrays are skipped (nil).
func ReadNBT(r io.Reader) (map[string]any, error) {
	return (&nbtReader{r: r}).root()
}
