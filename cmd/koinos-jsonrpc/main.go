package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"

	jsonrpc "github.com/koinos/koinos-jsonrpc/internal"
	log "github.com/koinos/koinos-log-golang"
	koinosmq "github.com/koinos/koinos-mq-golang"
	util "github.com/koinos/koinos-util-golang"
	ma "github.com/multiformats/go-multiaddr"
	flag "github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	basedirOption        = "basedir"
	amqpOption           = "amqp"
	listenOption         = "listen"
	endpointOption       = "endpoint"
	logLevelOption       = "log-level"
	instanceIDOption     = "instance-id"
	descriptorsDirOption = "descriptors"
)

const (
	basedirDefault        = ".koinos"
	amqpDefault           = "amqp://guest:guest@localhost:5672/"
	listenDefault         = "/ip4/127.0.0.1/tcp/8080"
	endpointDefault       = "/"
	logLevelDefault       = "info"
	descriptorsDirDefault = "descriptors"
)

const (
	appName = "jsonrpc"
	logDir  = "logs"
)

func main() {
	baseDir := flag.StringP(basedirOption, "d", basedirDefault, "the base directory")
	amqp := flag.StringP(amqpOption, "a", "", "AMQP server URL")
	listen := flag.StringP(listenOption, "l", "", "Multiaddr to listen on")
	endpoint := flag.StringP(endpointOption, "e", "", "Http listen endpoint")
	logLevel := flag.StringP(logLevelOption, "v", "", "The log filtering level (debug, info, warn, error)")
	instanceID := flag.StringP(instanceIDOption, "i", "", "The instance ID to identify this node")
	descriptorsDir := flag.StringP(descriptorsDirOption, "D", "", "The directory containing protobuf descriptors for rpc message types")

	flag.Parse()

	*baseDir = util.InitBaseDir(*baseDir)
	util.EnsureDir(*baseDir)
	yamlConfig := util.InitYamlConfig(*baseDir)

	*amqp = util.GetStringOption(amqpOption, amqpDefault, *amqp, yamlConfig.JSONRPC, yamlConfig.Global)
	*listen = util.GetStringOption(listenOption, listenDefault, *listen, yamlConfig.JSONRPC)
	*endpoint = util.GetStringOption(endpointOption, endpointDefault, *endpoint, yamlConfig.JSONRPC)
	*logLevel = util.GetStringOption(logLevelOption, logLevelDefault, *logLevel, yamlConfig.JSONRPC, yamlConfig.Global)
	*instanceID = util.GetStringOption(instanceIDOption, util.GenerateBase58ID(5), *instanceID, yamlConfig.JSONRPC, yamlConfig.Global)
	*descriptorsDir = util.GetStringOption(descriptorsDirOption, descriptorsDirDefault, *descriptorsDir, yamlConfig.JSONRPC, yamlConfig.Global)

	appID := fmt.Sprintf("%s.%s", appName, *instanceID)

	// Initialize logger
	logFilename := path.Join(util.GetAppDir(*baseDir, appName), logDir, "jsonrpc.log")
	err := log.InitLogger(*logLevel, false, logFilename, appID)
	if err != nil {
		panic(fmt.Sprintf("Invalid log-level: %s. Please choose one of: debug, info, warn, error", *logLevel))
	}

	client := koinosmq.NewClient(*amqp, koinosmq.NoRetry)
	client.Start()

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

	jsonrpcHandler := jsonrpc.NewRequestHandler(client)

	if !filepath.IsAbs(*descriptorsDir) {
		*descriptorsDir = path.Join(util.GetAppDir(*baseDir, appName), *descriptorsDir)
	}

	util.EnsureDir(*descriptorsDir)

	// For each file in descriptorsDir, try to parse as a FileDescriptor or FileDescriptorSet
	files, err := ioutil.ReadDir(*descriptorsDir)
	if err != nil {
		log.Errorf("Could not read directory %s: %s", *descriptorsDir, err.Error())
		os.Exit(1)
	}

	var protoFileOpts protodesc.FileOptions
	//var protoFiles protoregistry.Files
	fileDescriptorSet := &descriptorpb.FileDescriptorSet{}

	// Add FieldOptions to protoregistry
	fieldProto := descriptorpb.FieldOptions{}
	fileDescriptorSet.File = append(fileDescriptorSet.File, protodesc.ToFileDescriptorProto(fieldProto.ProtoReflect().Descriptor().ParentFile()))

	for _, f := range files {
		// If it is a file
		if !f.IsDir() {
			fileBytes, err := ioutil.ReadFile(path.Join(*descriptorsDir, f.Name()))
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

				fileDescriptorSet.File = append(fileDescriptorSet.File, fdProto)
			} else {
				for _, fdProto := range fds.GetFile() {
					fileDescriptorSet.File = append(fileDescriptorSet.File, fdProto)
				}
			}
		}
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

	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		log.Debug(string(body))
		response, ok := jsonrpcHandler.HandleRequest(body)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(response)
		log.Debug(string(response))
	}

	http.HandleFunc(*endpoint, httpHandler)
	go http.ListenAndServe(ipAddr+":"+tcpPort, nil)
	log.Infof("Listening on %v:%v%v", ipAddr, tcpPort, *endpoint)

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Info("Shutting down node...")
}
