package tlsfp

import (
	"crypto/sha256"
	"encoding/hex"
)

// JA4 — отпечаток по формату FoxIO: устойчивее JA3, потому что сортирует
// списки, вырезает GREASE и учитывает ALPN.
//
// Вид: t13d1516h2_8daaf6152771_02713d6af862
//
//	t — TLS, 13 — версия, d — SNI с доменом, 15 — шифров, 16 — расширений,
//	h2 — ALPN. Дальше два sha256-префикса: по шифрам и по расширениям с кривыми.
func (h Hello) JA4() string {
	// Списки сортируются и очищаются от GREASE: в буферах на стеке, без
	// промежуточных срезов и строк.
	ciphers := h.sortedNoGREASE16(h.Ciphers)
	exts := h.sortedNoGREASE16(h.Extensions)
	curves := h.sortedNoGREASE16(h.Curves)

	var cbuf [1024]byte
	var ebuf [1024]byte
	cs := appendUintList(cbuf[:0], ciphers, ',')
	es := appendUintList(ebuf[:0], exts, ',')
	es = append(es, '_')
	es = appendUintList(es, curves, ',')

	cSum := sha256.Sum256(cs)
	eSum := sha256.Sum256(es)

	var out [64]byte
	b := out[:0]
	b = append(b, 't')
	version := "12"
	for _, e := range h.Extensions {
		if e == 43 && h.supportedVersion == 0x0304 {
			version = "13"
		}
	}
	b = append(b, version...)
	if h.ServerName != "" {
		b = append(b, 'd')
	} else {
		b = append(b, 'i')
	}
	b = appendUint(b, len(ciphers))
	b = appendUint(b, len(exts))
	if len(h.ALPN) > 0 {
		a := h.ALPN[0]
		if len(a) > 2 {
			a = a[:2]
		}
		b = append(b, a...)
	} else {
		b = append(b, '0', '0')
	}
	b = append(b, '_')
	b = hex.AppendEncode(b, cSum[:6])
	b = append(b, '_')
	b = hex.AppendEncode(b, eSum[:6])
	return string(b)
}

// sortedNoGREASE16 копирует значения, вырезает GREASE и сортирует.
func (h Hello) sortedNoGREASE16(vals []uint16) []uint16 {
	out := make([]uint16, 0, len(vals))
	for _, v := range vals {
		if !isGREASE(v) {
			out = append(out, v)
		}
	}
	// Сортировка вставками: список короткий, а sort.Slice тянет reflect
	// и аллоцирует обёртку на каждый вызов.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// isGREASE — значения 0x0a0a, 0x1a1a, ... 0xfafa, которые клиенты вставляют
// намеренно, чтобы сломать наивные парсеры. В отпечаток они не входят.
func isGREASE(v uint16) bool {
	return v&0x0f0f == 0x0a0a && byte(v>>8) == byte(v)
}
