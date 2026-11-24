package main

import (
	"encoding/binary"
	"errors"
	"net"
	"syscall"
	"unsafe"
)

const soOriginalDst = 80 // SO_ORIGINAL_DST (IPv4) and IP6T_SO_ORIGINAL_DST (IPv6)

// originalDst returns where a REDIRECTed connection was really headed.
func originalDst(c *net.TCPConn) (net.IP, int, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return nil, 0, err
	}
	var ip net.IP
	var port int
	var serr error
	err = rc.Control(func(fd uintptr) {
		var a syscall.RawSockaddrInet4
		n := uint32(unsafe.Sizeof(a))
		_, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_IP, soOriginalDst,
			uintptr(unsafe.Pointer(&a)), uintptr(unsafe.Pointer(&n)), 0)
		if e != 0 {
			serr = e
			return
		}
		ip = net.IPv4(a.Addr[0], a.Addr[1], a.Addr[2], a.Addr[3])
		port = int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&a.Port))[:]))
	})
	if err != nil {
		return nil, 0, err
	}
	if serr != nil {
		return nil, 0, serr
	}
	if ip == nil || port == 0 {
		return nil, 0, errors.New("no original destination")
	}
	return ip, port, nil
}
