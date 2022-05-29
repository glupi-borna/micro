package lsp

import (
	"encoding/json"
	"errors"

	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var ErrNotSupported = errors.New("Operation not supported by language server")

type RPCCompletion struct {
	RPCVersion string             `json:"jsonrpc"`
	ID         int                `json:"id"`
	Result     lsp.CompletionList `json:"result"`
}

type RPCCompletionAlternate struct {
	RPCVersion string               `json:"jsonrpc"`
	ID         int                  `json:"id"`
	Result     []lsp.CompletionItem `json:"result"`
}

type RPCHover struct {
	RPCVersion string    `json:"jsonrpc"`
	ID         int       `json:"id"`
	Result     lsp.Hover `json:"result"`
}

type RPCFormat struct {
	RPCVersion string         `json:"jsonrpc"`
	ID         int            `json:"id"`
	Result     []lsp.TextEdit `json:"result"`
}

type hoverAlternate struct {
	// Contents is the hover's content
	Contents []interface{} `json:"contents"`

	// Range an optional range is a range inside a text document
	// that is used to visualize a hover, e.g. by changing the background color.
	Range lsp.Range `json:"range,omitempty"`
}

type RPCHoverAlternate struct {
	RPCVersion string         `json:"jsonrpc"`
	ID         int            `json:"id"`
	Result     hoverAlternate `json:"result"`
}

type RPCLocation struct {
	RPCVersion string 		   `json:"jsonrpc"`
	ID         int             `json:"id"`
	Result     lsp.Location    `json:"result"`
}

type RPCLocations struct {
	RPCVersion string 		    `json:"jsonrpc"`
	ID         int              `json:"id"`
	Result     []lsp.Location   `json:"result"`
}

type RPCLocationLinks struct {
	RPCVersion string 		        `json:"jsonrpc"`
	ID         int                  `json:"id"`
	Result     []lsp.LocationLink   `json:"result"`
}

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

	resp, err := s.sendRequest(lsp.MethodTextDocumentFormatting, params)
	if err != nil {
		return nil, err
	}

	var r RPCFormat
	err = json.Unmarshal(resp, &r)
	if err != nil {
		return nil, err
	}

	return r.Result, nil
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

	resp, err := s.sendRequest(lsp.MethodTextDocumentFormatting, params)
	if err != nil {
		return nil, err
	}

	var rpc RPCFormat
	err = json.Unmarshal(resp, &rpc)
	if err != nil {
		return nil, err
	}

	return rpc.Result, nil
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
	resp, err := s.sendRequest(lsp.MethodTextDocumentCompletion, params)
	if err != nil {
		return nil, err
	}

	var r RPCCompletion
	err = json.Unmarshal(resp, &r)
	if err == nil {
		return r.Result.Items, nil
	}
	var ra RPCCompletionAlternate
	err = json.Unmarshal(resp, &ra)
	if err != nil {
		return nil, err
	}
	return ra.Result, nil
}

func (s *Server) CompletionResolve() {

}

func (s *Server) Hover(filename string, pos lsp.Position) (string, error) {
	if !capabilityCheck(s.capabilities.HoverProvider) {
		return "", ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequest(lsp.MethodTextDocumentHover, params)
	if err != nil {
		return "", err
	}

	var r RPCHover
	err = json.Unmarshal(resp, &r)
	if err == nil {
		return r.Result.Contents.Value, nil
	}

	var ra RPCHoverAlternate
	err = json.Unmarshal(resp, &ra)
	if err != nil {
		return "", err
	}

	for _, c := range ra.Result.Contents {
		switch t := c.(type) {
		case string:
			return t, nil
		case map[string]interface{}:
			s, ok := t["value"].(string)
			if ok {
				return s, nil
			}
		}
	}
	return "", nil
}

func (s *Server) GetDefinition(filename string, pos lsp.Position) ([]lsp.Location, error) {
	if !capabilityCheck(s.capabilities.DefinitionProvider) {
		return nil, ErrNotSupported
	}

	params := positionParams(filename, pos)

	resp, err := s.sendRequest(lsp.MethodTextDocumentDefinition, params)
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

	resp, err := s.sendRequest(lsp.MethodTextDocumentDeclaration, params)
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

	resp, err := s.sendRequest(lsp.MethodTextDocumentTypeDefinition, params)
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

	resp, err := s.sendRequest(lsp.MethodTextDocumentReferences, params)
	if err != nil {
		return nil, err
	}

	return getLocations(resp)
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
