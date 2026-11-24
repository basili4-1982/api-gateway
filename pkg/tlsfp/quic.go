package tlsfp

// Отпечаток в QUIC.
//
// В HTTP/3 нет TLS-записей: ClientHello едет в CRYPTO-фреймах внутри Initial-
// пакета, который защищён header protection и AEAD. Ключи Initial выводятся из
// DCID и фиксированной соли — то есть расшифровать пакет может любой, кто его
// увидел. Этим и пользуемся: снимаем защиту, читаем ClientHello, считаем JA4.
//
// Пакет при этом не меняется: разбор идёт на копии, оригинал уходит дальше.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// initialSalt — соль из RFC 9001 §5.2. Все QUIC-клиенты используют её.
var initialSalt = []byte{
	0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
	0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a,
}

// ErrNotQUIC — пакет не похож на QUIC Initial.
var ErrNotQUIC = errors.New("quic: не Initial-пакет")

// ErrIncomplete — ClientHello ещё не собран: он может занимать несколько пакетов.
var ErrIncomplete = errors.New("quic: ClientHello не собран")

// hkdfExpandLabel — HKDF-Expand-Label из TLS 1.3 с пустым контекстом.
func hkdfExpandLabel(secret []byte, label string, length int) ([]byte, error) {
	full := "tls13 " + label
	info := make([]byte, 0, 4+len(full))
	info = append(info, byte(length>>8), byte(length))
	info = append(info, byte(len(full)))
	info = append(info, full...)
	info = append(info, 0) // пустой контекст
	return hkdf.Expand(sha256.New, secret, string(info), length)
}

// keyID — ключ кеша: DCID длиной до 20 байт, без выделения строки.
type keyID struct {
	n uint8
	b [20]byte
}

// InitialKeys — готовые ключи одного DCID: AEAD, iv и шифр для header protection.
type InitialKeys struct {
	aead cipher.AEAD
	iv   [12]byte
	hp   cipher.Block
}

// KeyCache хранит ключи по DCID. Вывод ключей — самая дорогая часть разбора
// (HKDF с четырьмя расширениями), а на соединение Initial-пакетов 1–2 с одним
// и тем же DCID.
type KeyCache struct {
	mu  sync.Mutex
	m   map[keyID]*InitialKeys
	max int
}

// NewKeyCache создаёт кеш на 4096 соединений.
func NewKeyCache() *KeyCache {
	return &KeyCache{m: make(map[keyID]*InitialKeys, 64), max: 4096}
}

func keyOf(dcid []byte) keyID {
	var id keyID
	if len(dcid) > len(id.b) {
		dcid = dcid[:len(id.b)]
	}
	id.n = uint8(len(dcid))
	copy(id.b[:], dcid)
	return id
}

// Get отдаёт ключи из кеша или выводит их и запоминает.
func (c *KeyCache) Get(dcid []byte) (*InitialKeys, error) {
	if c == nil {
		return deriveInitial(dcid)
	}
	id := keyOf(dcid)
	c.mu.Lock()
	k, ok := c.m[id]
	c.mu.Unlock()
	if ok {
		return k, nil
	}
	k, err := deriveInitial(dcid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.m) >= c.max {
		for key := range c.m { // простое вытеснение: чистим один
			delete(c.m, key)
			break
		}
	}
	c.m[id] = k
	c.mu.Unlock()
	return k, nil
}

// deriveInitial выводит ключи и собирает из них AEAD и шифр.
func deriveInitial(dcid []byte) (*InitialKeys, error) {
	key, iv, hp, err := initialKeys(dcid)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	hpBlock, err := aes.NewCipher(hp)
	if err != nil {
		return nil, err
	}
	k := &InitialKeys{aead: aead, hp: hpBlock}
	copy(k.iv[:], iv)
	return k, nil
}

// initialKeys выводит ключи Initial для направления клиента.
func initialKeys(dcid []byte) (key, iv, hp []byte, err error) {
	secret, err := hkdf.Extract(sha256.New, dcid, initialSalt)
	if err != nil {
		return nil, nil, nil, err
	}
	clientSecret, err := hkdfExpandLabel(secret, "client in", 32)
	if err != nil {
		return nil, nil, nil, err
	}
	if key, err = hkdfExpandLabel(clientSecret, "quic key", 16); err != nil {
		return nil, nil, nil, err
	}
	if iv, err = hkdfExpandLabel(clientSecret, "quic iv", 12); err != nil {
		return nil, nil, nil, err
	}
	if hp, err = hkdfExpandLabel(clientSecret, "quic hp", 16); err != nil {
		return nil, nil, nil, err
	}
	return key, iv, hp, nil
}

// readVarint читает QUIC varint (RFC 9000 §16). Возвращает значение и новое смещение.
func readVarint(b []byte, off int) (uint64, int, error) {
	if off >= len(b) {
		return 0, off, ErrNotQUIC
	}
	prefix := b[off] >> 6
	size := 1 << prefix
	if off+size > len(b) {
		return 0, off, ErrNotQUIC
	}
	var v uint64
	for i := 0; i < size; i++ {
		v = v<<8 | uint64(b[off+i])
	}
	// Верхние два бита — префикс длины, остальное значение.
	v &= (uint64(1) << (8*size - 2)) - 1
	return v, off + size, nil
}

// cryptoSegment — кусок ClientHello из CRYPTO-фрейма.
type cryptoSegment struct {
	offset uint64
	data   []byte
}

// ParseInitial расшифровывает Initial-пакет и возвращает CRYPTO-сегменты.
// Пакет не изменяется: защита снимается с копии заголовка.
func ParseInitial(pkt []byte) ([]byte, []cryptoSegment, error) {
	return ParseInitialCached(pkt, nil)
}

// ParseInitialCached — то же, но ключи берутся из кеша по DCID.
func ParseInitialCached(pkt []byte, cache *KeyCache) (dcid []byte, segments []cryptoSegment, err error) {
	if len(pkt) < 7 || pkt[0]&0x80 == 0 {
		return nil, nil, ErrNotQUIC
	}
	// Длинный заголовок: 1 байт флагов, 4 версии, DCID, SCID, токен, длина.
	pktType := (pkt[0] >> 4) & 0x03
	if pktType != 0 { // 0 — Initial
		return nil, nil, ErrNotQUIC
	}
	version := binary.BigEndian.Uint32(pkt[1:5])
	_ = version
	off := 5
	dcidLen := int(pkt[off])
	off++
	if dcidLen > 20 || off+dcidLen > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	dcid = append([]byte(nil), pkt[off:off+dcidLen]...)
	off += dcidLen
	if off >= len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	scidLen := int(pkt[off])
	off++
	if off+scidLen > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	off += scidLen
	tokenLen, off, err := readVarint(pkt, off)
	if err != nil || off+int(tokenLen) > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	off += int(tokenLen)
	length, off, err := readVarint(pkt, off)
	if err != nil || off+int(length) > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	pnOffset := off
	// Сэмпл для снятия защиты берётся с четвёртого байта после начала PN.
	if pnOffset+4+16 > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	keys, err := cache.Get(dcid)
	if err != nil {
		return nil, nil, err
	}
	var maskBuf [16]byte
	mask := maskBuf[:]
	keys.hp.Encrypt(mask, pkt[pnOffset+4:pnOffset+20])

	first := pkt[0] ^ (mask[0] & 0x0f)
	pnLen := int(first&0x03) + 1
	if pnOffset+pnLen > len(pkt) {
		return nil, nil, ErrNotQUIC
	}
	header := append([]byte(nil), pkt[:pnOffset+pnLen]...)
	header[0] = first
	var pn uint64
	for i := 0; i < pnLen; i++ {
		header[pnOffset+i] = pkt[pnOffset+i] ^ mask[1+i]
		pn = pn<<8 | uint64(header[pnOffset+i])
	}

	var nonce [12]byte
	copy(nonce[:], keys.iv[:])
	for i := 0; i < 8; i++ {
		nonce[len(nonce)-1-i] ^= byte(pn >> (8 * i))
	}
	end := pnOffset + int(length)
	if end > len(pkt) || end < pnOffset+pnLen+16 {
		return nil, nil, ErrNotQUIC
	}
	payload := pkt[pnOffset+pnLen : end]
	plain, err := keys.aead.Open(nil, nonce[:], payload, header)
	if err != nil {
		return nil, nil, fmt.Errorf("quic: расшифровка: %w", err)
	}

	segments, err = parseFrames(plain)
	if err != nil {
		return nil, nil, err
	}
	return dcid, segments, nil
}

// parseFrames читает CRYPTO-фреймы; остальное пропускает.
func parseFrames(b []byte) ([]cryptoSegment, error) {
	var out []cryptoSegment
	off := 0
	for off < len(b) {
		typ, next, err := readVarint(b, off)
		if err != nil {
			return out, nil // хвост не разобрали — не беда, ClientHello мог уже прийти
		}
		off = next
		switch typ {
		case 0x00: // PADDING
			continue
		case 0x01: // PING
			continue
		case 0x06: // CRYPTO
			frameOff, n, err := readVarint(b, off)
			if err != nil {
				return out, nil
			}
			frameLen, n2, err := readVarint(b, n)
			if err != nil || n2+int(frameLen) > len(b) {
				return out, nil
			}
			out = append(out, cryptoSegment{offset: frameOff, data: b[n2 : n2+int(frameLen)]})
			off = n2 + int(frameLen)
		default:
			// ACK, CONNECTION_CLOSE и прочее: длину без парсера не узнать.
			return out, nil
		}
	}
	return out, nil
}

// AssembleHello собирает ClientHello из сегментов и возвращает TLS-запись,
// пригодную для ParseClientHello.
func AssembleHello(segments []cryptoSegment) ([]byte, error) {
	if len(segments) == 0 {
		return nil, ErrIncomplete
	}
	// Сортируем по смещению: пакеты могут прийти не по порядку.
	ordered := append([]cryptoSegment(nil), segments...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].offset < ordered[j-1].offset; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	var buf []byte
	var want uint64
	for _, s := range ordered {
		if s.offset != want {
			return nil, ErrIncomplete
		}
		buf = append(buf, s.data...)
		want += uint64(len(s.data))
	}
	// В CRYPTO лежит handshake-сообщение: тип 1 и 3 байта длины.
	if len(buf) < 4 || buf[0] != 0x01 {
		return nil, ErrNotQUIC
	}
	hsLen := int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
	if len(buf) < 4+hsLen {
		return nil, ErrIncomplete
	}
	hs := buf[:4+hsLen]

	// ParseClientHello ждёт TLS-запись: добавляем фиктивный заголовок записи.
	rec := make([]byte, 0, 5+len(hs))
	rec = append(rec, 0x16, 0x03, 0x01)
	rec = append(rec, byte(len(hs)>>8), byte(len(hs)))
	rec = append(rec, hs...)
	return rec, nil
}
