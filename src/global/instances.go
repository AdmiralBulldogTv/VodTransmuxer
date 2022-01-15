package global

import "github.com/AdmiralBulldogTv/VodTransmuxer/src/instance"

type Instances struct {
	Redis      instance.Redis
	Mongo      instance.Mongo
	Prometheus instance.Prometheus
	RMQ        instance.RMQ
}
