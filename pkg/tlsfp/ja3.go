// Package tlsfp считает JA3-отпечаток TLS-клиента.
//
// Отпечаток нужен, чтобы отличать браузер от библиотеки: заголовки клиент
// подделывает, а набор шифров, расширений и кривых — нет.
package tlsfp

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// Hello — разобранная часть ClientHello, нужная для отпечатка.
type Hello struct {
	Version    uint16
	Ciphers    []uint16
	Extensions []uint16
	Curves     []uint16
	Formats    []uint8
	ServerName string
}

// ErrShortHello — данных меньше, чем нужно для разбора.
var ErrShortHello = errors.New("clienthello: не хватает данных")

// ErrNotHello — запись не является ClientHello.
var ErrNotHello = errors.New("clienthello: это не ClientHello")

// ParseClientHello разбирает TLS-запись с ClientHello.
func ParseClientHello(b []byte) (Hello, error) {
	var h Hello
	if len(b) < 5+4+2+32+1 {
		return h, ErrShortHello
	}
	if b[0] != 0x16 {
		return h, ErrNotHello
	}
	if b[5] != 0x01 {
		return h, ErrNotHello
	}
	recLen := int(binary.BigEndian.Uint16(b[3:5]))
	if len(b) < 5+recLen {
		return h, ErrShortHello
	}

	p := b[9:]
	if len(p) < 2+32+1 {
		return h, ErrShortHello
	}
	h.Version = binary.BigEndian.Uint16(p[0:2])
	p = p[2+32:]

	sidLen := int(p[0])
	if len(p) < 1+sidLen+2 {
		return h, ErrShortHello
	}
	p = p[1+sidLen:]

	csLen := int(binary.BigEndian.Uint16(p[0:2]))
	p = p[2:]
	if len(p) < csLen+1 {
		return h, ErrShortHello
	}
	for i := 0; i+1 < csLen; i += 2 {
		h.Ciphers = append(h.Ciphers, binary.BigEndian.Uint16(p[i:i+2]))
	}
	p = p[csLen:]

	compLen := int(p[0])
	if len(p) < 1+compLen {
		return h, ErrShortHello
	}
	p = p[1+compLen:]
	if len(p) < 2 {
		return h, nil
	}

	extLen := int(binary.BigEndian.Uint16(p[0:2]))
	p = p[2:]
	if len(p) < extLen {
		return h, ErrShortHello
	}
	for len(p) >= 4 {
		typ := binary.BigEndian.Uint16(p[0:2])
		l := int(binary.BigEndian.Uint16(p[2:4]))
		if len(p) < 4+l {
			break
		}
		data := p[4 : 4+l]
		h.Extensions = append(h.Extensions, typ)
		switch typ {
		case 0: // server_name
			if len(data) >= 5 {
				nameLen := int(binary.BigEndian.Uint16(data[3:5]))
				if len(data) >= 5+nameLen {
					h.ServerName = string(data[5 : 5+nameLen])
				}
			}
		case 10: // supported_groups
			if len(data) >= 2 {
				n := int(binary.BigEndian.Uint16(data[0:2]))
				for i := 2; i+1 < 2+n && i+1 < len(data); i += 2 {
					h.Curves = append(h.Curves, binary.BigEndian.Uint16(data[i:i+2]))
				}
			}
		case 11: // ec_point_formats
			if len(data) >= 1 {
				n := int(data[0])
				for i := 1; i <= n && i < len(data); i++ {
					h.Formats = append(h.Formats, data[i])
				}
			}
		}
		p = p[4+l:]
	}
	return h, nil
}

// String собирает строку JA3 до хеширования. Формат фиксирован: пять полей
// через запятую, значения через дефис.
func (h Hello) String() string {
	ints := func(vals []uint16) string {
		parts := make([]string, 0, len(vals))
		for _, v := range vals {
			parts = append(parts, strconv.Itoa(int(v)))
		}
		return strings.Join(parts, "-")
	}
	bytes := func(vals []uint8) string {
		parts := make([]string, 0, len(vals))
		for _, v := range vals {
			parts = append(parts, strconv.Itoa(int(v)))
		}
		return strings.Join(parts, "-")
	}
	return strconv.Itoa(int(h.Version)) + "," + ints(h.Ciphers) + "," +
		ints(h.Extensions) + "," + ints(h.Curves) + "," + bytes(h.Formats)
}

// JA3 возвращает md5-хеш отпечатка.
func JA3(b []byte) (string, error) {
	h, err := ParseClientHello(b)
	if err != nil {
		return "", err
	}
	sum := md5.Sum([]byte(h.String()))
	return hex.EncodeToString(sum[:]), nil
}
