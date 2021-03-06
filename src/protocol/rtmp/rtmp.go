package rtmp

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/av"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/global"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/protocol/rtmp/core"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/structures"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/svc/mongo"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/twitch"
	"github.com/AdmiralBulldogTv/VodTransmuxer/src/utils/uid"
	jsoniter "github.com/json-iterator/go"
	"github.com/nicklaw5/helix"
	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	maxQueueNum = 1024
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

var (
	writeTimeout      = 10 * time.Second
	errInvalidKey     = fmt.Errorf("invalid streamkey")
	errInternalServer = fmt.Errorf("internal server error")
)

type Server struct {
	handler     av.Handler
	gCtx        global.Context
	wg          *sync.WaitGroup
	streamers   map[string]*core.ConnServer
	streamerMtx *sync.Mutex
}

func NewRtmpServer(gCtx global.Context, h av.Handler) *Server {
	return &Server{
		handler:     h,
		gCtx:        gCtx,
		wg:          &sync.WaitGroup{},
		streamers:   map[string]*core.ConnServer{},
		streamerMtx: &sync.Mutex{},
	}
}

func (s *Server) Serve(listener net.Listener) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Error("rtmp serve panic: ", r)
		}
	}()

	var err error
	go func() {
		for {
			var netconn net.Conn
			netconn, err = listener.Accept()
			if err != nil {
				return
			}
			conn := core.NewConn(netconn, 4*1024)
			logrus.Debugf("new client, connect remote: %s local: %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
			go s.handleConn(conn)
		}
	}()
	<-s.gCtx.Done()
	s.wg.Wait()
	listener.Close()
}

func (s *Server) handleConn(conn *core.Conn) {
	connServer := core.NewConnServer(conn)

	if err := conn.HandshakeServer(); err != nil {
		conn.Close()
		logrus.Error("handleConn HandshakeServer err: ", err)
		return
	}

	ctx, cancel := context.WithCancel(s.gCtx)
	defer cancel()

	var (
		start time.Time
		vodID primitive.ObjectID
		user  structures.User
	)

	connServer.SetCallbackAuth(func() error {
		appname, name, _ := connServer.GetInfo()
		if connServer.IsPublisher() {
			if appname != "vods" && appname != "vods-relay" {
				return fmt.Errorf("application name=%s is not configured", appname)
			}
			logrus.Infof("publisher: %s/%s", appname, name)
			// check the stream key here.
			// stream key structure will be live_id_key
			splits := strings.SplitN(name, "_", 3)
			if len(splits) != 3 || splits[0] != "live" {
				return errInvalidKey
			}

			uID, err := primitive.ObjectIDFromHex(splits[1])
			if err != nil {
				return errInvalidKey
			}

			{
				lCtx, cancel := context.WithTimeout(ctx, time.Second*5)
				res := s.gCtx.Inst().Mongo.Collection(mongo.CollectionNameUsers).FindOne(lCtx, bson.M{"_id": uID, "stream_key": splits[2]})
				cancel()
				err = res.Err()
				if err != nil {
					if err == mongo.ErrNoDocuments {
						return errInvalidKey
					}

					logrus.Error("mongo query error: ", err)
					return errInternalServer
				}

				if err := res.Decode(&user); err != nil {
					logrus.Error("mongo decode error: ", err)
					return errInternalServer
				}

				logrus.Infof("user: %s started a new stream.", user.Twitch.Login)
			}

			start = time.Now()
			vodID = primitive.NewObjectIDFromTimestamp(start)

			redisCh := make(chan string, 10)
			s.gCtx.Inst().Redis.Subscribe(ctx, redisCh, fmt.Sprintf("stream-events:%s", user.ID.Hex()))
			go func() {
				defer close(redisCh)
				defer cancel()
				for msg := range redisCh {
					splits := strings.SplitN(msg, " ", 2)
					if splits[0] == "drop" && splits[1] != vodID.Hex() {
						return
					}
				}
			}()

			if err := s.gCtx.Inst().Redis.Publish(ctx, fmt.Sprintf("stream-events:%s", user.ID.Hex()), fmt.Sprintf("drop %s", vodID.Hex())); err != nil {
				logrus.Error("failed to emit drop event: ", err)
				return err
			}

			{
				i := -1
			start:
				i++
				if i == 8 {
					logrus.Errorf("failed to aquire stream lock: '%s'", "streamer-live:"+user.ID.Hex())
					return fmt.Errorf("failed to aquire lock")
				}
				set, err := s.gCtx.Inst().Redis.SetNX(ctx, "streamer-live:"+user.ID.Hex(), vodID.Hex(), time.Second*30)
				if err != nil {
					logrus.Error("failed to set key: ", err)
					return err
				}

				if !set {
					time.Sleep(time.Millisecond * 250)
					goto start
				}

				go func() {
					tick := time.NewTicker(time.Second * 15)
					defer func() {
						cancel()
						tick.Stop()

						err := s.gCtx.Inst().Redis.Del(context.Background(), "streamer-live:"+user.ID.Hex())
						if err != nil {
							logrus.Error("failed to delete key: ", err)
						}
					}()
					for {
						select {
						case <-ctx.Done():
							return
						case <-tick.C:
							err := s.gCtx.Inst().Redis.Expire(ctx, "streamer-live:"+user.ID.Hex(), time.Second*30)
							if err != nil {
								logrus.Error("failed to expire key: ", err)
								return
							}
						}
					}
				}()
			}

			{
				categories := []structures.VodCategory{{
					Timestamp: start,
					Name:      "Unknown",
					ID:        "0",
					URL:       "https://static-cdn.jtvnw.net/ttv-static/404_boxart.jpg",
				}}
				title := "Unknown"
				lCtx, cancel := context.WithTimeout(ctx, time.Second*5)
				auth, err := twitch.GetAuth(s.gCtx, lCtx)
				cancel()
				if err == nil {
					lCtx, cancel = context.WithTimeout(ctx, time.Second*5)
					req, err := http.NewRequestWithContext(lCtx, "GET", "https://api.twitch.tv/helix/channels?broadcaster_id="+user.Twitch.ID, nil)
					if err == nil {
						req.Header.Add("Client-Id", s.gCtx.Config().Twitch.ClientID)
						req.Header.Add("Authorization", "Bearer "+auth)
						resp, err := http.DefaultClient.Do(req)
						if err == nil {
							defer resp.Body.Close()
							data, err := ioutil.ReadAll(resp.Body)
							if err == nil {
								body := helix.ManyChannelInformation{}
								if err := json.Unmarshal(data, &body); err == nil && len(body.Channels) != 0 {
									url := fmt.Sprintf("https://static-cdn.jtvnw.net/ttv-boxart/%s-144x192.jpg", body.Channels[0].GameID)
									if body.Channels[0].GameName == "" {
										body.Channels[0].GameName = "Unknown"
										body.Channels[0].GameID = "0"
										url = "https://static-cdn.jtvnw.net/ttv-static/404_boxart.jpg"
									}
									categories[0] = structures.VodCategory{
										Timestamp: start,
										Name:      body.Channels[0].GameName,
										ID:        body.Channels[0].GameID,
										URL:       url,
									}
									if body.Channels[0].Title != "" {
										title = body.Channels[0].Title
									}
								} else {
									logrus.Errorf("bad resp from twitch: %s : %s", err, data)
								}
							} else {
								logrus.Error("bad resp from twitch: ", err)
							}
						} else {
							logrus.Error("bad resp from twitch: ", err)
						}
					} else {
						logrus.Error("http: ", err)
					}
				} else {
					logrus.Error("failed to get auth: ", err)
				}
				cancel()

				lCtx, cancel = context.WithTimeout(ctx, time.Second*5)
				_, err = s.gCtx.Inst().Mongo.Collection(mongo.CollectionNameVods).InsertOne(lCtx, structures.Vod{
					ID:         vodID,
					UserID:     uID,
					State:      structures.VodStateLive,
					Title:      title,
					StartedAt:  start,
					Categories: categories,
					Variants:   []structures.VodVariant{},
					Visibility: structures.VodVisibilityPublic,
				})
				cancel()
				if err != nil {
					logrus.Error("mongo insert error: ", err)
					return errInternalServer
				}
			}
		} else {
			if appname != "local" {
				return fmt.Errorf("application name=%s is not configured", appname)
			}

			ip := conn.RemoteAddr().(*net.TCPAddr)
			if !ip.IP.IsLoopback() {
				return fmt.Errorf("only local playback")
			}
		}
		return nil
	})

	if err := connServer.ReadMsg(); err != nil {
		connServer.Close()
		return
	}

	appname, name, _ := connServer.GetInfo()
	logrus.Debugf("handleConn: IsPublisher=%v", connServer.IsPublisher())
	if connServer.IsPublisher() {
		localLog := logrus.WithField("appname", appname).WithField("name", name)
		s.gCtx.Inst().Prometheus.CurrentStreamCount().Inc()
		localLog.Infof("New stream, %s/%s", appname, name)
		defer localLog.Infof("Stopped stream, %s/%s", appname, name)
		s.wg.Add(1)
		variants := []structures.VodVariant{}

		defer func() {
			s.gCtx.Inst().Prometheus.CurrentStreamCount().Dec()
			s.gCtx.Inst().Prometheus.TotalStreamDurationSeconds().Observe(float64(time.Since(start)/time.Millisecond) / 1000)
			defer s.wg.Done()
			connServer.Close()
			if start.After(time.Now().Add(-time.Minute)) {
				localLog.Info("vod canceled")
				_, err := s.gCtx.Inst().Mongo.Collection(mongo.CollectionNameVods).UpdateOne(s.gCtx, bson.M{
					"_id": vodID,
				}, bson.M{
					"$set": bson.M{
						"vod_state": structures.VodStateCanceled,
						"ended_at":  time.Now(),
					},
				})
				if err != nil {
					localLog.Error("mongo update error: ", err)
					return
				}
				if err := os.Remove(fmt.Sprintf("%s/%s.flv", s.gCtx.Config().RTMP.WritePath, vodID.Hex())); err != nil {
					localLog.Errorf("failed to remove vod from disk: %s : %s", fmt.Sprintf("%s/%s.flv", s.gCtx.Config().RTMP.WritePath, vodID.Hex()), err.Error())
				}
			} else {
				localLog.Info("vod ended after: ", time.Since(start))
				_, err := s.gCtx.Inst().Mongo.Collection(mongo.CollectionNameVods).UpdateOne(s.gCtx, bson.M{
					"_id": vodID,
				}, bson.M{
					"$set": bson.M{
						"vod_state": structures.VodStateQueued,
						"variants":  variants,
						"ended_at":  time.Now(),
					},
				})
				if err != nil {
					localLog.Error("mongo update error: ", err)
					return
				}

				for _, v := range variants {
					body, _ := json.Marshal(structures.VodTranscodeJob{
						VodID:   vodID,
						Variant: v,
					})

					if err := s.gCtx.Inst().RMQ.Publish(s.gCtx.Config().RMQ.TranscoderTaskQueue, amqp.Publishing{
						ContentType:  "application/json",
						DeliveryMode: amqp.Persistent,
						Body:         body,
					}); err != nil {
						localLog.Error("rmq publish error: ", err)
						return
					}
				}
			}
		}()

		sid := uid.NewId()
		reader := NewVirReader(connServer, uid.NewId(), fmt.Sprintf("local/%s", sid))
		s.handler.HandleReader(reader)

		localLog.Debug("new publisher: ", reader.Info())
		// we have to transcode this
		rtmpUrl := fmt.Sprintf("rtmp://127.0.0.1:1935/local/%s", sid)

		ffprobeCtx, ffprobeCancel := context.WithTimeout(ctx, time.Second*5)
		data, err := exec.CommandContext(ffprobeCtx, "ffprobe",
			"-v", "quiet",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			rtmpUrl, // rtmp url
		).CombinedOutput()
		ffprobeCancel()
		if err != nil {
			localLog.Errorf("failed to read stream: %s %s %s", rtmpUrl, err.Error(), data)
			return
		}
		mp := FFProbeData{}
		if err := json.Unmarshal(data, &mp); err != nil {
			localLog.Error("failed to read stream: ", err)
			return
		}
		// we have to figure out what the transcode is

		audio := mp.GetAudio()
		if audio.CodecType == "" {
			localLog.Error("could not find audio track")
			return
		}
		video := mp.GetVideo()
		if video.CodecType == "" {
			localLog.Error("could not find video track")
			return
		}

		bitrate, _ := strconv.Atoi(video.BitRate)
		if bitrate == 0 {
			localLog.Error("bad video bitrate")
			return
		}

		if bitrate > 12*1024*1024 { // 12000Kbps
			localLog.Error("bad video bitrate, bitrate too high: ", bitrate)
			return
		}

		width := video.Width
		height := video.Height

		_fps := strings.SplitN(video.RFrameRate, "/", 2)
		if len(_fps) != 2 {
			localLog.Error("bad video fps: ", video.RFrameRate)
			return
		}
		fpsLeft, _ := strconv.Atoi(_fps[0])
		fpsRight, _ := strconv.Atoi(_fps[1])
		fps := float64(fpsLeft) / float64(fpsRight)
		if fps > 120 {
			localLog.Error("bad video fps, fps too high: ", fps)
			return
		}
		if fps < 20 {
			localLog.Error("bad video fps, fps too low: ", fps)
			return
		}

		is16by9 := width*9 == height*16

		variants = append(variants, structures.VodVariant{
			Name:    "source",
			Width:   int(width),
			Height:  int(height),
			FPS:     int(fps),
			Bitrate: bitrate,
		})

		if is16by9 {
			if height > 720 && bitrate > 4500*1024 {
				variants = append(variants, structures.VodVariant{
					Name:    "720",
					Width:   1280,
					Height:  720,
					FPS:     int(math.Min(fps, 60)),
					Bitrate: 3250 * 1024,
				})
			}
			if height > 360 && bitrate > 2500*1024 {
				variants = append(variants, structures.VodVariant{
					Name:    "360",
					Width:   640,
					Height:  360,
					FPS:     int(math.Min(fps, 30)),
					Bitrate: 1250 * 1024,
				})
			}
		}

		ffmpegCtx, ffmpegCancel := context.WithCancel(context.Background())

		ffmpegCmd := exec.CommandContext(ffmpegCtx, "ffmpeg",
			"-i", rtmpUrl,
			"-c", "copy",
			"-f", "flv",
			fmt.Sprintf("%s/%s.flv", s.gCtx.Config().RTMP.WritePath, vodID.Hex()),
		)

		stdErr, _ := ffmpegCmd.StderrPipe()
		defer stdErr.Close()

		go func() {
			reader := textproto.NewReader(bufio.NewReader(stdErr))
			for {
				line, err := reader.ReadLine()
				localLog.Debug("ffmpeg output: ", line)
				if err != nil {
					return
				}
			}
		}()

		ffmpegCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		go func() {
			defer func() {
				cancel()
				ffmpegCancel()
			}()
			// this function must run in a different context
			if err := ffmpegCmd.Run(); err != nil {
				localLog.Error("failed to run transcoder: ", err)
			}
		}()

		<-ctx.Done()
		_ = ffmpegCmd.Process.Signal(syscall.SIGINT)
		select {
		case <-ffmpegCtx.Done():
		case <-time.After(time.Second * 15):
			ffmpegCancel()
		}
	} else {
		writer := NewVirWriter(connServer)
		logrus.Debugf("new player: %+v", writer.Info())
		s.handler.HandleWriter(writer)
	}
}
