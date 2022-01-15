package cache

import (
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
)

type Cache struct {
	gop      *GopCache
	videoSeq *SpecialCache
	audioSeq *SpecialCache
	metadata *SpecialCache
}

func NewCache() *Cache {
	return &Cache{
		gop:      NewGopCache(1),
		videoSeq: NewSpecialCache(),
		audioSeq: NewSpecialCache(),
		metadata: NewSpecialCache(),
	}
}

func (cache *Cache) Write(p av.Packet) error {
	if p.IsMetadata {
		cache.metadata.Write(&p)
		return nil
	} else {
		if !p.IsVideo {
			ah, ok := p.Header.(av.AudioPacketHeader)
			if ok {
				if ah.SoundFormat() == av.SOUND_AAC &&
					ah.AACPacketType() == av.AAC_SEQHDR {
					cache.audioSeq.Write(&p)
					return nil
				} else {
					return nil
				}
			}

		} else {
			vh, ok := p.Header.(av.VideoPacketHeader)
			if ok {
				if vh.IsSeq() {
					cache.videoSeq.Write(&p)
					return nil
				}
			} else {
				return nil
			}

		}
	}
	return cache.gop.Write(&p)
}

func (cache *Cache) Send(w av.WriteCloser) error {
	if err := cache.metadata.Send(w); err != nil {
		return err
	}

	if err := cache.videoSeq.Send(w); err != nil {
		return err
	}

	if err := cache.audioSeq.Send(w); err != nil {
		return err
	}

	if err := cache.gop.Send(w); err != nil {
		return err
	}

	return nil
}
