package tlsfp

// Сборка отпечатков в буфер: без strconv и без промежуточных строк.

// appendUint дописывает число в буфер.
func appendUint(dst []byte, v int) []byte {
	if v == 0 {
		return append(dst, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	return append(dst, tmp[i:]...)
}

// appendUintList дописывает числа через разделитель.
func appendUintList(dst []byte, vals []uint16, sep byte) []byte {
	for i, v := range vals {
		if i > 0 {
			dst = append(dst, sep)
		}
		dst = appendUint(dst, int(v))
	}
	return dst
}
