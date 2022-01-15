package cache

import (
	"bytes"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/amf"
	"github.com/sirupsen/logrus"
)

const (
	SetDataFrame string = "@setDataFrame"
	OnMetaData   string = "onMetaData"
)

func init() {
	b := bytes.NewBuffer(nil)
	encoder := &amf.Encoder{}
	if _, err := encoder.Encode(b, SetDataFrame, amf.AMF0); err != nil {
		logrus.Fatal(err)
	}
}

type SpecialCache struct {
	full bool
	p    *av.Packet
}

func NewSpecialCache() *SpecialCache {
	return &SpecialCache{}
}

func (specialCache *SpecialCache) Write(p *av.Packet) {
	specialCache.p = p
	specialCache.full = true
}

func (specialCache *SpecialCache) Send(w av.WriteCloser) error {
	if !specialCache.full {
		return nil
	}
	return w.Write(specialCache.p)
}
