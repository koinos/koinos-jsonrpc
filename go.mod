module github.com/koinos/koinos-jsonrpc

go 1.15

require (
	github.com/koinos/koinos-log-golang v0.0.0-20210621202301-3310a8e5866b
	github.com/koinos/koinos-mq-golang v0.0.0-20210424202816-d2bd4d1894d1
	github.com/koinos/koinos-proto-golang v0.0.0-20211018161645-72b02953d6cf
	github.com/koinos/koinos-util-golang v0.0.0-20210909225633-da2fb456bade
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/spf13/pflag v1.0.5
	google.golang.org/protobuf v1.27.1
)

replace google.golang.org/protobuf => github.com/koinos/protobuf-go v1.27.2-0.20211016005428-adb3d63afc5e
