package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"syscall"

	jsonrpc "github.com/koinos/koinos-jsonrpc/internal"
	koinosmq "github.com/koinos/koinos-mq-golang"
	ma "github.com/multiformats/go-multiaddr"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

const (
	basedirOption  = "basedir"
	amqpOption     = "amqp"
	listenOption   = "listen"
	endpointOption = "endpoint"
)

const (
	basedirDefault  = ".koinos"
	amqpDefault     = "amqp://guest:guest@localhost:5672/"
	listenDefault   = "/ip4/127.0.0.1/tcp/8080"
	endpointDefault = "/"
)

func main() {
	var baseDir = flag.StringP(basedirOption, "d", basedirDefault, "the base directory")
	var amqp = flag.StringP(amqpOption, "a", "", "AMQP server URL")
	var listen = flag.StringP(listenOption, "l", "", "Multiaddr to listen on")
	var endpoint = flag.StringP(endpointOption, "e", "", "Http listen endpoint")

	flag.Parse()

	if !filepath.IsAbs(*baseDir) {
		homedir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		*baseDir = filepath.Join(homedir, *baseDir)
	}
	ensureDir(*baseDir)

	yamlConfigPath := filepath.Join(*baseDir, "config.yml")
	if _, err := os.Stat(yamlConfigPath); os.IsNotExist(err) {
		yamlConfigPath = filepath.Join(*baseDir, "config.yaml")
	}

	yamlConfig := yamlConfig{}
	if _, err := os.Stat(yamlConfigPath); err == nil {
		data, err := ioutil.ReadFile(yamlConfigPath)
		if err != nil {
			panic(err)
		}

		err = yaml.Unmarshal(data, &yamlConfig)
		if err != nil {
			panic(err)
		}
	} else {
		yamlConfig.Global = make(map[string]interface{})
		yamlConfig.JSONRPC = make(map[string]interface{})
	}

	*amqp = getStringOption(amqpOption, amqpDefault, *amqp, yamlConfig.JSONRPC, yamlConfig.Global)
	*listen = getStringOption(listenOption, listenDefault, *listen, yamlConfig.JSONRPC)
	*endpoint = getStringOption(endpointOption, endpointDefault, *endpoint, yamlConfig.JSONRPC)

	client := koinosmq.NewClient(*amqp)
	client.Start()

	m, err := ma.NewMultiaddr(*listen)
	if err != nil {
		panic(err)
	}

	ipAddr, err := m.ValueForProtocol(ma.P_IP4)
	if err != nil {
		panic("Expected ip4 address")
	}

	tcpPort, err := m.ValueForProtocol((ma.P_TCP))
	if err != nil {
		panic("Expected tcp port")
	}

	httpHandler := func(w http.ResponseWriter, req *http.Request) {
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		response, ok := jsonrpc.HandleJSONRPCRequest(body, client)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(response)
	}

	http.HandleFunc(*endpoint, httpHandler)
	go http.ListenAndServe(ipAddr+":"+tcpPort, nil)
	log.Printf("Listensing on %v:%v%v", ipAddr, tcpPort, *endpoint)

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("Shutting down node...")
}

func getHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("There was a problem finding the user's home directory")
	}

	if runtime.GOOS == "windows" {
		home = path.Join(home, "AppData")
	}

	return home
}

type yamlConfig struct {
	Global  map[string]interface{} `yaml:"global,omitempty"`
	JSONRPC map[string]interface{} `yaml:"jsonrpc,omitempty"`
}

func getStringOption(key string, defaultValue string, cliArg string, configs ...map[string]interface{}) string {
	if cliArg != "" {
		return cliArg
	}

	for _, config := range configs {
		if v, ok := config[key]; ok {
			if option, ok := v.(string); ok {
				return option
			}
		}
	}

	return defaultValue
}

func ensureDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, os.ModePerm)
	}
}
