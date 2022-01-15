package rtmp

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/flv"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/rtmp/core"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type GetInFo interface {
	GetInfo() (string, string, string)
}

type StreamReadWriteCloser interface {
	GetInFo
	Close() error
	Write(core.ChunkStream) error
	Read(c *core.ChunkStream) error
}

type StaticsBW struct {
	VideoDataInBytes uint64
	AudioDataInBytes uint64
}

type VirWriter struct {
	Uid    string
	closed bool
	av.RWBaser
	conn        StreamReadWriteCloser
	packetQueue chan *av.Packet
}

func NewVirWriter(conn StreamReadWriteCloser) *VirWriter {
	id, _ := uuid.NewRandom()
	ret := &VirWriter{
		Uid:         id.String(),
		conn:        conn,
		RWBaser:     av.NewRWBaser(writeTimeout),
		packetQueue: make(chan *av.Packet, maxQueueNum),
	}

	go ret.Check()
	go func() {
		if err := ret.SendPacket(); err != nil {
			logrus.Warnf("rtmp send packet, err=%v", err)
		}
	}()
	return ret
}

func (v *VirWriter) Check() {
	var c core.ChunkStream
	for {
		if err := v.conn.Read(&c); err != nil {
			v.Close(err)
			return
		}
	}
}

func (v *VirWriter) DropPacket(pktQue chan *av.Packet, info av.Info) {
	logrus.Warningf("[%v] packet queue max!!!", info)
	for i := 0; i < maxQueueNum-84; i++ {
		tmpPkt, ok := <-pktQue
		// try to don't drop audio
		if ok && tmpPkt.IsAudio {
			if len(pktQue) > maxQueueNum-2 {
				logrus.Debug("drop audio pkt")
				<-pktQue
			} else {
				pktQue <- tmpPkt
			}

		}

		if ok && tmpPkt.IsVideo {
			videoPkt, ok := tmpPkt.Header.(av.VideoPacketHeader)
			// dont't drop sps config and dont't drop key frame
			if ok && (videoPkt.IsSeq() || videoPkt.IsKeyFrame()) {
				pktQue <- tmpPkt
			}
			if len(pktQue) > maxQueueNum-10 {
				logrus.Debug("drop video pkt")
				<-pktQue
			}
		}
	}
	logrus.Debug("packet queue len: ", len(pktQue))
}

func (v *VirWriter) Write(p *av.Packet) (err error) {
	err = nil

	if v.closed {
		err = fmt.Errorf("VirWriter closed")
		return
	}
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("VirWriter has already been closed:%v", e)
		}
	}()
	if len(v.packetQueue) >= maxQueueNum-24 {
		v.DropPacket(v.packetQueue, v.Info())
	} else {
		v.packetQueue <- p
	}

	return
}

func (v *VirWriter) SendPacket() error {
	Flush := reflect.ValueOf(v.conn).MethodByName("Flush")
	var cs core.ChunkStream
	for {
		p, ok := <-v.packetQueue
		if ok {
			cs.Data = p.Data
			cs.Length = uint32(len(p.Data))
			cs.StreamID = p.StreamID
			cs.Timestamp = p.TimeStamp + v.BaseTimeStamp()

			if p.IsVideo {
				cs.TypeID = av.TAG_VIDEO
			} else {
				if p.IsMetadata {
					cs.TypeID = av.TAG_SCRIPTDATAAMF0
				} else {
					cs.TypeID = av.TAG_AUDIO
				}
			}
			// logrus.Debug("send packet")
			v.SetPreTime()
			v.RecTimeStamp(cs.Timestamp, cs.TypeID)
			err := v.conn.Write(cs)
			if err != nil {
				v.closed = true
				return err
			}
			Flush.Call(nil)
		} else {
			return nil
		}

	}
}

func (v *VirWriter) Info() (ret av.Info) {
	ret.UID = v.Uid
	_, _, URL := v.conn.GetInfo()
	ret.URL = URL
	_url, err := url.Parse(URL)
	if err != nil {
		logrus.Warning(err)
	}
	ret.Key = strings.TrimLeft(_url.Path, "/")
	ret.Inter = true
	return
}

func (v *VirWriter) Close(err error) {
	if !v.closed {
		close(v.packetQueue)
	}
	v.closed = true
	v.conn.Close()
}

type VirReader struct {
	Uid string
	Key string
	av.RWBaser
	demuxer    *flv.Demuxer
	conn       StreamReadWriteCloser
	ReadBWInfo StaticsBW
}

func NewVirReader(conn StreamReadWriteCloser, id string, key string) *VirReader {
	return &VirReader{
		Uid:        id,
		Key:        key,
		conn:       conn,
		RWBaser:    av.NewRWBaser(writeTimeout),
		demuxer:    flv.NewDemuxer(),
		ReadBWInfo: StaticsBW{},
	}
}

func (v *VirReader) SaveStatics(length uint64, isVideoFlag bool) {
	if isVideoFlag {
		v.ReadBWInfo.VideoDataInBytes += length
	} else {
		v.ReadBWInfo.AudioDataInBytes += length
	}
}

func (v *VirReader) Read(p *av.Packet) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Warning("rtmp read packet panic: ", r)
		}
	}()

	v.SetPreTime()
	var cs core.ChunkStream
	for {
		err = v.conn.Read(&cs)
		if err != nil {
			return err
		}
		if cs.TypeID == av.TAG_AUDIO ||
			cs.TypeID == av.TAG_VIDEO ||
			cs.TypeID == av.TAG_SCRIPTDATAAMF0 ||
			cs.TypeID == av.TAG_SCRIPTDATAAMF3 {
			break
		}
	}

	p.IsAudio = cs.TypeID == av.TAG_AUDIO
	p.IsVideo = cs.TypeID == av.TAG_VIDEO
	p.IsMetadata = cs.TypeID == av.TAG_SCRIPTDATAAMF0 || cs.TypeID == av.TAG_SCRIPTDATAAMF3
	p.StreamID = cs.StreamID
	p.Data = cs.Data
	p.TimeStamp = cs.Timestamp

	v.SaveStatics(uint64(len(p.Data)), p.IsVideo)
	return v.demuxer.DemuxH(p)
}

func (v *VirReader) Info() (ret av.Info) {
	ret.UID = v.Uid
	ret.Key = v.Key
	_, _, URL := v.conn.GetInfo()
	ret.URL = URL
	return
}

func (v *VirReader) Close(err error) {
	logrus.Debug("publisher ", v.Info(), "closed: "+err.Error())
	v.conn.Close()
}
