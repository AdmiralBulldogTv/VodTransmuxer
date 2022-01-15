package parser

import (
	"fmt"
	"io"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/parser/aac"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/parser/h264"
)

var (
	errNoAudio = fmt.Errorf("demuxer no audio")
)

type CodecParser struct {
	aac  *aac.Parser
	h264 *h264.Parser
}

func NewCodecParser() *CodecParser {
	return &CodecParser{}
}

func (codeParser *CodecParser) SampleRate() (int, error) {
	if codeParser.aac == nil {
		return 0, errNoAudio
	}
	return codeParser.aac.SampleRate(), nil
}

func (codeParser *CodecParser) Parse(p *av.Packet, w io.Writer) error {
	if p.IsVideo {
		f, ok := p.Header.(av.VideoPacketHeader)
		if ok {
			if f.CodecID() == av.VIDEO_H264 {
				if codeParser.h264 == nil {
					codeParser.h264 = h264.NewParser()
				}
				return codeParser.h264.Parse(p.Data, f.IsSeq(), w)
			}
		}
	} else {
		f, ok := p.Header.(av.AudioPacketHeader)
		if ok {
			if f.SoundFormat() == av.SOUND_AAC {
				if codeParser.aac == nil {
					codeParser.aac = aac.NewParser()
				}
				return codeParser.aac.Parse(p.Data, f.AACPacketType(), w)
			}
		}
	}
	return fmt.Errorf("invalid type")
}
