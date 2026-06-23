package pubsub

type Publisher interface {
	Publish(topic string, payload []byte) error
}

type Subscriber interface {
	Subscribe(topic string, handler func(topic string, payload []byte)) error
	Unsubscribe(topic string) error
}

type PubSub interface {
	Publisher
	Subscriber
	Close() error
}
