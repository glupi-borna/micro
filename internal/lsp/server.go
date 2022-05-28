package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"fmt"
	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/tcell/v2"
	"github.com/zyedidia/micro/v2/internal/screen"
)

var activeServers map[string]*Server
var slock sync.Mutex

func init() {
	activeServers = make(map[string]*Server)
}

func GetServer(l Language, dir string) *Server {
	s, ok := activeServers[l.Command+"-"+dir]
	if ok && s.Active {
		return s
	}
	return nil
}

func GetActiveServerNames() []string {
	var servers []string

	for server := range activeServers {
		servers = append(servers, server)
	}

	return servers
}

func ShutdownAllServers() {
	for _, s := range activeServers {
		if s.Active {
			s.Shutdown()
		}
	}
}

type Server struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	language     *Language
	capabilities lsp.ServerCapabilities
	root         string
	lock         sync.Mutex
	Active       bool
	requestID    int
	responses    map[int]chan ([]byte)
	diagnostics  sync.Map
}

type RPCRequest struct {
	RPCVersion string      `json:"jsonrpc"`
	ID         int         `json:"id"`
	Method     string      `json:"method"`
	Params     interface{} `json:"params"`
}

type RPCNotification struct {
	RPCVersion string      `json:"jsonrpc"`
	Method     string      `json:"method"`
	Params     interface{} `json:"params"`
}

type RPCInit struct {
	RPCVersion string               `json:"jsonrpc"`
	ID         int                  `json:"id"`
	Result     lsp.InitializeResult `json:"result"`
}

type RPCResult struct {
	RPCVersion string `json:"jsonrpc"`
	ID         int    `json:"id,omitempty"`
	Method     string `json:"method,omitempty"`
}

type RPCDiag struct {
	RPCVersion string `json:"jsonrpc"`
	ID     int                          `json:"id,omitempty"`
	Method string                       `json:"method,omitempty"`
	Params lsp.PublishDiagnosticsParams `json:"params"`
}


func StartServer(l Language) (*Server, error) {
	s := new(Server)

	c := exec.Command(l.Command, l.Args...)

	c.Stderr = log.Writer()

	stdin, err := c.StdinPipe()
	if err != nil {
		log.Println("[micro-lsp]", err)
		return nil, err
	}

	stdout, err := c.StdoutPipe()
	if err != nil {
		log.Println("[micro-lsp]", err)
		return nil, err
	}

	err = c.Start()
	if err != nil {
		log.Println("[micro-lsp]", err)
		return nil, err
	}

	s.cmd = c
	s.stdin = stdin
	s.stdout = bufio.NewReader(stdout)
	s.language = &l
	s.responses = make(map[int]chan []byte)

	return s, nil
}

// Initialize performs the LSP initialization handshake
// The directory must be an absolute path
func (s *Server) Initialize(directory string) {
	params := lsp.InitializeParams{
		ProcessID: int32(os.Getpid()),
		RootURI:   uri.File(directory),
		Capabilities: lsp.ClientCapabilities{
			Workspace: &lsp.WorkspaceClientCapabilities{
				WorkspaceEdit: &lsp.WorkspaceClientCapabilitiesWorkspaceEdit{
					DocumentChanges:    true,
					ResourceOperations: []string{"create", "rename", "delete"},
				},
				ApplyEdit: true,
			},
			TextDocument: &lsp.TextDocumentClientCapabilities{
				Formatting: &lsp.TextDocumentClientCapabilitiesFormatting{
					DynamicRegistration: false,
				},
				Completion: &lsp.CompletionTextDocumentClientCapabilities{
					DynamicRegistration: false,
					CompletionItem: &lsp.TextDocumentClientCapabilitiesCompletionItem{
						SnippetSupport:          false,
						CommitCharactersSupport: false,
						DocumentationFormat:     []lsp.MarkupKind{lsp.PlainText},
						DeprecatedSupport:       false,
						PreselectSupport:        false,
						InsertReplaceSupport:    true,
					},
					ContextSupport: false,
				},
				Hover: &lsp.TextDocumentClientCapabilitiesHover{
					DynamicRegistration: false,
					ContentFormat:       []lsp.MarkupKind{lsp.PlainText},
				},
			},
		},
	}

	activeServers[s.language.Command+"-"+directory] = s
	s.Active = true
	s.root = directory

	go s.receive()

	s.lock.Lock()
	go func() {
		resp, err := s.sendRequest(lsp.MethodInitialize, params)
		if err != nil {
			log.Println("[micro-lsp]", err)
			s.Active = false
			s.lock.Unlock()
			return
		}

		// todo parse capabilities
		log.Println("[micro-lsp] <<<", string(resp))

		var r RPCInit
		json.Unmarshal(resp, &r)

		s.lock.Unlock()
		err = s.sendNotification(lsp.MethodInitialized, struct{}{})
		if err != nil {
			log.Println("[micro-lsp]", err)
		}

		s.capabilities = r.Result.Capabilities
	}()
}

func (s *Server) GetLanguage() *Language {
	return s.language
}

func (s *Server) GetCommand() *exec.Cmd {
	return s.cmd
}

func (s *Server) Shutdown() {
	s.sendRequest(lsp.MethodShutdown, nil)
	s.sendNotification(lsp.MethodExit, nil)
	s.Active = false
}

func (s *Server) receive() {
	for s.Active {
		resp, err := s.receiveMessage()
		if err == io.EOF {
			log.Println("Received EOF, shutting down")
			s.Active = false
			return
		}
		if err != nil {
			log.Println("[micro-lsp,error]", err)
			continue
		}
		log.Println("[micro-lsp] <<<", string(resp))

		var r RPCResult
		err = json.Unmarshal(resp, &r)
		if err != nil {
			log.Println("[micro-lsp,error]", err)
			continue
		}

		switch r.Method {
		case lsp.MethodWindowLogMessage:
			// TODO
		case lsp.MethodTextDocumentPublishDiagnostics:
			var diag RPCDiag
			err = json.Unmarshal(resp, &diag)
			if err != nil {
				log.Println("[micro-lsp,error]", err)
				continue
			}
			fileuri := uri.URI(string(diag.Params.URI))
			s.diagnostics.Store(fileuri, diag.Params.Diagnostics)
		case "":
			// Response
			if _, ok := s.responses[r.ID]; ok {
				log.Println("[micro-lsp] Got response for", r.ID)
				s.responses[r.ID] <- resp
			}
		}
	}
}

func Style(d *lsp.Diagnostic) tcell.Style {
	switch d.Severity {
	case lsp.DiagnosticSeverityInformation:
	case lsp.DiagnosticSeverityHint:
		if style, ok := config.Colorscheme["gutter-info"]; ok {
			return style
		}
	case lsp.DiagnosticSeverityWarning:
		if style, ok := config.Colorscheme["gutter-warning"]; ok {
			return style
		}
	case lsp.DiagnosticSeverityError:
		if style, ok := config.Colorscheme["gutter-error"]; ok {
			return style
		}
	}
	return config.DefStyle
}

func (s *Server) GetDiagnostics(filename string) []lsp.Diagnostic {
	fileuri := uri.File(filename)
	diags, exists := s.diagnostics.Load(fileuri)
	if exists {
		return diags.([]lsp.Diagnostic)
	} else {
		return nil
	}
}

func (s *Server) DiagnosticsCount(filename string) int {
	fileuri := uri.File(filename)
	diags, exists := s.diagnostics.Load(fileuri)
	if exists {
		return len(diags.([]lsp.Diagnostic))
	} else {
		return 0
	}
}

func (s *Server) receiveMessage() (outbyte []byte, err error) {
	defer func() {
		if r:= recover(); r != nil {
			err = fmt.Errorf("pkg: %v", r)
			outbyte = nil
		} else {
			go screen.Redraw();
		}
	}()

	n := -1
	for {
		b, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		headerline := strings.TrimSpace(string(b))
		if len(headerline) == 0 {
			break
		}
		if strings.HasPrefix(headerline, "Content-Length:") {
			split := strings.Split(headerline, ":")
			if len(split) <= 1 {
				break
			}
			n, err = strconv.Atoi(strings.TrimSpace(split[1]))
			if err != nil {
				return nil, err
			}
		}
	}

	if n <= 0 {
		return []byte{}, nil
	}

	outbyte = make([]byte, n)
	_, err = io.ReadFull(s.stdout, outbyte)
	if err != nil {
		log.Println("[micro-lsp]", err)
	}
	return outbyte, err
}

func (s *Server) sendNotification(method string, params interface{}) error {
	m := RPCNotification{
		RPCVersion: "2.0",
		Method:     method,
		Params:     params,
	}

	s.lock.Lock()
	go s.sendMessageUnlock(m)
	return nil
}

func (s *Server) sendRequest(method string, params interface{}) ([]byte, error) {
	id := s.requestID
	s.requestID++
	r := make(chan []byte)
	s.responses[id] = r

	m := RPCRequest{
		RPCVersion: "2.0",
		ID:         id,
		Method:     method,
		Params:     params,
	}

	err := s.sendMessage(m)
	if err != nil {
		return nil, err
	}

	var bytes []byte
	select {
	case bytes = <-r:
	case <-time.After(5 * time.Second):
		err = errors.New("Request timed out")
	}
	delete(s.responses, id)

	return bytes, err
}

func (s *Server) sendMessage(m interface{}) error {
	msg, err := json.Marshal(m)
	if err != nil {
		return err
	}

	log.Println("[micro-lsp] >>>", string(msg))

	// encode header and proper line endings
	msg = append(msg, '\r', '\n')
	header := []byte("Content-Length: " + strconv.Itoa(len(msg)) + "\r\n\r\n")
	msg = append(header, msg...)

	_, err = s.stdin.Write(msg)
	return err
}

func (s *Server) sendMessageUnlock(m interface{}) error {
	defer s.lock.Unlock()
	return s.sendMessage(m)
}
