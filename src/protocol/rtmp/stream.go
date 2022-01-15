package rtmp

import (
	"fmt"
	"sync"
	"time"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/rtmp/cache"
	"github.com/sirupsen/logrus"
)

var (
	EmptyID = ""
)

type RtmpStream struct {
	streams *sync.Map
}

func NewRtmpStream() *RtmpStream {
	ret := &RtmpStream{
		streams: &sync.Map{},
	}
	go ret.CheckAlive()
	return ret
}

func (rs *RtmpStream) HandleReader(r av.ReadCloser) {
	info := r.Info()
	logrus.Debugf("HandleReader: info[%v]", info)

	var stream *Stream
	var ok bool
	i, _ := rs.streams.Load(info.Key)
	if stream, ok = i.(*Stream); ok {
		stream.TransStop()
		id := stream.ID()
		if id != EmptyID && id != info.UID {
			ns := NewStream()
			stream.Copy(ns)
			stream = ns
			rs.streams.Store(info.Key, ns)
		}
	} else {
		stream = NewStream()
		rs.streams.Store(info.Key, stream)
		stream.info = info
		logrus.Info("new: ", info.Key)
	}

	stream.AddReader(r)
}

func (rs *RtmpStream) HandleWriter(w av.WriteCloser) {
	info := w.Info()
	logrus.Debugf("HandleWriter: info[%v]", info)

	var s *Stream
	item, ok := rs.streams.Load(info.Key)
	if !ok {
		logrus.Debugf("HandleWriter: not found create new info[%v]", info)
		s = NewStream()
		rs.streams.Store(info.Key, s)
		s.info = info
	} else {
		s = item.(*Stream)
		s.AddWriter(w)
	}
}

func (rs *RtmpStream) GetStreams() *sync.Map {
	return rs.streams
}

func (rs *RtmpStream) CheckAlive() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		<-tick.C
		rs.streams.Range(func(key, val interface{}) bool {
			v := val.(*Stream)
			if v.CheckAlive() == 0 {
				rs.streams.Delete(key)
			}
			return true
		})
	}
}

type Stream struct {
	isStart bool
	cache   *cache.Cache
	r       av.ReadCloser
	ws      *sync.Map
	info    av.Info
}

type PackWriterCloser struct {
	init bool
	w    av.WriteCloser
}

func (p *PackWriterCloser) GetWriter() av.WriteCloser {
	return p.w
}

func NewStream() *Stream {
	return &Stream{
		cache: cache.NewCache(),
		ws:    &sync.Map{},
	}
}

func (s *Stream) ID() string {
	if s.r != nil {
		return s.r.Info().UID
	}
	return EmptyID
}

func (s *Stream) GetReader() av.ReadCloser {
	return s.r
}

func (s *Stream) GetWs() *sync.Map {
	return s.ws
}

func (s *Stream) Copy(dst *Stream) {
	dst.info = s.info
	s.ws.Range(func(key, val interface{}) bool {
		v := val.(*PackWriterCloser)
		s.ws.Delete(key)
		v.w.CalcBaseTimestamp()
		dst.AddWriter(v.w)
		return true
	})
}

func (s *Stream) AddReader(r av.ReadCloser) {
	s.r = r
	go s.TransStart()
}

func (s *Stream) AddWriter(w av.WriteCloser) {
	info := w.Info()
	pw := &PackWriterCloser{w: w}
	s.ws.Store(info.UID, pw)
}

func (s *Stream) TransStart() {
	s.isStart = true
	var p av.Packet

	logrus.Debugf("TransStart: %v", s.info)

	for {
		if !s.isStart {
			s.closeInter()
			return
		}
		err := s.r.Read(&p)
		if err != nil {
			s.closeInter()
			s.isStart = false
			return
		}

		err = s.cache.Write(p)
		if err != nil {
			s.closeInter()
			s.isStart = false
			return
		}

		s.ws.Range(func(key, val interface{}) bool {
			v := val.(*PackWriterCloser)
			if !v.init {
				// logrus.Debugf("cache.send: %v", v.w.Info())
				if err = s.cache.Send(v.w); err != nil {
					logrus.Debugf("[%s] send cache packet error: %v, remove", v.w.Info(), err)
					s.ws.Delete(key)
					return true
				}
				v.init = true
			} else {
				newPacket := p
				// writeType := reflect.TypeOf(v.w)
				// logrus.Debugf("w.Write: type=%v, %v", writeType, v.w.Info())
				if err = v.w.Write(&newPacket); err != nil {
					logrus.Debugf("[%s] write packet error: %v, remove", v.w.Info(), err)
					s.ws.Delete(key)
				}
			}
			return true
		})
	}
}

func (s *Stream) TransStop() {
	logrus.Debugf("TransStop: %s", s.info.Key)

	if s.isStart && s.r != nil {
		s.r.Close(fmt.Errorf("stop old"))
	}

	s.isStart = false
}

func (s *Stream) CheckAlive() (n int) {
	if s.r != nil && s.isStart {
		if s.r.Alive() {
			n++
		} else {
			s.r.Close(fmt.Errorf("read timeout"))
		}
	}

	s.ws.Range(func(key, val interface{}) bool {
		v := val.(*PackWriterCloser)
		if v.w != nil {
			//Alive from RWBaser, check last frame now - timestamp, if > timeout then Remove it
			if !v.w.Alive() {
				logrus.Infof("write timeout remove")
				s.ws.Delete(key)
				v.w.Close(fmt.Errorf("write timeout"))
				return true
			}
			n++
		}
		return true
	})

	return
}

func (s *Stream) closeInter() {
	if s.r != nil {
		logrus.Debugf("[%v] publisher closed", s.r.Info())
	}

	s.ws.Range(func(key, val interface{}) bool {
		v := val.(*PackWriterCloser)
		if v.w != nil {
			v.w.Close(fmt.Errorf("closed"))
			if v.w.Info().IsInterval() {
				s.ws.Delete(key)
				logrus.Debugf("[%v] player closed and remove\n", v.w.Info())
			}
		}
		return true
	})
}
