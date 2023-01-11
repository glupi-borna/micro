package lsp

import (
	"encoding/json"
	"errors"
	"reflect"
	"fmt"

	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var ErrNotSupported = errors.New("Operation not supported by language server")

type LSPError int

const (
	ParseError LSPError     = -32700;
	InvalidRequest          = -32600;
	MethodNotFound          = -32601;
	InvalidParams           = -32602;
	InternalError           = -32603;
	ServerNotInitialized    = -32002;
	UnknownErrorCode        = -32001;
	RequestFailed           = -32803;
	ServerCancelled         = -32802;
	ContentModified         = -32801;
	RequestCancelled        = -32800;
)

func (err LSPError) String() string {
	switch err {
		case ParseError: return "ParseError"
		case InvalidRequest: return "InvalidRequest"
		case MethodNotFound: return "MethodNotFound"
		case InvalidParams: return "InvalidParams"
		case InternalError: return "InternalError"
		case ServerNotInitialized: return "ServerNotInitialized"
		case UnknownErrorCode: return "UnknownErrorCode"
		case RequestFailed: return "RequestFailed"
		case ServerCancelled: return "ServerCancelled"
		case ContentModified: return "ContentModified"
		case RequestCancelled: return "RequestCancelled"
	}
	return "UnknownLSPError"
}

type lspError struct {
	Code    LSPError             `json:"code"`
	Message string               `json:"message"`
}

type RPCError struct {
	RPCVersion string             `json:"jsonrpc"`
	ID         int                `json:"id"`
	LSPError   *lspError          `json:"error"`
}

func (e *RPCError) Error() string {
	return e.LSPError.Code.String() + ": " + e.LSPError.Message
}

type RPCResponse[RESULT any] struct {
	RPCVersion string `json:"jsonrpc"`
	ID         int    `json:"id"`
	Result     RESULT `json:"result"`
}

type rangePlaceholder struct {
	Range       lsp.Range           `json:"range"`
	Placeholder string              `json:"placeholder"`
}

type renameDefault struct {
	DefaultBehavior bool            `json:"defaultBehavior"`
}

type RenameSymbol struct {
	Range       lsp.Range
	Placeholder string
	UseDefault  bool
	UseRange    bool
	CanRename   bool
}

type LSPHover struct {
	Contents interface{} `json:"contents"`
}

type RPCCompletion = RPCResponse[lsp.CompletionList]
type RPCCompletionAlt = RPCResponse[[]lsp.CompletionItem]
type RPCHover = RPCResponse[LSPHover]
type RPCLocation = RPCResponse[lsp.Location]
type RPCLocations = RPCResponse[[]lsp.Location]
type RPCLocationLinks = RPCResponse[[]lsp.LocationLink]
type RPCRange = RPCResponse[lsp.Range]
type RPCRangePlaceholder = RPCResponse[rangePlaceholder]
type RPCRenameDefault = RPCResponse[renameDefault]
type RPCRename = RPCResponse[lsp.WorkspaceEdit]

func (s *Server) sendRequestChecked(method string, params interface{}) ([]byte, error) {
	resp, err := s.sendRequest(method, params)
	if err != nil {
		return resp, err
	}

	var rpcError RPCError
	err = json.Unmarshal(resp, &rpcError)
	if err == nil && rpcError.LSPError != nil {
		return resp, &rpcError
	}

	return resp, nil
}

func sendUnmarshal[K any](s *Server, method string, params interface{}) (K, error) {
	var empty K
	resp, err := s.sendRequestChecked(method, params)
	if err != nil { return empty, err }

	var r RPCResponse[K]
	err = json.Unmarshal(resp, &r)
	if err != nil { return empty, err }

	return r.Result, nil
}

func typedUnmarshaller[P any, K any](method string) func(*Server, P)(K, error) {
	return func(s *Server, params P)(K, error) {
		return sendUnmarshal[K](s, method, params)
	}
}

var unmarshalFormat = typedUnmarshaller[
	lsp.DocumentFormattingParams,
	[]lsp.TextEdit,
](lsp.MethodTextDocumentFormatting)

var unmarshalRangeFormat = typedUnmarshaller[
	lsp.DocumentRangeFormattingParams,
	[]lsp.TextEdit,
](lsp.MethodTextDocumentRangeFormatting)

func Position(x, y uint32) lsp.Position {
	return lsp.Position{
		Line:      y,
		Character: x,
	}
}

func (s *Server) DocumentFormat(filename string, options lsp.FormattingOptions) ([]lsp.TextEdit, error) {
	if !capabilityCheck(s.capabilities.DocumentFormattingProvider) {
		return nil, ErrNotSupported
	}
	doc := lsp.TextDocumentIdentifier{
		URI: uri.File(filename),
	}

	params := lsp.DocumentFormattingParams{
		Options:      options,
		TextDocument: doc,
	}

	return unmarshalFormat(s, params)
}

func (s *Server) DocumentRangeFormat(filename string, r lsp.Range, options lsp.FormattingOptions) ([]lsp.TextEdit, error) {
	if !capabilityCheck(s.capabilities.DocumentRangeFormattingProvider) {
		return nil, ErrNotSupported
	}

	doc := lsp.TextDocumentIdentifier{
		URI: uri.File(filename),
	}

	params := lsp.DocumentRangeFormattingParams{
		Options:      options,
		Range:        r,
		TextDocument: doc,
	}

	return unmarshalRangeFormat(s, params)
}

func (s *Server) Completion(filename string, pos lsp.Position) ([]lsp.CompletionItem, error) {
	if !capabilityCheck(s.capabilities.CompletionProvider) {
		return nil, ErrNotSupported
	}

	cc := lsp.CompletionContext{
		TriggerKind: lsp.CompletionTriggerKindInvoked,
	}

	docpos := positionParams(filename, pos)

	params := lsp.CompletionParams{
		TextDocumentPositionParams: docpos,
		Context:                    &cc,
	}
	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentCompletion, params)
	if err != nil {
		return nil, err
	}

	var r RPCCompletion
	err = json.Unmarshal(resp, &r)
	if err == nil {
		return r.Result.Items, nil
	}
	var ra RPCCompletionAlt
	err = json.Unmarshal(resp, &ra)
	if err != nil {
		return nil, err
	}
	return ra.Result, nil
}

func (s *Server) extractString(value reflect.Value, original interface{}) (string, error) {
	if (original == nil) { return "", nil }
	// if (value.IsZero()) { return "" }
	rt := value.Type()
	switch rt.Kind() {
		case reflect.String:
			return value.String(), nil

		case reflect.Map:
			value := value.MapIndex(reflect.ValueOf("value"))
			if value.IsZero() { return "", errors.New("map: zero value") }
			if !value.IsValid() { return "", errors.New("map: invalid value") }
			return s.extractString(value, original)

		case reflect.Slice: fallthrough
		case reflect.Array:
			len := value.Len()

			str := ""
			for i:=0; i<len; i++ {
				substr, err := s.extractString(value.Index(i), original)
				if err != nil { return "", err }
				str += substr + "\n"
			}

			return str, nil

		case reflect.Struct:
			len := rt.NumField()
			str := ""
			for i:=0; i<len; i++ {
				if rt.Field(i).Name == "Value" {
					return value.Field(i).String(), nil
				}
				str += rt.Field(i).Name + ":" + rt.Field(i).Type.Name() + "\n"
			}
			return "", errors.New(fmt.Sprint("struct:", str))

		default:
			iface := value.Interface()
			switch val := iface.(type){
				case string: return val, nil
				case map[string]interface{}:
					v, ok := val["value"]
					if !ok { return "", errors.New("no value field!") }
					str, ok := v.(string)
					if !ok { return "", errors.New("value field is not a string!") }
					return str, nil
			}

			return "", errors.New("interface: " + fmt.Sprintf("%v: %v", rt.Kind().String(), original))
	}
}

func (s *Server) Hover(filename string, pos lsp.Position) (string, error) {
	if !capabilityCheck(s.capabilities.HoverProvider) {
		return "", ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentHover, params)
	if err != nil {
		return "", err
	}

	var ra RPCHover
	err = json.Unmarshal(resp, &ra)
	if err != nil {
		return "", err
	}

	return s.extractString(reflect.ValueOf(ra.Result.Contents), ra.Result.Contents)
}

func (s *Server) GetDefinition(filename string, pos lsp.Position) ([]lsp.Location, error) {
	if !capabilityCheck(s.capabilities.DefinitionProvider) {
		return nil, ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentDefinition, params)
	if err != nil {
		return nil, err
	}

	return getLocations(resp)
}

func (s *Server) GetDeclaration(filename string, pos lsp.Position) ([]lsp.Location, error) {
	if !capabilityCheck(s.capabilities.DeclarationProvider) {
		return nil, ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentDeclaration, params)
	if err != nil {
		return nil, err
	}

	return getLocations(resp)
}

func (s *Server) GetTypeDefinition(filename string, pos lsp.Position) ([]lsp.Location, error) {
	if !capabilityCheck(s.capabilities.TypeDefinitionProvider) {
		return nil, ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentTypeDefinition, params)
	if err != nil {
		return nil, err
	}

	return getLocations(resp)
}

func (s *Server) FindReferences(filename string, pos lsp.Position) ([]lsp.Location, error) {
	if !capabilityCheck(s.capabilities.ReferencesProvider) {
		return nil, ErrNotSupported
	}

	params := lsp.ReferenceParams {
		Context: lsp.ReferenceContext {
			IncludeDeclaration: true,
		},
		TextDocumentPositionParams: positionParams(filename, pos),
	}

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentReferences, params)
	if err != nil {
		return nil, err
	}

	return getLocations(resp)
}

func (s *Server) GetRenameSymbol(filename string, pos lsp.Position) (RenameSymbol, error) {
	if !capabilityCheck(s.capabilities.RenameProvider) {
		return RenameSymbol{CanRename: false}, ErrNotSupported
	}

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentPrepareRename, positionParams(filename, pos))
	if err != nil {
		return RenameSymbol{CanRename: false}, err
	}

	var r RPCRange
	err = json.Unmarshal(resp, &r)
	if err == nil {
		return RenameSymbol{
			Range: r.Result,
			UseRange: true,
			CanRename: true,
		}, nil
	}

	var ra1 RPCRangePlaceholder
	err = json.Unmarshal(resp, &ra1)
	if err == nil {
		return RenameSymbol{
			Range: ra1.Result.Range,
			Placeholder: ra1.Result.Placeholder,
			UseRange: false,
			CanRename: true,
		}, nil
	}

	var ra2 RPCRenameDefault
	err = json.Unmarshal(resp, &ra2)
	if err != nil {
		return RenameSymbol{
			UseDefault: ra2.Result.DefaultBehavior,
			CanRename: true,
		}, nil
	}

	return RenameSymbol{CanRename: false}, nil
}

func (s *Server) RenameSymbol(filename string, pos lsp.Position, new_name string) (lsp.WorkspaceEdit, error) {
	if !capabilityCheck(s.capabilities.RenameProvider) {
		return lsp.WorkspaceEdit{}, ErrNotSupported
	}

	params := lsp.RenameParams {
		TextDocumentPositionParams: positionParams(filename, pos),
		NewName: new_name,
	}

	resp, err := s.sendRequestChecked(lsp.MethodTextDocumentRename, params)
	if err != nil {
		return lsp.WorkspaceEdit{}, err
	}

	var r RPCRename
	err = json.Unmarshal(resp, &r)
	if err != nil {
		return lsp.WorkspaceEdit{}, err
	}

	return r.Result, nil
}

func capabilityCheck(capability interface{}) bool {
	b, ok := capability.(bool)
	if ok {
		return b
	}
	return capability != nil
}

func positionParams(filename string, pos lsp.Position) lsp.TextDocumentPositionParams {
	return lsp.TextDocumentPositionParams {
		TextDocument: lsp.TextDocumentIdentifier{
			URI: uri.File(filename),
		},
		Position: pos,
	}
}

func getLocations(resp []byte) ([]lsp.Location, error) {
	var r RPCLocation
	err := json.Unmarshal(resp, &r)
	if err == nil {
		return []lsp.Location{r.Result}, nil
	}

	var ra1 RPCLocations
	err = json.Unmarshal(resp, &ra1)
	if err == nil {
		return ra1.Result, nil
	}

	var ra2 RPCLocationLinks
	err = json.Unmarshal(resp, &ra2)
	if err != nil {
		return nil, err
	}

	var res []lsp.Location
	for _, loc := range ra2.Result {
		res = append(res, lsp.Location{
			URI: loc.TargetURI,
			Range: loc.TargetRange,
		})
	}

	return res, nil
}
