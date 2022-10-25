module github.com/koinos/koinos-jsonrpc

go 1.15

require (
	github.com/koinos/koinos-log-golang v0.0.0-20210621202301-3310a8e5866b
	github.com/koinos/koinos-mq-golang v0.0.0-20221024231917-d90ee8d48910
	github.com/koinos/koinos-proto-golang v0.3.1-0.20220708180354-16481ac5469c
	github.com/koinos/koinos-util-golang v0.0.0-20220831225923-5ba6e0d4e7b9
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/spf13/pflag v1.0.5
	github.com/streadway/amqp v1.0.0 // indirect
	google.golang.org/protobuf v1.27.1
)

replace google.golang.org/protobuf => github.com/koinos/protobuf-go v1.27.2-0.20211016005428-adb3d63afc5e
