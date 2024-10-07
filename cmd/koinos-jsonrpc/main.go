package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	jsonrpc "github.com/koinos/koinos-jsonrpc/internal"
	log "github.com/koinos/koinos-log-golang/v2"
	koinosmq "github.com/koinos/koinos-mq-golang"
	"github.com/koinos/koinos-proto-golang/v2/koinos"
	"github.com/koinos/koinos-proto-golang/v2/koinos/protocol"
	util "github.com/koinos/koinos-util-golang/v2"
	ma "github.com/multiformats/go-multiaddr"
	flag "github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	basedirOption        = "basedir"
	amqpOption           = "amqp"
	listenOption         = "listen"
	endpointOption       = "endpoint"
	logLevelOption       = "log-level"
	logDirOption         = "log-dir"
	logColorOption       = "log-color"
	logDatetimeOption    = "log-datetime"
	instanceIDOption     = "instance-id"
	descriptorsDirOption = "descriptors"
	jobsOption           = "jobs"
	gatewayTimeoutOption = "gateway-timeout"
	mqTimeoutOption      = "mq-timeout"
	whitelistOption      = "whitelist"
	blacklistOption      = "blacklist"
	versionOption        = "version"
)

const (
	basedirDefault        = ".koinos"
	amqpDefault           = "amqp://guest:guest@localhost:5672/"
	listenDefault         = "/ip4/127.0.0.1/tcp/8080"
	endpointDefault       = "/"
	logLevelDefault       = "info"
	logColorDefault       = true
	logDatetimeDefault    = true
	descriptorsDirDefault = "descriptors"
	jobsDefault           = 16
	gatewayTimeoutDefault = 3
	mqTimeoutDefault      = 5
)

const (
	appName = "jsonrpc"
)

type Job struct {
	request  []byte
	response chan []byte
}

// Version display values
const (
	DisplayAppName = "Koinos JSONRPC"
	Version        = "v1.1.0"
)

// Gets filled in by the linker
var Commit string

func main() {
	baseDirPtr := flag.StringP(basedirOption, "d", basedirDefault, "the base directory")
	amqp := flag.StringP(amqpOption, "a", "", "AMQP server URL")
	listen := flag.StringP(listenOption, "L", "", "Multiaddr to listen on")
	endpoint := flag.StringP(endpointOption, "e", "", "Http listen endpoint")
	logLevel := flag.StringP(logLevelOption, "l", "", "The log filtering level (debug, info, warning, error)")
	logDir := flag.String(logDirOption, "", "The logging directory")
	logColor := flag.Bool(logColorOption, logColorDefault, "Log color toggle")
	logDatetime := flag.Bool(logDatetimeOption, logDatetimeDefault, "Log datetime on console toggle")
	instanceID := flag.StringP(instanceIDOption, "i", "", "The instance ID to identify this node")
	descriptorsDir := flag.StringP(descriptorsDirOption, "D", "", "The directory containing protobuf descriptors for rpc message types")
	jobs := flag.UintP(jobsOption, "j", jobsDefault, "Number of jobs")
	gatewayTimeout := flag.IntP(gatewayTimeoutOption, "g", gatewayTimeoutDefault, "The timeout to enqueue a request")
	mqTimeout := flag.IntP(mqTimeoutOption, "m", mqTimeoutDefault, "The timeout for MQ requests")
	whitelist := flag.StringSliceP(whitelistOption, "w", []string{}, "RPC targets to whitelist")
	blacklist := flag.StringSliceP(blacklistOption, "b", []string{}, "RPC targets to blacklist")
	version := flag.BoolP(versionOption, "v", false, "Print version and exit")

	flag.Parse()

	baseDir, err := util.InitBaseDir(*baseDirPtr)
	if err != nil {
		fmt.Printf("Could not initialize base directory '%v'\n", *baseDirPtr)
		os.Exit(1)
	}

	if *version {
		fmt.Println(makeVersionString())
		os.Exit(0)
	}

	yamlConfig := util.InitYamlConfig(baseDir)

	*amqp = util.GetStringOption(amqpOption, amqpDefault, *amqp, yamlConfig.JSONRPC, yamlConfig.Global)
	*listen = util.GetStringOption(listenOption, listenDefault, *listen, yamlConfig.JSONRPC)
	*endpoint = util.GetStringOption(endpointOption, endpointDefault, *endpoint, yamlConfig.JSONRPC)
	*logLevel = util.GetStringOption(logLevelOption, logLevelDefault, *logLevel, yamlConfig.JSONRPC, yamlConfig.Global)
	*logDir = util.GetStringOption(logDirOption, *logDir, *logDir, yamlConfig.JSONRPC, yamlConfig.Global)
	*logColor = util.GetBoolOption(logColorOption, logColorDefault, *logColor, yamlConfig.JSONRPC, yamlConfig.Global)
	*logDatetime = util.GetBoolOption(logDatetimeOption, logDatetimeDefault, *logDatetime, yamlConfig.JSONRPC, yamlConfig.Global)
	*instanceID = util.GetStringOption(instanceIDOption, util.GenerateBase58ID(5), *instanceID, yamlConfig.JSONRPC, yamlConfig.Global)
	*descriptorsDir = util.GetStringOption(descriptorsDirOption, descriptorsDirDefault, *descriptorsDir, yamlConfig.JSONRPC, yamlConfig.Global)
	*gatewayTimeout = util.GetIntOption(gatewayTimeoutOption, gatewayTimeoutDefault, *gatewayTimeout, yamlConfig.JSONRPC, yamlConfig.Global)
	*mqTimeout = util.GetIntOption(mqTimeoutOption, mqTimeoutDefault, *mqTimeout, yamlConfig.JSONRPC, yamlConfig.Global)
	*whitelist = util.GetStringSliceOption(whitelistOption, *whitelist, yamlConfig.JSONRPC, yamlConfig.Global)
	*blacklist = util.GetStringSliceOption(blacklistOption, *blacklist, yamlConfig.JSONRPC, yamlConfig.Global)

	if len(*logDir) > 0 && !path.IsAbs(*logDir) {
		*logDir = path.Join(util.GetAppDir(baseDir, appName), *logDir)
	}

	err = log.InitLogger(appName, *instanceID, *logLevel, *logDir, *logColor, *logDatetime)
	if err != nil {
		panic(fmt.Sprintf("Invalid log-level: %s. Please choose one of: debug, info, warning, error", *logLevel))
	}

	log.Info(makeVersionString())

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	client := koinosmq.NewClient(*amqp, koinosmq.NoRetry)
	client.Start(ctx)

	m, err := ma.NewMultiaddr(*listen)
	if err != nil {
		panic(err)
	}

	ipAddr, err := m.ValueForProtocol(ma.P_IP4)
	if err != nil {
		ipAddr = ""
	}

	tcpPort, err := m.ValueForProtocol((ma.P_TCP))
	if err != nil {
		panic("Expected tcp port")
	}

	if len(*whitelist) > 0 {
		log.Infof("Whitelist: %s", *whitelist)
	}

	if len(*blacklist) > 0 {
		log.Infof("Blacklist: %s", *blacklist)
	}

	jsonrpcHandler := jsonrpc.NewRequestHandler(client, uint(*mqTimeout), *whitelist, *blacklist)

	if !filepath.IsAbs(*descriptorsDir) {
		*descriptorsDir = path.Join(util.GetAppDir(baseDir, appName), *descriptorsDir)
	}

	err = util.EnsureDir(*descriptorsDir)
	if err != nil {
		log.Errorf("Could not read directory %s: %s", *descriptorsDir, err.Error())
		os.Exit(1)
	}

	// For each file in descriptorsDir, try to parse as a FileDescriptor or FileDescriptorSet
	files, err := os.ReadDir(*descriptorsDir)
	if err != nil {
		log.Errorf("Could not read directory %s: %s", *descriptorsDir, err.Error())
		os.Exit(1)
	}

	fileMap := make(map[string]*descriptorpb.FileDescriptorProto)

	// Add FieldOptions to protoregistry
	fieldProtoFile := protodesc.ToFileDescriptorProto((&descriptorpb.FieldOptions{}).ProtoReflect().Descriptor().ParentFile())
	fileMap[*fieldProtoFile.Name] = fieldProtoFile

	anyProtoFile := protodesc.ToFileDescriptorProto((&anypb.Any{}).ProtoReflect().Descriptor().ParentFile())
	fileMap[*anyProtoFile.Name] = anyProtoFile

	optionsFile := protodesc.ToFileDescriptorProto((koinos.BytesType(0)).Descriptor().ParentFile())
	fileMap[*optionsFile.Name] = optionsFile

	commonFile := protodesc.ToFileDescriptorProto((&koinos.BlockTopology{}).ProtoReflect().Descriptor().ParentFile())
	fileMap[*commonFile.Name] = commonFile

	protocolFile := protodesc.ToFileDescriptorProto((&protocol.Block{}).ProtoReflect().Descriptor().ParentFile())
	fileMap[*protocolFile.Name] = protocolFile

	chainFile := protodesc.ToFileDescriptorProto((&koinos.BlockTopology{}).ProtoReflect().Descriptor().ParentFile())
	fileMap[*chainFile.Name] = chainFile

	for _, f := range files {
		// If it is a file
		if !f.IsDir() {
			fileBytes, err := os.ReadFile(path.Join(*descriptorsDir, f.Name()))
			if err != nil {
				log.Errorf("Could not read file %s: %s", f.Name(), err.Error())
				continue
			}

			var fds descriptorpb.FileDescriptorSet
			err = proto.Unmarshal(fileBytes, &fds)
			if err != nil {
				fdProto := &descriptorpb.FileDescriptorProto{}
				err2 := proto.Unmarshal(fileBytes, fdProto)
				if err2 != nil {
					log.Errorf("Could not parse file %s: (%s, %s)", f.Name(), err.Error(), err2.Error())
					continue
				}

				fileMap[*fdProto.Name] = fdProto
			} else {
				for _, fdProto := range fds.GetFile() {
					fileMap[*fdProto.Name] = fdProto
				}
			}
		}
	}

	var protoFileOpts protodesc.FileOptions
	fileDescriptorSet := &descriptorpb.FileDescriptorSet{}

	for _, v := range fileMap {
		fileDescriptorSet.File = append(fileDescriptorSet.File, v)
	}

	protoFiles, err := protoFileOpts.NewFiles(fileDescriptorSet)
	if err != nil {
		log.Errorf("Could not convert file descriptor set: %s", err.Error())
		os.Exit(1)
	}

	protoFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		jsonrpcHandler.RegisterService(fd)
		return true
	})

	jobChan := make(chan Job, *jobs*2)

	for i := uint(0); i < *jobs; i++ {
		go func() {
			for {
				select {
				case job := <-jobChan:
					resp, ok := jsonrpcHandler.HandleRequest(job.request)
					if !ok {
						close(job.response)
					} else {
						job.response <- resp
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	var recentRequests uint32

	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		log.Debug(string(body))
		respChan := make(chan []byte, 1)
		job := Job{request: body, response: respChan}

		select {
		case jobChan <- job:
		case <-ctx.Done():
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case <-time.After(time.Duration(*gatewayTimeout) * time.Second):
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}

		response, ok := <-respChan
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(response)
		if err != nil {
			log.Warnf("An error occurred while writing HTTP response: %v", err.Error())
			return
		}

		atomic.AddUint32(&recentRequests, 1)

		log.Debug(string(response))
	}

	http.HandleFunc(*endpoint, httpHandler)

	errs := make(chan error, 1)
	go func() {
		errs <- http.ListenAndServe(ipAddr+":"+tcpPort, nil)
	}()

	log.Infof("Listening on %v:%v%v", ipAddr, tcpPort, *endpoint)

	go func() {
		for {
			select {
			case <-time.After(60 * time.Second):
				numRequests := atomic.SwapUint32(&recentRequests, 0)

				if numRequests > 0 {
					log.Infof("Recently handled %v request(s)", numRequests)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := <-errs; err != nil {
		log.Error(err.Error())
	}

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info("Shutting down node...")
}

func makeVersionString() string {
	commitString := ""
	if len(Commit) >= 8 {
		commitString = fmt.Sprintf("(%s)", Commit[0:8])
	}

	return fmt.Sprintf("%s %s %s", DisplayAppName, Version, commitString)
}
