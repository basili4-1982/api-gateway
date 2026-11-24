package tlsfp

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// JA4 — отпечаток по формату FoxIO: устойчивее JA3, потому что сортирует
// списки, вырезает GREASE и учитывает ALPN.
//
// Вид: t13d1516h2_8daaf6152771_02713d6af862
//
//	t — TLS, 13 — версия, d — SNI с доменом, 15 — шифров, 16 — расширений,
//	h2 — ALPN. Дальше два sha256-префикса: по шифрам и по расширениям с кривыми.
func (h Hello) JA4() string {
	ciphers := make([]int, 0, len(h.Ciphers))
	for _, c := range h.Ciphers {
		if !isGREASE(c) {
			ciphers = append(ciphers, int(c))
		}
	}
	exts := make([]int, 0, len(h.Extensions))
	for _, e := range h.Extensions {
		if !isGREASE(e) {
			exts = append(exts, int(e))
		}
	}
	sort.Ints(ciphers)
	sort.Ints(exts)

	// Версия: сначала supported_versions (0x0304 — TLS 1.3), иначе legacy.
	version := "12"
	for _, e := range h.Extensions {
		if e == 43 && h.supportedVersion == 0x0304 {
			version = "13"
		}
	}

	sni := "i" // без SNI
	if h.ServerName != "" {
		sni = "d"
	}

	alpn := "00"
	if len(h.ALPN) > 0 {
		alpn = h.ALPN[0]
		if len(alpn) > 2 {
			alpn = alpn[:2]
		}
	}

	prefix := "t" + version + sni +
		strconv.Itoa(len(ciphers)) + strconv.Itoa(len(exts)) + alpn

	cipherPart := sha256Hex(joinInts(ciphers, ","))
	// Расширения в JA4 идут вместе с кривыми: так отпечаток различает
	// поддержку групп, не дублируя отдельное поле.
	extPart := sha256Hex(joinInts(exts, ",") + "_" + joinInts(h.sortedCurves(), ","))

	return prefix + "_" + cipherPart + "_" + extPart
}

func (h Hello) sortedCurves() []int {
	out := make([]int, 0, len(h.Curves))
	for _, c := range h.Curves {
		if !isGREASE(c) {
			out = append(out, int(c))
		}
	}
	sort.Ints(out)
	return out
}

// isGREASE — значения 0x0a0a, 0x1a1a, ... 0xfafa, которые клиенты вставляют
// намеренно, чтобы сломать наивные парсеры. В отпечаток они не входят.
func isGREASE(v uint16) bool {
	return v&0x0f0f == 0x0a0a && byte(v>>8) == byte(v)
}

func joinInts(vals []int, sep string) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, sep)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
