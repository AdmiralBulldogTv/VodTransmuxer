package core

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"time"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/utils"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/utils/pio"
	"github.com/sirupsen/logrus"
)

var (
	timeout = 3 * time.Second
)

var (
	hsClientFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'P', 'l', 'a', 'y', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsServerFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'M', 'e', 'd', 'i', 'a', ' ',
		'S', 'e', 'r', 'v', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsClientPartialKey = hsClientFullKey[:30]
	hsServerPartialKey = hsServerFullKey[:36]
)

func hsParse1(p []byte, peerkey []byte, key []byte) (ok bool, digest []byte) {
	var pos int
	if pos = hsFindDigest(p, peerkey, 772); pos == -1 {
		if pos = hsFindDigest(p, peerkey, 8); pos == -1 {
			return
		}
	}
	ok = true
	digest = hsMakeDigest(key, p[pos:pos+32], -1)
	return
}

func hsMakeDigest(key []byte, src []byte, gap int) (dst []byte) {
	h := hmac.New(sha256.New, key)
	if gap <= 0 {
		h.Write(src)
	} else {
		h.Write(src[:gap])
		h.Write(src[gap+32:])
	}
	return h.Sum(nil)
}

func hsCalcDigestPos(p []byte, base int) (pos int) {
	for i := 0; i < 4; i++ {
		pos += int(p[base+i])
	}
	pos = (pos % 728) + base + 4
	return
}

func hsFindDigest(p []byte, key []byte, base int) int {
	gap := hsCalcDigestPos(p, base)
	digest := hsMakeDigest(key, p, gap)
	if !bytes.Equal(p[gap:gap+32], digest) {
		return -1
	}
	return gap
}

func hsCreate01(p []byte, time uint32, ver uint32, key []byte) error {
	p[0] = 3
	p1 := p[1:]
	_, err := rand.Read(p1[8:])
	if err != nil {
		return err
	}
	pio.PutU32BE(p1[0:4], time)
	pio.PutU32BE(p1[4:8], ver)
	gap := hsCalcDigestPos(p1, 8)
	digest := hsMakeDigest(key, p1, gap)
	copy(p1[gap:], digest)
	return nil
}

func hsCreate2(p []byte, key []byte) error {
	_, err := rand.Read(p)
	if err != nil {
		return err
	}
	gap := len(p) - 32
	digest := hsMakeDigest(key, p, gap)
	copy(p[gap:], digest)
	return nil
}

func (conn *Conn) HandshakeServer() error {
	C0C1C2 := make([]byte, 1536*2+1)
	C0 := C0C1C2[:1]
	C1 := C0C1C2[1 : 1536+1]
	C0C1 := C0C1C2[:1536+1]
	C2 := C0C1C2[1536+1:]

	S0S1S2, err := utils.GenerateRandomBytes(1536*2 + 1)
	if err != nil {
		return err
	}
	S0 := S0S1S2[:1]
	S1 := S0S1S2[1 : 1536+1]
	S0S1 := S0S1S2[:1536+1]
	S2 := S0S1S2[1536+1:]

	err = conn.Conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}
	if _, err := io.ReadFull(conn.rw, C0C1); err != nil {
		return err
	}

	err = conn.Conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}
	if C0[0] != 3 {
		logrus.Warnf("rtmp: handshake version=%d invalid", C0[0])
	}
	cliver := pio.U32BE(C1[4:8])
	if cliver != 0 {
		var ok bool
		var digest []byte
		if ok, digest = hsParse1(C1, hsClientPartialKey, hsServerFullKey); !ok {
			return fmt.Errorf("rtmp: handshake server: C1 invalid")
		}
		err = hsCreate01(S0S1, pio.U32BE(C1[0:4]), 0, hsServerPartialKey)
		if err != nil {
			return err
		}
		err = hsCreate2(S2, digest)
		if err != nil {
			return err
		}
	} else {
		copy(S1[:4], C1[:4])
		copy(S1[4:8], C1[4:8])

		copy(S2[:4], C1[:4])
		copy(S2[4:8], C1[4:8])
		copy(S2[8:], C1[8:])
	}

	S0[0] = 3

	err = conn.Conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}
	if _, err := conn.rw.Write(S0S1S2); err != nil {
		return err
	}

	err = conn.Conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}
	if err = conn.rw.Flush(); err != nil {
		return err
	}

	err = conn.Conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}
	if _, err := io.ReadFull(conn.rw, C2); err != nil {
		return err
	}

	if !bytes.Equal(S1[:4], C2[:4]) {
		return fmt.Errorf("invalid c2 bad time")
	}

	if !bytes.Equal(S1[8:], C2[8:]) {
		return fmt.Errorf("invalid c2 bad random")
	}

	return conn.Conn.SetDeadline(time.Time{})
}
