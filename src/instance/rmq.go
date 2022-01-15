package instance

import "github.com/streadway/amqp"

type RMQ interface {
	Publish(queueName string, msg amqp.Publishing) error
}
