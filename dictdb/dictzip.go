// Package dictdb reads dictd-format dictionaries: a sorted .index of
// headwords and a .dict.dz body compressed for random access.
package dictdb

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// dictzip is a reader for the "dzip" variant of gzip used by dictd.
//
// A plain gzip file has to be decompressed from the beginning, which for a
// 33MB dictionary would mean either a slow lookup or an uncompressed copy on
// disk. dictzip avoids both: the body is deflated in fixed-size chunks with a
// full flush between them, so each chunk begins on a byte boundary with a
// reset history and can be inflated on its own. The chunk sizes live in the
// gzip FEXTRA field under the subfield id "RA", so a lookup seeks to the one
// chunk holding the definition and inflates about 58KB.
type dictzip struct {
	mu        sync.Mutex
	cache     map[int][]byte // recently inflated chunks, keyed by chunk index
	r         io.ReaderAt
	chunkLen  int
	offsets   []int64 // compressed start of each chunk, from dataStart
	sizes     []uint16
	dataStart int64
}

const (
	flagFHCRC = 1 << 1
	flagExtra = 1 << 2
	flagName  = 1 << 3
	flagComnt = 1 << 4
)

func newDictzip(r io.ReaderAt) (*dictzip, error) {
	// Read enough for the header; the RA field is bounded by XLEN's 16 bits.
	head := make([]byte, 1<<16+64)
	n, err := r.ReadAt(head, 0)
	if n < 12 {
		return nil, fmt.Errorf("dictzip: short file: %w", err)
	}
	head = head[:n]

	if head[0] != 0x1f || head[1] != 0x8b {
		return nil, fmt.Errorf("dictzip: not gzip")
	}
	if head[2] != 8 {
		return nil, fmt.Errorf("dictzip: unexpected compression method %d", head[2])
	}
	flg := head[3]
	if flg&flagExtra == 0 {
		return nil, fmt.Errorf("dictzip: no extra field; this is plain gzip, not dictzip")
	}

	p := 10
	xlen := int(binary.LittleEndian.Uint16(head[p:]))
	p += 2
	extra := head[p : p+xlen]
	p += xlen

	z := &dictzip{r: r}
	if err := z.parseRA(extra); err != nil {
		return nil, err
	}

	// The optional trailing header fields must be stepped over to find where
	// the deflate data actually begins.
	if flg&flagName != 0 {
		i := bytes.IndexByte(head[p:], 0)
		if i < 0 {
			return nil, fmt.Errorf("dictzip: unterminated file name")
		}
		p += i + 1
	}
	if flg&flagComnt != 0 {
		i := bytes.IndexByte(head[p:], 0)
		if i < 0 {
			return nil, fmt.Errorf("dictzip: unterminated comment")
		}
		p += i + 1
	}
	if flg&flagFHCRC != 0 {
		p += 2
	}
	z.dataStart = int64(p)

	// Turn the per-chunk compressed sizes into absolute offsets once.
	z.offsets = make([]int64, len(z.sizes))
	var off int64
	for i, s := range z.sizes {
		z.offsets[i] = off
		off += int64(s)
	}
	return z, nil
}

// parseRA locates the random-access subfield inside the gzip extra field.
func (z *dictzip) parseRA(extra []byte) error {
	for len(extra) >= 4 {
		si1, si2 := extra[0], extra[1]
		l := int(binary.LittleEndian.Uint16(extra[2:]))
		if len(extra) < 4+l {
			return fmt.Errorf("dictzip: truncated extra subfield")
		}
		body := extra[4 : 4+l]
		if si1 == 'R' && si2 == 'A' {
			if len(body) < 6 {
				return fmt.Errorf("dictzip: short RA subfield")
			}
			if v := binary.LittleEndian.Uint16(body); v != 1 {
				return fmt.Errorf("dictzip: unsupported RA version %d", v)
			}
			z.chunkLen = int(binary.LittleEndian.Uint16(body[2:]))
			count := int(binary.LittleEndian.Uint16(body[4:]))
			if len(body) < 6+2*count {
				return fmt.Errorf("dictzip: RA subfield holds %d chunks but is too short", count)
			}
			z.sizes = make([]uint16, count)
			for i := 0; i < count; i++ {
				z.sizes[i] = binary.LittleEndian.Uint16(body[6+2*i:])
			}
			if z.chunkLen == 0 {
				return fmt.Errorf("dictzip: zero chunk length")
			}
			return nil
		}
		extra = extra[4+l:]
	}
	return fmt.Errorf("dictzip: no RA subfield; not a dictzip file")
}

// get returns length bytes starting at the uncompressed offset off.
func (z *dictzip) get(off, length int64) ([]byte, error) {
	if off < 0 || length < 0 {
		return nil, fmt.Errorf("dictzip: negative offset or length")
	}
	first := int(off / int64(z.chunkLen))
	last := int((off + length - 1) / int64(z.chunkLen))
	if length == 0 {
		return nil, nil
	}
	if first >= len(z.sizes) {
		return nil, fmt.Errorf("dictzip: offset %d past end of data", off)
	}
	if last >= len(z.sizes) {
		last = len(z.sizes) - 1
	}

	out := make([]byte, 0, int64(last-first+1)*int64(z.chunkLen))
	for i := first; i <= last; i++ {
		chunk, err := z.inflateChunk(i)
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}

	start := off - int64(first)*int64(z.chunkLen)
	if start > int64(len(out)) {
		return nil, fmt.Errorf("dictzip: offset %d beyond inflated data", off)
	}
	end := start + length
	if end > int64(len(out)) {
		end = int64(len(out))
	}
	return out[start:end], nil
}

// inflateChunk decompresses one chunk, remembering the result. Successive
// lookups land in the same region often enough -- adjacent headwords share a
// chunk -- that a small cache removes most of the work.
func (z *dictzip) inflateChunk(i int) ([]byte, error) {
	z.mu.Lock()
	if b, ok := z.cache[i]; ok {
		z.mu.Unlock()
		return b, nil
	}
	z.mu.Unlock()

	comp := make([]byte, z.sizes[i])
	if _, err := z.r.ReadAt(comp, z.dataStart+z.offsets[i]); err != nil && err != io.EOF {
		return nil, fmt.Errorf("dictzip: reading chunk %d: %w", i, err)
	}
	fr := flate.NewReader(bytes.NewReader(comp))
	defer fr.Close()
	out := make([]byte, z.chunkLen)
	n, err := io.ReadFull(fr, out)
	// The final chunk is short, and a chunk ends without a terminating block,
	// so an unexpected EOF here is normal rather than corruption.
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("dictzip: inflating chunk %d: %w", i, err)
	}
	out = out[:n]

	z.mu.Lock()
	// Bounded so a scan through the whole dictionary cannot grow without end.
	if len(z.cache) >= 64 {
		z.cache = nil
	}
	if z.cache == nil {
		z.cache = make(map[int][]byte, 8)
	}
	z.cache[i] = out
	z.mu.Unlock()

	return out, nil
}
