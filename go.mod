module github.com/koinos/koinos-jsonrpc

go 1.15

require (
	github.com/koinos/koinos-log-golang v1.0.1-0.20231002210928-929b5ecd5bc8 // indirect
	github.com/koinos/koinos-log-golang/v2 v2.0.0
	github.com/koinos/koinos-mq-golang v1.0.0
	github.com/koinos/koinos-proto-golang/v2 v2.0.2-0.20240326155010-7093c73d96a4
	github.com/koinos/koinos-util-golang/v2 v2.0.1-0.20240326163418-f3fe2cd5d271
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/spf13/pflag v1.0.5
	google.golang.org/protobuf v1.30.0
)

replace google.golang.org/protobuf => github.com/koinos/protobuf-go v1.27.2-0.20211016005428-adb3d63afc5e
