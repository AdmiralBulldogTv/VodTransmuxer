package rmq

import (
	"context"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/instance"
	"github.com/streadway/amqp"
)

type RmqInst struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func New(ctx context.Context, opts SetupOptions) (instance.RMQ, error) {
	conn, err := amqp.Dial(opts.URI)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	_, err = ch.QueueDeclare(opts.QueueName, true, false, false, false, nil)
	if err != nil {
		return nil, err
	}

	return &RmqInst{
		conn: conn,
		ch:   ch,
	}, nil
}

func (r *RmqInst) Publish(queueName string, msg amqp.Publishing) error {
	return r.ch.Publish("", queueName, false, false, msg)
}

type SetupOptions struct {
	URI       string
	QueueName string
}
