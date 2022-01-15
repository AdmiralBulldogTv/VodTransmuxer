package protocol

import (
	"net"
	"sync"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/global"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/rtmp"
	"github.com/sirupsen/logrus"
)

func New(gCtx global.Context) <-chan struct{} {
	done := make(chan struct{})

	extRtmpListen, err := net.Listen("tcp", gCtx.Config().RTMP.Bind)
	if err != nil {
		panic(err)
	}

	extStream := rtmp.NewRtmpStream()
	rtmpTranscode := rtmp.NewRtmpServer(gCtx, extStream)

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		logrus.Info("RTMP Listen On ", extRtmpListen.Addr())
		rtmpTranscode.Serve(extRtmpListen)
	}()

	go func() {
		<-gCtx.Done()
		wg.Wait()
		close(done)
	}()

	return done
}
