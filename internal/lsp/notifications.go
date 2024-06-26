package lsp

import (
	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (s *Server) DidOpen(filename, language, text string, version int32) {
	doc := lsp.TextDocumentItem{
		URI:        uri.File(filename),
		LanguageID: lsp.LanguageIdentifier(language),
		Version:    version,
		Text:       text,
	}

	params := lsp.DidOpenTextDocumentParams{
		TextDocument: doc,
	}

	go s.sendNotification(lsp.MethodTextDocumentDidOpen, params)
}

func (s *Server) DidSave(filename string) {
	doc := lsp.TextDocumentIdentifier{
		URI: uri.File(filename),
	}

	params := lsp.DidSaveTextDocumentParams{
		TextDocument: doc,
	}
	go s.sendNotification(lsp.MethodTextDocumentDidSave, params)
}

func (s *Server) DidChange(filename string, version int32, changes []lsp.TextDocumentContentChangeEvent) {
	doc := lsp.VersionedTextDocumentIdentifier{
		TextDocumentIdentifier: lsp.TextDocumentIdentifier{
			URI: uri.File(filename),
		},
		Version: version,
	}

	params := lsp.DidChangeTextDocumentParams{
		TextDocument:   doc,
		ContentChanges: changes,
	}
	go s.sendNotification(lsp.MethodTextDocumentDidChange, params)
}

func (s *Server) DidClose(filename string) {
	doc := lsp.TextDocumentIdentifier{
		URI: uri.File(filename),
	}

	params := lsp.DidCloseTextDocumentParams{
		TextDocument: doc,
	}

	fileuri := uri.File(filename)
	_, exists := s.diagnostics.Load(fileuri)
	if exists {
		s.diagnostics.Delete(fileuri)
	}

	go s.sendNotification(lsp.MethodTextDocumentDidClose, params)
}
