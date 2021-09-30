package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	log "github.com/koinos/koinos-log-golang"
	koinosmq "github.com/koinos/koinos-mq-golang"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// The RPCRequest allows for parsing incoming JSON RPC
// while deferring the parsing of the params
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id"`
	Params  json.RawMessage `json:"params"`
}

// RPCError represents a JSON RPC error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// RPCResponse represents a JSON RPC response
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
	ID      interface{}     `json:"id"`
}

// RequestHandler handles jsonrpc requests
type RequestHandler struct {
	mqClient           *koinosmq.Client
	serviceDescriptors map[string]protoreflect.FileDescriptor
}

const (
	// JSONRPCAppError indicates an application error
	JSONRPCAppError = -32001

	// JSONRPCParseError indicates an unparseable request
	JSONRPCParseError = -32700

	// JSONRPCInvalidReq indicates an invalid request
	JSONRPCInvalidReq = -32600

	// JSONRPCMethodNotFound indicates the requested method is unknown
	JSONRPCMethodNotFound = -32601

	// JSONRPCInvalidParams indicates the provided params are not valid
	JSONRPCInvalidParams = -32602

	// JSONRPCInternalError indicates an internal server error
	JSONRPCInternalError = -32603
)

var (
	// ErrMalformedMethod indicates the method was not properly formed
	ErrMalformedMethod = errors.New("Methods should be in the format service_name.method_name")

	// ErrInvalidService indicates the correct ServiceName was not supplied
	ErrInvalidService = errors.New("Invalid service name provided")

	// ErrUnknownMethod indicates the method is not known
	ErrUnknownMethod = errors.New("Unknown method")

	// ErrInvalidParams indicates the parameters could not be parsed
	ErrInvalidParams = errors.New("Parameters could not be parsed")

	// ErrInvalidJSONRPCVersion indicates an improper JSON RPC version was specified
	ErrInvalidJSONRPCVersion = errors.New("Invalid or missing JSON RPC version was specified")

	// ErrInvalidJSONRPCID indicates an invalid JSON RPC ID was provided
	ErrInvalidJSONRPCID = errors.New("Invalid ID was specified")

	// ErrMissingJSONRPCID indicates the ID does not exist
	ErrMissingJSONRPCID = errors.New("Missing ID")

	// ErrFractionalJSONRPCID indicates a fractional number was identified as the ID
	ErrFractionalJSONRPCID = errors.New("ID must not contain fractional parts")

	// ErrUnsupportedJSONRPCIDType indicates an ID type that is unsupported
	ErrUnsupportedJSONRPCIDType = errors.New("An ID must be a Number (non-fractional), String, or Null")

	// ErrUnexpectedResponse indicates a malformed RPC response
	ErrUnexpectedResponse = errors.New("Unexpected RPC response from microservice")
)

const (
	// MethodSeparator is used to in the method name to split the microservice name and desired method to run
	MethodSeparator = "."

	// MethodSections defines the number of sections in the JSON RPC method
	MethodSections = 2

	// RPCTimeoutSeconds defines how long to wait for a Koinos RPC response
	RPCTimeoutSeconds = 5
)

func errorWithID(e error) bool {
	switch e {
	case ErrInvalidJSONRPCID:
	case ErrMissingJSONRPCID:
	case ErrFractionalJSONRPCID:
	case ErrUnsupportedJSONRPCIDType:
	default:
		return false
	}
	return true
}

func parseMethod(j *RPCRequest) (string, string, string, error) {
	methodData := strings.SplitN(j.Method, MethodSeparator, MethodSections)
	if len(methodData) < MethodSections {
		return "", "", "", ErrMalformedMethod
	}
	service := methodData[len(methodData)-2]
	qualifiedService := strings.Join(methodData[:len(methodData)-1], MethodSeparator)
	method := methodData[len(methodData)-1]

	if len(methodData) == MethodSections {
		qualifiedService = "koinos.rpc." + service
	}

	return service, qualifiedService, method, nil
}

func translateRequest(j *RPCRequest, service string, qualifiedService string, method string, services map[string]protoreflect.FileDescriptor) ([]byte, error) {
	// Attempt to find service name as a FileDescripor
	// If I cannot find it, prefix with 'koinos.rpc.' and attempt again
	// (koinos.rpc.mempool and mempool will both be valid serives names)
	filed, exists := services[service]
	if !exists {
		filed, exists = services[qualifiedService]

		if !exists {
			return nil, ErrInvalidService
		}
	}

	// Find and create request message
	desc := filed.Messages().ByName(protoreflect.Name(service + "_request"))
	if desc == nil {
		return nil, ErrInvalidService
	}
	req := dynamicpb.NewMessage(desc)

	// Find the method MessageDescriptor
	desc = filed.Messages().ByName(protoreflect.Name(method + "_request"))
	if desc == nil {
		return nil, ErrUnknownMethod
	}

	// Construct proper requst object ('get_pending_transactions' -> 'get_pending_transactions_request')
	// Parse request to Message
	msg := dynamicpb.NewMessage(desc)
	err := protojson.Unmarshal(j.Params, msg)
	if err != nil {
		return nil, ErrInvalidParams
	}

	fieldd := req.Descriptor().Fields().ByName(protoreflect.Name(method))
	if fieldd == nil {
		return nil, ErrUnknownMethod
	}
	req.Set(fieldd, protoreflect.ValueOf(msg))

	// Serialize to bytes and return
	return proto.Marshal(req)
}

func parseRequest(request []byte) (*RPCRequest, error) {
	var rpcRequest RPCRequest
	err := json.Unmarshal(request, &rpcRequest)
	if err != nil {
		return nil, err
	}
	return &rpcRequest, nil
}

func validateRequest(request *RPCRequest) error {
	// Check ID first, an invalid ID must return a Null ID in the response!

	// The client MUST provide an ID with a request
	if len(request.ID) <= 0 {
		return ErrMissingJSONRPCID
	}

	// Valid IDs are Number, String, or Null
	var id interface{}
	err := json.Unmarshal(request.ID, &id)
	if err != nil {
		return ErrInvalidJSONRPCID
	}

	switch t := id.(type) {
	case string:
	case float64:
		// Numbers SHOULD NOT contain fractional parts
		if t != float64(int64(t)) {
			return ErrFractionalJSONRPCID
		}
	case nil:
	default:
		return ErrUnsupportedJSONRPCIDType
	}

	// We require that JSON RPC is 2.0
	if request.JSONRPC != "2.0" {
		return ErrInvalidJSONRPCVersion
	}

	return nil
}

func translateResponse(responseBytes []byte, service string, qualifiedService string, method string, services map[string]protoreflect.FileDescriptor) RPCResponse {
	var response = RPCResponse{}

	// Get expected response type from qualified service
	filed, exists := services[service]
	if !exists {
		filed, exists = services[qualifiedService]

		if !exists {
			response.Error = RPCError{
				Code:    JSONRPCInternalError,
				Message: fmt.Sprintf("%v", ErrInvalidService.Error()),
			}
			return response
		}
	}

	// Find and create response message
	desc := filed.Messages().ByName(protoreflect.Name(service + "_response"))
	if desc == nil {
		response.Error = RPCError{
			Code:    JSONRPCInternalError,
			Message: fmt.Sprintf("%v", ErrInvalidService.Error()),
		}
		return response
	}
	resp := dynamicpb.NewMessage(desc)

	// Parse response
	err := proto.Unmarshal(responseBytes, resp)
	if err != nil {
		response.Error = RPCError{
			Code:    JSONRPCInternalError,
			Message: fmt.Sprintf("%v", err),
		}
		return response
	}

	// If error response
	fieldd := resp.Descriptor().Fields().ByName(protoreflect.Name("error"))
	if resp.Has(fieldd) {
		rpcErr := resp.Get(fieldd).Message()
		errBytes, err := protojson.Marshal(rpcErr.Interface())
		if err != nil {
			response.Error = RPCError{
				Code:    JSONRPCInternalError,
				Message: fmt.Sprintf("%v", err),
			}
			return response
		}

		var rpcError RPCError
		err = json.Unmarshal(errBytes, &rpcError)
		if err != nil {
			response.Error = RPCError{
				Code:    JSONRPCInternalError,
				Message: fmt.Sprintf("%v", err),
			}
			return response
		}

		rpcError.Code = JSONRPCInternalError
		response.Error = rpcError
		return response
	}

	// If not error
	fieldd = resp.Descriptor().Fields().ByName(protoreflect.Name(method))
	if fieldd == nil {
		response.Error = RPCError{
			Code:    JSONRPCInternalError,
			Message: fmt.Sprintf("%v", ErrInvalidService.Error()),
		}
		return response
	}

	if !resp.Has(fieldd) {
		respJSON, err := protojson.Marshal(resp.Interface())
		if err != nil {
			response.Error = RPCError{
				Code:    JSONRPCInternalError,
				Message: fmt.Sprintf("%v", err),
			}
			return response
		}

		response.Error = RPCError{
			Code:    JSONRPCInternalError,
			Message: "Unexpected response",
			Data:    respJSON,
		}
		return response
	}

	rpcResp := resp.Get(fieldd).Message()
	jsonWriter := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}
	respJSON, err := jsonWriter.Marshal(rpcResp.Interface())

	if err != nil {
		response.Error = RPCError{
			Code:    JSONRPCInternalError,
			Message: fmt.Sprintf("%v", err),
		}
		return response
	}

	response.Result = respJSON

	return response
}

// NewRequestHandler returns a new RequestHandler
func NewRequestHandler(client *koinosmq.Client) *RequestHandler {
	handler := &RequestHandler{
		mqClient:           client,
		serviceDescriptors: make(map[string]protoreflect.FileDescriptor),
	}

	return handler
}

// RegisterService from a FileDescriptor
func (h *RequestHandler) RegisterService(fd protoreflect.FileDescriptor) {
	h.serviceDescriptors[string(fd.Package())] = fd
	log.Infof("Registered descriptor package: %s", string(fd.Package()))
}

func makeErrorResponse(id json.RawMessage, code int, message string, data string) ([]byte, bool) {
	jsonError, e := json.Marshal(RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
	if e != nil {
		log.Warnf("An unexpected error has occurred: %v", e.Error())
		return make([]byte, 0), false
	}
	return jsonError, true
}

// HandleRequest handles a jsonrpc request, returning the results as a byte string
// Any error that occurs will be returned in an error response instead of propagating to the caller
// If ok = false is retured, it means the client cannot recover from this error and the caller should close the connection
func (h *RequestHandler) HandleRequest(reqBytes []byte) ([]byte, bool) {
	request, err := parseRequest(reqBytes)
	if err != nil {
		return makeErrorResponse(nil, JSONRPCParseError, "Unable to parse request", err.Error())
	}

	err = validateRequest(request)
	if err != nil {
		// If there was an error in detecting the id in the Request object (e.g. Parse error/Invalid Request), it MUST be Null.
		id := request.ID
		if errorWithID(err) {
			id = nil
		}
		return makeErrorResponse(id, JSONRPCInvalidReq, "Invalid request", err.Error())
	}

	service, qualifiedService, method, err := parseMethod(request)
	if err != nil {
		return makeErrorResponse(request.ID, JSONRPCMethodNotFound, "Unable to translate request", err.Error())
	}

	internalRequest, err := translateRequest(request, service, qualifiedService, method, h.serviceDescriptors)
	if err != nil {
		return makeErrorResponse(request.ID, JSONRPCMethodNotFound, "Unable to translate request", err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeoutSeconds*time.Second)
	defer cancel()
	log.Infof(qualifiedService)
	responseBytes, err := h.mqClient.RPCContext(ctx, "application/octet-stream", service, internalRequest)

	if err != nil {
		return makeErrorResponse(request.ID, JSONRPCInternalError, "An internal server error has occurred", err.Error())
	}

	response := translateResponse(responseBytes, service, qualifiedService, method, h.serviceDescriptors)
	response.JSONRPC = "2.0"
	response.ID = request.ID

	jsonResponse, err := json.Marshal(&response)
	if err != nil {
		return makeErrorResponse(request.ID, JSONRPCInternalError, "An internal server error has occurred", err.Error())
	}

	return jsonResponse, true
}
