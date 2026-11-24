package main

// findSNI reads the host name from a TLS ClientHello record (5-byte record
// header + handshake). off is the offset of the first host name byte inside rec.
func findSNI(rec []byte) (host string, off int, ok bool) {
	u16 := func(p int) int { return int(rec[p])<<8 | int(rec[p+1]) }
	if len(rec) < 5+4+2+32+1 || rec[0] != 0x16 || rec[5] != 0x01 {
		return "", 0, false
	}
	p := 5 + 4 + 2 + 32  // record hdr, handshake hdr, version, random
	p += 1 + int(rec[p]) // session id
	if p+2 > len(rec) {
		return "", 0, false
	}
	p += 2 + u16(p) // cipher suites
	if p+1 > len(rec) {
		return "", 0, false
	}
	p += 1 + int(rec[p]) // compression methods
	if p+2 > len(rec) {
		return "", 0, false
	}
	end := p + 2 + u16(p)
	p += 2
	if end > len(rec) {
		end = len(rec)
	}
	for p+4 <= end {
		typ, l := u16(p), u16(p+2)
		p += 4
		if p+l > end {
			return "", 0, false
		}
		if typ == 0 { // server_name: list length(2) | type(1) | name length(2) | name
			if l < 5 || rec[p+2] != 0 {
				return "", 0, false
			}
			n := u16(p + 3)
			if 5+n > l {
				return "", 0, false
			}
			return string(rec[p+5 : p+5+n]), p + 5, true
		}
		p += l
	}
	return "", 0, false
}
